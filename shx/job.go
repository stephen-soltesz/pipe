// Package shx provides shell-like operations for Go.
//
// A Job represents one or more operations. A single-operation Job may represent
// running a command (Exec, System), or reading or writing a file (ReadFile,
// WriteFile), or a user defined operation (Func). A multiple-operation Job runs
// several single operation jobs in a sequence (Script) or pipeline (Pipe).
// Taken together, these primitive types allow the composition of more and more
// complex operations.
//
// Users control how a Job runs using the State. State controls the Job input
// and output, as well as its working directory and environment.
//
// Users may produce a human-readable representation of a complex Job in a
// shell-like syntax using the Description. Because some operations have no
// shell equivalent, the result is only representative.
//
// Examples are provided for all primitive Job types: Exec, System, Func, Pipe,
// Script. Additional convenience Jobs make creating more complex operations a
// little easier. Advanced users may create their own Job types for more
// flexibility.
package shx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// State is a Job configuration. Callers provide the first initial State, and
// as a Job executes it creates new State instances derived from the original,
// e.g. for Pipes and subcommands.
type State struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
	Dir    string
	Env    []string
}

// Description is used to produce a representation of a Job. Custom Job types
// should use the Description interface to represent their behavior in a
// helpful, human-readable form. After collecting a description, serialize using
// the String() method.
type Description struct {
	// Depth is used to control line prefix indentation in complex Jobs.
	Depth int
	desc  bytes.Buffer
	line  int
	pipe  bool
	idx   int
	sep   string
}

// Line adds a new command to the output buffer. If OpenRight() was previously
// called, then the command is appended as a continuation of a single line.
// Otherwise, the command is formatted as a single line.
func (d *Description) Line(cmd string) {
	if d.pipe {
		d.idx++
		if d.idx > 1 {
			d.desc.WriteString(d.sep + cmd)
			return
		}
		d.desc.WriteString(fmt.Sprintf("%2d: %s%s", d.line, prefix(d.Depth), cmd))
		return
	}
	d.line++
	d.desc.WriteString(fmt.Sprintf("%2d: %s%s\n", d.line, prefix(d.Depth), cmd))
}

// OpenRight begins formatting a multi-part expression on a single line.
// OpenRight may help format a list, a pipeline, or similar expression.
// Subsequent calls to Line() add commands to the end of the current line,
// prefixed by "sep". OpenRight() returns a closeright function that terminates
// the line and resets the default behavior of Line() until OpenRight is called
// again.
func (d *Description) OpenRight(sep string) (closeright func(end string)) {
	d.line++
	d.pipe = true
	d.idx = 0
	d.sep = sep
	closeright = func(end string) {
		d.pipe = false
		// Verify that some commands were printed before adding extra newline.
		if d.idx > 0 {
			d.desc.WriteString(end + "\n")
		}
	}
	return closeright
}

// String serializes a description produced by running Job.Describe(). Calling
// String() resets the Description buffer.
func (d *Description) String() string {
	s := d.desc.String()
	d.desc.Reset()
	return s
}

// New creates a State instance based on the current process state, using
// os.Stdin, os.Stdout, and os.Stderr, as well as the current working directory
// and environment.
func New() *State {
	d, _ := os.Getwd()
	s := &State{
		Stdin:  os.Stdin,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
		Dir:    d,
		Env:    os.Environ(),
	}
	return s
}

func (s *State) copy() *State {
	// Make independent copy of environment.
	env2 := make([]string, len(s.Env))
	for _, v := range s.Env {
		env2 = append(env2, v)
	}
	return &State{
		Stdin:  s.Stdin,
		Stdout: s.Stdout,
		Stderr: s.Stderr,
		Dir:    s.Dir,
		Env:    env2,
	}
}

func prefix(d int) string {
	v := ""
	for i := 0; i < d; i++ {
		v = v + "  "
	}
	return v
}

// SetDir sets the State Dir value and returns the previous value.
func (s *State) SetDir(dir string) string {
	prev := s.Dir
	s.Dir = dir
	return prev
}

// Path produces a path relative to the State's current directory. If arguments
// represent an absolute path, then that is used. If multiple arguments are
// provided, they're joined using filepath.Join.
func (s *State) Path(path ...string) string {
	if len(path) == 0 {
		return s.Dir
	}
	if filepath.IsAbs(path[0]) {
		return filepath.Join(path...)
	}
	if len(path) == 1 {
		return filepath.Join(s.Dir, path[0])
	}
	return filepath.Join(append([]string{s.Dir}, path...)...)
}

// SetEnv sets the named variable to the given value in the State environment.
// If the named variable is already defined it is overwritten.
func (s *State) SetEnv(name, value string) {
	prefix := name + "="
	// Find and overwrite an existing value.
	for i, kv := range s.Env {
		if strings.HasPrefix(kv, prefix) {
			s.Env[i] = prefix + value
			return
		}
	}
	// Or, add the new value to the s.Env.
	s.Env = append(s.Env, prefix+value)
}

// GetEnv reads the named variable from the State environment. If name is not
// found, an empty value is returned. An undefined variable and a variable set
// to the empty value are indistinguishable.
func (s *State) GetEnv(name string) string {
	prefix := name + "="
	for _, kv := range s.Env {
		if strings.HasPrefix(kv, prefix) {
			return strings.TrimPrefix(kv, prefix)
		}
	}
	// name not found.
	return ""
}

// Job is the interface for an operation. A Job controls how an operation is run
// and represented.
type Job interface {
	// Describe produces a readable representation of the Job operation. After
	// calling Describe, use Description.String() to report the result.
	Describe(d *Description)

	// Run executes the Job using the given State. A Job should terminate when
	// the given context is cancelled.
	Run(ctx context.Context, s *State) error
}

// Exec creates a Job to execute the given command with the given arguments.
func Exec(cmd string, args ...string) *ExecJob {
	return &ExecJob{
		name: cmd,
		args: args,
	}
}

// System is an Exec job that interprets the given cmd using "/bin/sh".
func System(cmd string) *ExecJob {
	return &ExecJob{
		name: "/bin/sh",
		args: []string{"-c", cmd},
	}
}

// ExecJob implements the Job interface for basic process execution.
type ExecJob struct {
	name string
	args []string
}

// Run executes the command.
func (f *ExecJob) Run(ctx context.Context, s *State) error {
	cmd := exec.CommandContext(ctx, f.name, f.args...)
	cmd.Dir = s.Dir
	cmd.Env = s.Env
	cmd.Stdin = s.Stdin
	cmd.Stdout = s.Stdout
	cmd.Stderr = s.Stderr
	err := cmd.Start()
	if err != nil {
		return err
	}
	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("%w: %s %s", err, f.name, strings.Join(f.args, " "))
	}
	return nil
}

// Describe generates a description for this command.
func (f *ExecJob) Describe(d *Description) {
	args := ""
	if len(f.args) > 0 {
		args = " " + strings.Join(f.args, " ")
	}
	d.Line(f.name + args)
}

// Func creates a new FuncJob that runs the given function. Job functions should
// honor the context to support cancelation. The given name is used to describe
// this function.
func Func(name string, job func(ctx context.Context, s *State) error) *FuncJob {
	return &FuncJob{
		Job: job,
		Desc: func(d *Description) {
			d.Line(name)
		},
	}
}

// FuncJob is a generic Job type that allows creating new operations without
// creating a totally new type. When created directly, both Job and Desc fields
// must be defined.
type FuncJob struct {
	Job  func(ctx context.Context, s *State) error
	Desc func(d *Description)
}

// Run executes the job function.
func (f *FuncJob) Run(ctx context.Context, s *State) error {
	return f.Job(ctx, s)
}

// Describe generates a description for this custom function.
func (f *FuncJob) Describe(d *Description) {
	f.Desc(d)
}

// NewReaderContext creates a context-aware io.Reader. This is helpful for
// creating custom Jobs that are context-aware when reading from otherwise
// unbounded IO operations, e.g. io.Copy().
func NewReaderContext(ctx context.Context, r io.Reader) io.Reader {
	return &readerCtx{ctx: ctx, Reader: r}
}

type readerCtx struct {
	ctx context.Context
	io.Reader
}

func (r *readerCtx) Read(p []byte) (n int, err error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.Reader.Read(p)
}

// Read creates a Job that reads from the given reader and writes it to Job's
// stdout. Read creates a context-aware reader from the given io.Reader.
func Read(r io.Reader) Job {
	return &FuncJob{
		Job: func(ctx context.Context, s *State) error {
			_, err := io.Copy(s.Stdout, NewReaderContext(ctx, r))
			return err
		},
		Desc: func(d *Description) {
			d.Line(fmt.Sprintf("read(%v)", r))
		},
	}
}

// ReadFile creates a Job that reads from the named file and writes it to the
// Job's stdout.
func ReadFile(path string) Job {
	return &FuncJob{
		Job: func(ctx context.Context, s *State) error {
			file, err := os.Open(s.Path(path))
			if err != nil {
				return err
			}
			defer file.Close()
			_, err = io.Copy(s.Stdout, NewReaderContext(ctx, file))
			return err
		},
		Desc: func(d *Description) {
			d.Line(fmt.Sprintf("cat < %s", path))
		},
	}
}

// Write creates a Job that reads from the Job input and writes to the given
// writer. Write creates a context-aware reader from the Job input.
func Write(w io.Writer) Job {
	return &FuncJob{
		Job: func(ctx context.Context, s *State) error {
			_, err := io.Copy(w, NewReaderContext(ctx, s.Stdin))
			return err
		},
		Desc: func(d *Description) {
			d.Line(fmt.Sprintf("write(%v)", w))
		},
	}
}

// WriteFile creates a Job that reads from the Job input and writes to the named
// file. The output path is created if it does not exist and is truncated if it
// does.
func WriteFile(path string, perm os.FileMode) Job {
	return &FuncJob{
		Job: func(ctx context.Context, s *State) error {
			file, err := os.OpenFile(s.Path(path), os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
			if err != nil {
				return err
			}
			defer file.Close()
			_, err = io.Copy(file, NewReaderContext(ctx, s.Stdin))
			return err
		},
		Desc: func(d *Description) {
			d.Line(fmt.Sprintf("cat > %s", path))
		},
	}
}

// Chdir creates Job that changes the Job state Dir. This does not alter the
// process working directory.
func Chdir(dir string) Job {
	return &FuncJob{
		Job: func(ctx context.Context, s *State) error {
			s.Dir = s.Path(dir)
			return nil
		},
		Desc: func(d *Description) {
			d.Line(fmt.Sprintf("cd %s", dir))
		},
	}
}

// SetEnv creates a Job that changes to Job state Env by setting name=value.
// SetEnv is most helpful in Script() Jobs.
func SetEnv(name string, value string) Job {
	return &FuncJob{
		Job: func(ctx context.Context, s *State) error {
			s.SetEnv(name, value)
			return nil
		},
		Desc: func(d *Description) {
			d.Line(fmt.Sprintf("export %s=%s", name, value))
		},
	}
}

// SetEnvFromJob creates a new Job that sets the given name in Env to the result
// written to stdout by running the given Job. Errors from the given Job are
// returned.
func SetEnvFromJob(name string, job Job) Job {
	return &FuncJob{
		Job: func(ctx context.Context, s *State) error {
			b := &bytes.Buffer{}
			s2 := &State{Stdout: b}
			err := job.Run(ctx, s2)
			if err != nil {
				return err
			}
			s.SetEnv(name, strings.TrimSpace(b.String()))
			return nil
		},
		Desc: func(d *Description) {
			cp := d.OpenRight("")
			d.Line(fmt.Sprintf("export %s=$(", name))
			job.Describe(d)
			d.Line(")")
			cp("")
		},
	}
}

// Script creates a Job that executes the given Job parameters in sequence. If
// any Job returns an error, execution stops.
func Script(t ...Job) *ScriptJob {
	return &ScriptJob{
		Jobs: t,
	}
}

// ErrScriptError is a base Script error.
var ErrScriptError = errors.New("script execution error")

// ScriptJob implements the Job interface for running an ordered sequence of Jobs.
type ScriptJob struct {
	Jobs []Job
}

// Run sequentially executes every Job in the script. Any Job error stops
// execution and generates an error describing the command that failed.
func (c *ScriptJob) Run(ctx context.Context, s *State) error {
	z := s.copy()
	for i := range c.Jobs {
		err := c.Jobs[i].Run(ctx, z)
		// Special handling for script errors.
		if err != nil && !errors.Is(err, ErrScriptError) {
			d := &Description{}
			c.Describe(d)
			str := d.String()
			return fmt.Errorf("%w:\n%s - %s", ErrScriptError, str, err.Error())
		}
		// All other errors.
		if err != nil {
			return err
		}
	}
	return nil
}

// Describe generates a description for all jobs in the script.
func (c *ScriptJob) Describe(d *Description) {
	d.Line("(")
	d.Depth++
	for i := range c.Jobs {
		c.Jobs[i].Describe(d)
	}
	d.Depth--
	d.Line(")")
}

// Pipe creates a Job that executes the given Jobs as a "shell pipeline",
// passing the output of the first to the input of the next, and so on.
// If any Job returns an error, the first error is returned.
func Pipe(t ...Job) *PipeJob {
	return &PipeJob{
		Jobs: t,
	}
}

// PipeJob implements the Job interface for running multiple Jobs in a
// pipeline.
type PipeJob struct {
	Jobs []Job
}

// Run executes every Job in the pipeline. The stdout from the first command is
// passed to the stdin to the next command. The stderr for all commands is
// inherited from the given State. If any Job returns an error, the first error
// is returned for the entire PipeJob.
func (c *PipeJob) Run(ctx context.Context, z *State) error {
	e := c.Jobs
	p := nPipes(z.Stdin, z.Stdout, len(e))
	s := make([]*State, len(e))
	for i := range e {
		s[i] = &State{
			Stdin:  p[i].R,
			Stdout: p[i].W,
			Stderr: z.Stderr,
			Dir:    z.Dir,
			Env:    z.Env,
		}
	}
	// Create channel for all pipe job return values.
	done := make(chan error, len(e))

	// Create a wait group to block on all Jobs returning.
	wg := sync.WaitGroup{}
	defer wg.Wait()

	// Context cancellation will execute before waiting on wait group.
	ctx2, cancel2 := context.WithCancel(ctx)
	defer cancel2()

	// Run all jobs in reverse order, end of pipe to beginning.
	for i := len(e) - 1; i >= 0; i-- {
		wg.Add(1)
		go func(n, i int, e Job, s *State) {
			err := e.Run(ctx2, s)
			// Send possible errors to outer loop.
			done <- err
			wg.Done()
			if i != 0 {
				closeReader(s.Stdin)
			}
			if i != n-1 {
				closeWriter(s.Stdout)
			}
		}(len(e), i, e[i], s[i])
	}

	// Wait for goroutines to return or context cancellation.
	for range e {
		var err error
		select {
		case err = <-done:
		case <-ctx.Done():
			// Continue colleting errors after context cancellation.
			err = <-done
		}
		// Return first error. Deferred wait group will block until all Jobs return.
		if err != nil {
			return err
		}
	}
	return nil
}

// Describe generates a description for all jobs in the pipeline.
func (c *PipeJob) Describe(d *Description) {
	closePipe := d.OpenRight(" | ")
	defer closePipe("")
	for i := range c.Jobs {
		c.Jobs[i].Describe(d)
	}
}

func closeWriter(w io.Writer) error {
	c, ok := w.(io.WriteCloser)
	if ok {
		return c.Close()
	}
	// Not a write closer, so cannot be closed.
	return nil
}

func closeReader(r io.Reader) error {
	c, ok := r.(io.ReadCloser)
	if ok {
		return c.Close()
	}
	// Not a read closer, so cannot be closed.
	return nil
}

type rw struct {
	R io.Reader
	W io.Writer
}

func nPipes(r io.Reader, w io.Writer, n int) []rw {
	var p []rw
	for i := 0; i < n-1; i++ {
		rp, wp := io.Pipe()
		p = append(p, rw{R: r, W: wp})
		r = rp
	}
	p = append(p, rw{R: r, W: w})
	return p
}
