// Package shx provides shell-like operations for Go.
//
// A Job represents one or more operations. A single-operation Job may represent
// running a command (Exec, System) or reading or writing a file (ReadFile,
// WriteFile), or a custom function (Func). A multiple-operation Job combines
// several single operation jobs (Script) or pipeline (Pipe). Together, these
// allow the composition of more and more complex operations.
//
// When a Job runs, it uses State to control its input and output, as well as
// its working directory, environment.
//
// A Job Description is a human-readable representation using shell-like syntax.
// Because some operations have no shell equivalent, the result is only
// representative.
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

// Description is used to produce a representation of a Job. After collecting a
// description, serialize using the String() method.
type Description struct {
	Depth int
	desc  bytes.Buffer
	line  int
	pipe  bool
	idx   int
}

// Line adds a new line to the output buffer. If StartPipe() was previously
// called, then the command is formatted as part of a pipeline. Otherwise, the
// command is formatted as a stand alone line.
func (d *Description) Line(cmd string) {
	if d.pipe {
		if d.idx > 0 {
			d.desc.WriteString(" | " + cmd)
			return
		}
		d.idx++
		d.desc.WriteString(cmd)
		return
	}
	d.line++
	d.desc.WriteString(fmt.Sprintf("%2d: %s%s\n", d.line, prefix(d.Depth), cmd))
}

// StartPipe begins a line of a command pipeline. Calls to StartPipe() should be
// paired with EndPipe().
func (d *Description) StartPipe() {
	d.line++
	d.desc.WriteString(fmt.Sprintf("%2d: %s", d.line, prefix(d.Depth)))
	d.pipe = true
}

// EndPipe concludes a command pipeline. Calls to EndPipe() should be preceeded
// by a call to StartPipe().
func (d *Description) EndPipe() {
	d.pipe = false
	d.idx = 0
	d.desc.WriteString("\n")
}

// String
func (d *Description) String() string {
	return d.desc.String()
}

// New creates a State instance based on the current process state, using
// os.Stdin, os.Stdout, and os.Stderr, as well as the current working directory
// and environment, with dry run disabled.
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
	return &State{
		Stdin:  s.Stdin,
		Stdout: s.Stdout,
		Stderr: s.Stderr,
		Dir:    s.Dir,
		Env:    s.Env,
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
	// Run executes the Job using the given State. A Job should terminate when
	// the given context is cancelled.
	Run(ctx context.Context, s *State) error

	// Describe produces a readable representation of the Job operation. After
	// calling Describe, use Description.String() to report the result.
	Describe(d *Description)
}

// Exec creates a Job to execute the given command with the given arguments.
func Exec(cmd string, args ...string) Job {
	return &execJob{
		name: cmd,
		args: args,
	}
}

// System is an Exec job that interprets the given cmd using "/bin/sh".
func System(cmd string) Job {
	return &execJob{
		name: "/bin/sh",
		args: []string{"-c", cmd},
	}
}

type execJob struct {
	name string
	args []string
}

func (f *execJob) Run(ctx context.Context, s *State) error {
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

func (f *execJob) Describe(d *Description) {
	args := ""
	if len(f.args) > 0 {
		args = " " + strings.Join(f.args, " ")
	}
	d.Depth++
	d.Line(f.name + args)
	d.Depth--
}

// Func creates a Job that runs the given function. Job functions should honor
// the context to support cancelation.
func Func(name string, job func(ctx context.Context, s *State) error) Job {
	return &funcJob{
		name: name,
		job:  job,
	}
}

type funcJob struct {
	name string
	job  func(ctx context.Context, s *State) error
}

func (f *funcJob) Run(ctx context.Context, s *State) error {
	return f.job(ctx, s)
}

func (f *funcJob) Describe(d *Description) {
	d.Depth++
	d.Line(f.name)
	d.Depth--
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
	return &funcJob{
		name: fmt.Sprintf("read(%v)", r),
		job: func(ctx context.Context, s *State) error {
			_, err := io.Copy(s.Stdout, NewReaderContext(ctx, r))
			return err
		},
	}
}

// ReadFile creates a Job that reads from the named file and writes it to the
// Job's stdout.
func ReadFile(path string) Job {
	return &funcJob{
		name: fmt.Sprintf("cat < %s", path),
		job: func(ctx context.Context, s *State) error {
			file, err := os.Open(s.Path(path))
			if err != nil {
				return err
			}
			defer file.Close()
			_, err = io.Copy(s.Stdout, NewReaderContext(ctx, file))
			return err
		},
	}
}

// Write creates a Job that reads from the Job input and writes to the given
// writer. Write creates a context-aware reader from the Job input.
func Write(w io.Writer) Job {
	return &funcJob{
		name: fmt.Sprintf("write(%v)", w),
		job: func(ctx context.Context, s *State) error {
			_, err := io.Copy(w, NewReaderContext(ctx, s.Stdin))
			return err
		},
	}
}

// WriteFile creates a Job that reads from the Job input and writes to the named
// file. The output path is created if it does not exist and is truncated if it
// does.
func WriteFile(path string, perm os.FileMode) Job {
	return &funcJob{
		name: fmt.Sprintf("cat > %s", path),
		job: func(ctx context.Context, s *State) error {
			file, err := os.OpenFile(s.Path(path), os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
			if err != nil {
				return err
			}
			defer file.Close()
			_, err = io.Copy(file, NewReaderContext(ctx, s.Stdin))
			return err
		},
	}
}

// Chdir creates Job that changes the Job state Dir. This does not alter the
// process working directory.
func Chdir(dir string) Job {
	return &funcJob{
		name: fmt.Sprintf("cd %s", dir),
		job: func(ctx context.Context, s *State) error {
			s.Dir = s.Path(dir)
			return nil
		},
	}
}

// SetEnv creates a Job that changes to Job state Env by setting name=value.
// SetEnv is most helpful in Script() Jobs.
func SetEnv(name string, value string) Job {
	return &funcJob{
		name: fmt.Sprintf("export %s=%s", name, value),
		job: func(ctx context.Context, s *State) error {
			s.SetEnv(name, value)
			return nil
		},
	}
}

// Script creates a Job that executes the given Job parameters in sequence. If
// any Job returns an error, execution stops.
func Script(t ...Job) Job {
	return &scriptJob{
		Name: "# Script",
		Jobs: t,
	}
}

// ErrScriptExecError is the base error for Script errors.
var ErrScriptExecError = errors.New("script execution error")

type scriptJob struct {
	Name string
	Jobs []Job
}

func (c *scriptJob) Run(ctx context.Context, s *State) error {
	z := s.copy()
	for i := range c.Jobs {
		err := c.Jobs[i].Run(ctx, z)
		// Special handling for script errors.
		if err != nil && !errors.Is(err, ErrScriptExecError) {
			d := &Description{}
			c.Describe(d)
			str := d.String()
			return fmt.Errorf("%w:\n%s - %s", ErrScriptExecError, str, err.Error())
		}
		// All other errors.
		if err != nil {
			return err
		}
	}
	return nil
}

func (c *scriptJob) Describe(d *Description) {
	d.Depth++
	d.Line("(")
	for i := range c.Jobs {
		c.Jobs[i].Describe(d)
	}
	d.Line(")")
	d.Depth--
}

// Pipe creates a Job that executes the given Jobs as a "shell pipeline",
// passing the output of the first to the input of the next, and so on.
// If any Job returns an error, the first error is returned.
func Pipe(t ...Job) Job {
	return &pipeJob{
		Name: "# Pipeline",
		Jobs: t,
	}
}

type pipeJob struct {
	Name string
	Jobs []Job
}

func (c *pipeJob) Run(ctx context.Context, z *State) error {
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

func (c *pipeJob) Describe(d *Description) {
	d.Depth++
	d.StartPipe()
	for i := range c.Jobs {
		c.Jobs[i].Describe(d)
	}
	d.EndPipe()
	d.Depth--
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
