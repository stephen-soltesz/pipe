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
pipe
shell
shx
compose
gosh
script

ops.Sh
cmd.Sh
script
command / operation / task / job / jx
hybrid shell go program script


shx.Task

shx.Func
shx.Sh
shx.Exec
shx.Pipe
shx.Script
*/

type State struct {
	Stdin  io.ReadCloser
	Stdout io.WriteCloser
	Stderr io.WriteCloser
	Dir    string
	Env    []string
	DryRun bool
}

// Path returns the provided path relative to the state's current directory.
// If multiple arguments are provided, they're joined via filepath.Join.
// If path is absolute, it is taken by itself.
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
	for i, kv := range s.Env {
		if strings.HasPrefix(kv, prefix) {
			s.Env[i] = prefix + value
			return
		}
	}
	s.Env = append(s.Env, prefix+value)
}

func (s *State) Copy() *State {
	return &State{
		Stdin:  s.Stdin,
		Stdout: s.Stdout,
		Stderr: s.Stderr,
		Dir:    s.Dir,
		Env:    s.Env,
		DryRun: s.DryRun,
	}
}

type Task interface {
	Run(ctx context.Context, s *State) error
	Kill()
	String() string
}

type NopCloser struct {
	io.Writer
}

func (NopCloser) Close() error { return nil }

func Run(t Task) error {
	s := &State{
		Stdin:  os.Stdin,  // nil,
		Stdout: os.Stdout, // DiscardCloser{ioutil.Discard},
		Stderr: os.Stderr,
	}
	ctx := context.Background()
	return t.Run(ctx, s)
}

func Script(t ...Task) Task {
	return &scriptTask{
		Name:     "# Script",
		Subtasks: t,
	}
}

func Pipe(t ...Task) Task {
	return &pipeTask{
		Name:     "# Pipeline",
		Subtasks: t,
	}
}

func System(cmd string) Task {
	return &execTask{
		name: "/bin/sh",
		args: []string{"-c", cmd},
	}
}

func Exec(cmd string, args ...string) Task {
	return &execTask{
		name: cmd,
		args: args,
	}
}

func Func(name string, task func(ctx context.Context, s *State) error) Task {
	return &funcTask{
		name: name,
		task: task,
	}
}

/*
func IfThen(cond Task, yes Task) Task {
	return &funcTask{
		name: fmt.Sprintf("if (%s) ; then %s", cond.String(), yes.String()),
		task: func(ctx context.Context, s *State) error {
			if err := cond.Run(ctx, s); err == nil {
				return yes.Run(ctx, s)
			}
			return nil
		},
	}
}

func IfThenElse(cond Task, yes Task, no Task) Task {
	return &funcTask{
		name: fmt.Sprintf("if (%s) ; then %s else %s", cond.String(), yes.String(), no.String()),
		task: func(ctx context.Context, s *State) error {
			if err := cond.Run(ctx, s); err == nil {
				return yes.Run(ctx, s)
			} else {
				return no.Run(ctx, s)
			}
		},
	}
}

// True is a Task that unconditionally returns successfully.
func True() Task {
	return &funcTask{
		name: "true",
		task: func(ctx context.Context, s *State) error {
			return nil
		},
	}
}
*/

// Read reads data from r and writes it to the pipe's stdout.
func Read(r io.Reader) Task {
	return &funcTask{
		name: fmt.Sprintf("read(%v)", r),
		task: func(ctx context.Context, s *State) error {
			_, err := io.Copy(s.Stdout, r)
			return err
		},
	}
}

func ReadFile(path string) Task {
	return &funcTask{
		name: fmt.Sprintf("ReadFile(%s)", path),
		task: func(ctx context.Context, s *State) error {
			file, err := os.Open(s.Path(path))
			if err != nil {
				return err
			}
			_, err = io.Copy(s.Stdout, file)
			file.Close()
			return err
		},
	}
}

// Write writes to w the data read from the pipe's stdin.
func Write(w io.Writer) Task {
	return &funcTask{
		name: "write",
		task: func(ctx context.Context, s *State) error {
			_, err := io.Copy(w, s.Stdin)
			return err
		},
	}
}

func WriteFile(path string, perm os.FileMode) Task {
	return &funcTask{
		name: fmt.Sprintf("WriteFile(%s)", path),
		task: func(ctx context.Context, s *State) error {
			file, err := os.OpenFile(s.Path(path), os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
			if err != nil {
				return err
			}
			_, err = io.Copy(file, s.Stdin)
			file.Close()
			return err
		},
	}
}

func Chdir(dir string) Task {
	return &funcTask{
		name: fmt.Sprintf("chdir(%s)", dir),
		task: func(ctx context.Context, s *State) error {
			s.Dir = s.Path(dir)
			return nil
		},
	}
}

func SetEnv(name string, value string) Task {
	return &funcTask{
		name: fmt.Sprintf("export %s=%s", name, value),
		task: func(ctx context.Context, s *State) error {
			s.SetEnv(name, value)
			return nil
		},
	}
}

type scriptTask struct {
	Name     string
	Subtasks []Task
}

func (c *scriptTask) Run(ctx context.Context, s *State) error {
	restore, err := chdir(s.Path())
	if err != nil {
		return err
	}
	defer restore()

	for i := range c.Subtasks {
		logx.Debug.Println(c.Subtasks[i].String())
		// s1 := s.Copy()
		err := c.Subtasks[i].Run(ctx, s)
		if err != nil {
			return err
		}
	}
	return nil
}
func (c *scriptTask) Kill() {
	for i := range c.Subtasks {
		logx.Debug.Println("killing", c.Subtasks[i].String())
		c.Subtasks[i].Kill()
	}
}

func (c *scriptTask) String() string {
	s := []string{} // c.Name}
	for i := range c.Subtasks {
		s = append(s, fmt.Sprintf("line %2d: %s", i, c.Subtasks[i].String()))
	}
	return strings.Join(s, ";\n")
}

type pipeTask struct {
	Name     string
	Subtasks []Task
}

func (c *pipeTask) Run(ctx context.Context, z *State) error {
	restore, err := chdir(z.Path())
	if err != nil {
		return err
	}
	defer restore()

	logx.Debug.Println(c.String())
	e := c.Subtasks
	p := nPipes(z.Stdin, z.Stdout, len(e))
	s := make([]*State, len(e))
	for i := range e {
		s[i] = &State{
			Stdin:  p[i].R,
			Stdout: p[i].W,
			Stderr: os.Stderr,
			Dir:    z.Dir,
			Env:    z.Env,
			DryRun: z.DryRun,
		}
	}
	wg := sync.WaitGroup{}
	done := make(chan error, len(e))
	for i := len(e) - 1; i >= 0; i-- {
		wg.Add(1)
		go func(n, i int, e Task, s *State) {
			err := e.Run(ctx, s)
			// Send possible errors to outer loop.
			done <- err
			wg.Done()
			if i != 0 {
				s.Stdin.Close()
			}
			if i != n-1 {
				s.Stdout.Close()
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

func (c *pipeTask) Kill() {
	for i := range c.Subtasks {
		logx.Debug.Println("killing", c.Subtasks[i].String())
		c.Subtasks[i].Kill()
	}
}

func (c *pipeTask) String() string {
	s := []string{} // c.Name}
	for i := range c.Subtasks {
		s = append(s, c.Subtasks[i].String())
	}
	return strings.Join(s, " | ")
}

type funcTask struct {
	name string
	task func(ctx context.Context, s *State) error
}

func (f *funcTask) Run(ctx context.Context, s *State) error {
	restore, err := chdir(s.Path())
	if err != nil {
		return err
	}
	defer restore()
	logx.Debug.Println(f.String())
	return f.task(ctx, s)
}

func (f *funcTask) String() string {
	return f.name
}
func (f *funcTask) Kill() {
}

type execTask struct {
	name string
	args []string

	m      sync.Mutex
	p      *os.Process
	cancel bool
}

func (f *execTask) Run(ctx context.Context, s *State) error {
	f.m.Lock()
	if f.cancel {
		f.m.Unlock()
		return nil
	}
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
		logx.Debug.Printf("\tENV: %s", s.Env)
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

func (f *execTask) Kill() {
	f.m.Lock()
	p := f.p
	f.cancel = true
	f.m.Unlock()
	if p != nil {
		p.Kill()
	}
}

func (f *execTask) String() string {
	return fmt.Sprintf("%s %s", f.name, strings.Join(f.args, " "))
}

type rw struct {
	R io.ReadCloser
	W io.WriteCloser
}

func nPipes(r io.ReadCloser, w io.WriteCloser, n int) []rw {
	var p []rw
	for i := 0; i < n-1; i++ {
		rp, wp := io.Pipe()
		p = append(p, rw{R: r, W: wp})
		r = rp
	}
	p = append(p, rw{R: r, W: w})
	return p
}

func chdir(d string) (func() error, error) {
	logx.LogxDebug.Set("true")
	orig, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	f := func() error {
		return os.Chdir(orig)
	}
	if d == "" {
		return f, nil
	}
	logx.Debug.Printf("%s -> chdir(%s)", orig, d)
	err = os.Chdir(d)
	if err != nil {
		logx.Debug.Printf("chdir(%s) -> %s", d, err)
		return nil, err
	}
	return f, nil
}
