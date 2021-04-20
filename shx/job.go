package shx

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/m-lab/go/logx"
)

/*
shx.Job

shx.Func
shx.Exec
shx.Pipe
shx.Script
shx.System

s := New()
s.SetDryRun(true)
s.SetDir("/"")

sc := Script(
	SetEnv("FOO", "TEST")
	Chdir("/"),
	Exec("pwd"),
	Pipe(
		Exec("ls"),
		Exec("cat"),
		WriteFile("output.", 0777),
	),
)
sc.Run(ctx, s)

*/

// State is the shx Job configuration. Callers provide the first State
// instance, and as a Job executes it creates new State instances derived from
// the original, e.g. for Pipes and subcommands.
type State struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
	Dir    string
	Env    []string
	DryRun bool
}

// New creates a State instance using current process Stdin, Stdout, and Stderr.
// As well, the returned State includes the current process working directory
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

// SetDryRun sets the DryRun value for this State instance and returns the
// previous value.
func (s *State) SetDryRun(val bool) bool {
	prev := s.DryRun
	s.DryRun = val
	return prev
}

// SetDir sets the Dir value for this State instance and returns the previous value.
func (s *State) SetDir(dir string) string {
	prev := s.Dir
	s.Dir = dir
	return prev
}

// Path returns the provided path relative to the state's current directory. If
// multiple arguments are provided, they're joined via filepath.Join. If path is
// absolute, it is taken by itself.
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

// SetEnv sets the named environment variable to the given value in s.
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

// GetEnv gets the named environment variable from s. If name is not found, the
// empty value is returned. This is indistinguishable from a name set to the
// empty value.
func (s *State) GetEnv(name string) string {
	prefix := name + "="
	for _, kv := range s.Env {
		if strings.HasPrefix(kv, prefix) {
			return strings.TrimPrefix(kv, prefix)
		}
	}
	return ""
}

// Job is the primary interface supported by this package.
type Job interface {
	// Run executes the Job using the given State. A Job implementation should
	// terminate when the given context is cancelled.
	Run(ctx context.Context, s *State) error

	// String returns a readable representation of the Job operation.
	String() string
}

// Script creates a Job that executes the given Jobs in sequence. If any Job
// returns an error, execution stops.
func Script(t ...Job) Job {
	return &scriptJob{
		Name:     "# Script",
		Subtasks: t,
	}
}

// Pipe creates a Job that executes the given Jobs as a "shell pipeline",
// passing the output of the first to the input of the next, and so on.
// If any Job returns an error, the first error is returned.
func Pipe(t ...Job) Job {
	return &pipeJob{
		Name:     "# Pipeline",
		Subtasks: t,
	}
}

// Exec creates a Job to exec the given command.
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

// Func creates a Job that runs the given job function. Job functions should
// honor the passed context to support cancelation.
func Func(name string, job func(ctx context.Context, s *State) error) Job {
	return &funcJob{
		name: name,
		task: job,
	}
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

// NewReaderContext creates a context-aware io.Reader. This is helpful for
// making otherwise unbounded IO operations context-aware, e.g. io.Copy().
func NewReaderContext(ctx context.Context, r io.Reader) io.Reader {
	return &readerCtx{ctx: ctx, Reader: r}
}

// Read creates a Job that reads from the given reader and writes it to Job's
// stdout. Read creates a context-aware reader from the given reader.
func Read(r io.Reader) Job {
	return &funcJob{
		name: fmt.Sprintf("read(%v)", r),
		task: func(ctx context.Context, s *State) error {
			_, err := io.Copy(s.Stdout, NewReaderContext(ctx, r))
			return err
		},
	}
}

// ReadFile creates a Job that reads from the named file and writes it to the
// Job's stdout.
func ReadFile(path string) Job {
	return &funcJob{
		name: fmt.Sprintf("ReadFile(%s)", path),
		task: func(ctx context.Context, s *State) error {
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
// writer. Write creates a context-aware reader for the Job input reader.
func Write(w io.Writer) Job {
	return &funcJob{
		name: fmt.Sprintf("write(%v)", w),
		task: func(ctx context.Context, s *State) error {
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
		name: fmt.Sprintf("WriteFile(%s)", path),
		task: func(ctx context.Context, s *State) error {
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
		name: fmt.Sprintf("chdir(%s)", dir),
		task: func(ctx context.Context, s *State) error {
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
		task: func(ctx context.Context, s *State) error {
			s.SetEnv(name, value)
			return nil
		},
	}
}

type scriptJob struct {
	Name     string
	Subtasks []Job
}

func (c *scriptJob) Run(ctx context.Context, s *State) error {
	for i := range c.Subtasks {
		logx.Debug.Println(c.Subtasks[i].String())
		err := c.Subtasks[i].Run(ctx, s)
		if err != nil {
			str := c.stringUntil(i + 1)
			return fmt.Errorf("%s - %w", str, err)
		}
	}
	return nil
}

func (c *scriptJob) String() string {
	s := []string{c.Name}
	for i := range c.Subtasks {
		s = append(s, fmt.Sprintf("line %2d: %s", i, c.Subtasks[i].String()))
	}
	return strings.Join(s, ";\n")
}

func (c *scriptJob) stringUntil(v int) string {
	s := []string{}
	for i := 0; i < v; i++ {
		s = append(s, fmt.Sprintf("line %2d: %s", i, c.Subtasks[i].String()))
	}
	return strings.Join(s, ";\n")
}

type pipeJob struct {
	Name     string
	Subtasks []Job
}

func (c *pipeJob) Run(ctx context.Context, z *State) error {
	logx.Debug.Println(c.String())
	e := c.Subtasks
	p := nPipes(z.Stdin, z.Stdout, len(e))
	s := make([]*State, len(e))
	for i := range e {
		s[i] = &State{
			Stdin:  p[i].R,
			Stdout: p[i].W,
			Stderr: z.Stderr,
			Dir:    z.Dir,
			Env:    z.Env,
			DryRun: z.DryRun,
		}
	}
	wg := sync.WaitGroup{}
	done := make(chan error, len(e))
	for i := len(e) - 1; i >= 0; i-- {
		wg.Add(1)
		go func(n, i int, e Job, s *State) {
			err := e.Run(ctx, s)
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
	wg.Wait()

	for range e {
		var err error
		select {
		case err = <-done:
		case <-ctx.Done():
			err = <-done
		}
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

func (c *pipeJob) String() string {
	s := []string{}
	for i := range c.Subtasks {
		s = append(s, c.Subtasks[i].String())
	}
	return strings.Join(s, " | ")
}

type funcJob struct {
	name string
	task func(ctx context.Context, s *State) error
}

func (f *funcJob) Run(ctx context.Context, s *State) error {
	logx.Debug.Println(f.String())
	return f.task(ctx, s)
}

func (f *funcJob) String() string {
	return f.name
}

type execJob struct {
	name string
	args []string

	m sync.Mutex
	p *os.Process
}

func (f *execJob) Run(ctx context.Context, s *State) error {
	f.m.Lock()
	cmd := exec.CommandContext(ctx, f.name, f.args...)
	cmd.Dir = s.Dir
	cmd.Env = s.Env
	cmd.Stdin = s.Stdin
	cmd.Stdout = s.Stdout
	cmd.Stderr = s.Stderr
	if s.DryRun {
		f.m.Unlock()
		logx.Debug.Printf("Exec:")
		logx.Debug.Printf("\tCMD: %s", f.name)
		logx.Debug.Printf("\tARG: %s", f.args)
		logx.Debug.Printf("\tDIR: %s", s.Dir)
		// logx.Debug.Printf("\tENV: %s", s.Env)
		return nil
	}
	err := cmd.Start()
	f.p = cmd.Process
	f.m.Unlock()
	if err != nil {
		return err
	}
	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("%s %w", f.String(), err)
	}
	return nil
}

func (f *execJob) String() string {
	return fmt.Sprintf("%s %s", f.name, strings.Join(f.args, " "))
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
