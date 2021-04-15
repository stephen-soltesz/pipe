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
/*
s := NewSession()
s.SetDir("foo")
s.SetEnvVar("key", "var")
Script(
	Pipe(
		"a",
		"b", "c"),
	s.System("pwd"),
)
*/

type Session struct {
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
func (s *Session) Path(path ...string) string {
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

// SetEnvVar sets the named environment variable to the given value in s.
func (s *Session) SetEnvVar(name, value string) {
	prefix := name + "="
	for i, kv := range s.Env {
		if strings.HasPrefix(kv, prefix) {
			s.Env[i] = prefix + value
			return
		}
	}
	s.Env = append(s.Env, prefix+value)
}

func (s *Session) SetDir(dir string) Task {
	return &funcTask{
		name: fmt.Sprintf("chdir(%s)", dir),
		task: func(ctx context.Context, s *State) error {
			s.Dir = s.Path(dir)
			return nil
		},
	}
}

type execSession struct {
	name string
	args []string

	m      sync.Mutex
	p      *os.Process
	cancel bool
}

func (f *execSession) Run(ctx context.Context, s *State) error {
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

func (f *execSession) Kill() {
	f.m.Lock()
	p := f.p
	f.cancel = true
	f.m.Unlock()
	if p != nil {
		p.Kill()
	}
}

func (f *execSession) String() string {
	return fmt.Sprintf("%s %s", f.name, strings.Join(f.args, " "))
}
