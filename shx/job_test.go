package shx_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path"
	"strings"
	"testing"
	"time"

	. "github.com/stephen-soltesz/pipe/shx"
)

func init() {
	log.SetFlags(log.LUTC | log.Llongfile)
}

func TestDescription(t *testing.T) {
	tests := []struct {
		name  string
		lines []string
		cmds  []string
		want  string
	}{
		{
			name:  "success-script",
			lines: []string{"env", "pwd"},
			want:  " 1: env\n 2: pwd\n",
		},
		{
			name: "success-pipe",
			cmds: []string{"env", "cat"},
			want: " 1: env | cat\n",
		},
		{
			name:  "success-script-pipe",
			lines: []string{"env", "pwd"},
			cmds:  []string{"env", "cat"},
			want:  " 1: env\n 2: pwd\n 3: env | cat\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := &Description{}
			for _, line := range tt.lines {
				d.Line(line)
			}
			closepipe := d.OpenPipe()
			for _, cmd := range tt.cmds {
				d.Line(cmd)
			}
			closepipe()
			v := d.String()
			if v != tt.want {
				t.Errorf("Description: wrong result; got %q, want %q", v, tt.want)
			}
		})
	}
}

func TestExec(t *testing.T) {
	tests := []struct {
		name    string
		cmd     string
		args    []string
		want    string
		wantErr bool
	}{
		{
			name: "success",
			cmd:  "/bin/echo",
			args: []string{"a", "b"},
			want: "a b\n",
		},
		{
			name:    "error-no-such-command",
			cmd:     "/not-a-dir/not-a-real-command",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			job := Exec(tt.cmd, tt.args...)
			ctx := context.Background()
			b := bytes.NewBuffer(nil)
			s := &State{
				Stdout: b,
			}
			err := job.Run(ctx, s)
			if (err != nil) != tt.wantErr {
				t.Errorf("Exec() = %v, want %t", err, tt.wantErr)
			}
			if b.String() != tt.want {
				t.Errorf("Exec() = got %v, want %v", b.String(), tt.want)
			}
		})
	}
}

func ExampleExec() {
	ex := Exec("echo", "a", "b")
	s := &State{
		Stdout: os.Stdout,
	}
	ctx := context.Background()
	err := ex.Run(ctx, s)
	if err != nil {
		panic(err)
	}
	// Output: a b
}

func ExampleSystem() {
	sys := System("echo a b")
	s := &State{
		Stdout: os.Stdout,
	}
	ctx := context.Background()
	err := sys.Run(ctx, s)
	if err != nil {
		panic(err)
	}
	// Output: a b
}

func TestFunc(t *testing.T) {
	count := 0
	tests := []struct {
		name string
		job  func(ctx context.Context, s *State) error
	}{
		{
			name: "success",
			job:  func(ctx context.Context, s *State) error { count++; return nil },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := Func(tt.name, tt.job)
			ctx := context.Background()
			s := &State{
				Stdout: os.Stdout,
			}
			err := f.Run(ctx, s)
			if err != nil {
				t.Errorf("Func() failed; got %v, want nil", err)
			}
		})
	}
	if count != 1 {
		t.Errorf("Func() count incorrect; got %d, want 1", count)
	}
}

func ExampleFunc() {
	f := Func("example", func(ctx context.Context, s *State) error {
		b, err := ioutil.ReadAll(s.Stdin)
		if err != nil {
			return err
		}
		_, err = s.Stdout.Write([]byte(base64.URLEncoding.EncodeToString(b)))
		return err
	})
	s := &State{
		Stdin:  bytes.NewBuffer([]byte(`{"key":"value"}\n`)),
		Stdout: os.Stdout,
	}
	ctx := context.Background()
	err := f.Run(ctx, s)
	if err != nil {
		panic(err)
	}
	// Output: eyJrZXkiOiJ2YWx1ZSJ9XG4=
}

func TestScript(t *testing.T) {
	tmpdir := t.TempDir()

	tests := []struct {
		name    string
		t       []Job
		want    string
		wantErr bool
	}{
		{
			name: "success",
			t: []Job{
				Chdir(tmpdir),
				System("pwd"),
			},
			want: tmpdir + "\n",
		},
		{
			name: "stop-after-error",
			t: []Job{
				// Force an error.
				System("exit 1"),
				Func("test-failure", func(ctx context.Context, s *State) error {
					t.Fatalf("script should not continue executing after error.")
					return nil
				}),
			},
			wantErr: true,
		},
		{
			name: "stop-after-deep-error",
			t: []Job{
				// Force an error within a sub-Script.
				Script(
					[]Job{
						System("exit 1"),
					}...,
				),
				Func("test-failure", func(ctx context.Context, s *State) error {
					t.Fatalf("script should not continue executing after error.")
					return nil
				}),
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			b := bytes.NewBuffer(nil)
			s := &State{
				Stdout: b,
			}
			sc := Script(tt.t...)
			err := sc.Run(ctx, s)
			if (err != nil) && !tt.wantErr {
				t.Fatalf("failed to run test: %s", err)
			}
			if b.String() != tt.want {
				t.Errorf("Script() wrong pwd output; got %s, want %s", b.String(), tt.want)
			}
		})
	}
}

func ExampleScript() {
	sc := Script(
		SetEnv("FOO", "BAR"),
		Exec("env"),
	)
	s := &State{
		Stdout: os.Stdout,
	}
	ctx := context.Background()
	err := sc.Run(ctx, s)
	if err != nil {
		panic(err)
	}
	d := &Description{}
	sc.Describe(d)
	fmt.Println(d.String())
	// Output: FOO=BAR
	//  1: (
	//  2:   export FOO=BAR
	//  3:   env
	//  4: )
}

func TestPipe(t *testing.T) {
	tmpdir := t.TempDir()

	tests := []struct {
		name    string
		t       []Job
		z       *State
		want    string
		wantErr bool
	}{
		{
			name: "okay",
			t: []Job{
				System("pwd"),
				System("cat"),
				WriteFile("output.log", 0666),
			},
			want: tmpdir + "\n",
		},
		{
			name: "success-readcloser-writecloser",
			t: []Job{
				Func(
					"reset-writer",
					func(ctx context.Context, s *State) error {
						s.Stdout = bytes.NewBuffer(nil)
						return nil
					}),
				Func(
					"reset-reader",
					func(ctx context.Context, s *State) error {
						s.Stdin = bytes.NewBuffer(nil)
						return nil
					}),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := Pipe(tt.t...)
			ctx := context.Background()
			s := New()
			s.Dir = tmpdir
			if err := c.Run(ctx, s); (err != nil) != tt.wantErr {
				t.Errorf("pipeJob.Run() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.want == "" {
				return
			}
			b, err := ioutil.ReadFile(path.Join(tmpdir, "output.log"))
			if err != nil && !tt.wantErr {
				t.Errorf("pipeJob.Run() readfile error = %v, want nil", err)
			}
			if string(b) != tt.want {
				t.Errorf("pipeJob.Run() wrong output = %q, want %q", string(b), tt.want)
			}
		})
	}
}

func ExamplePipe() {
	p := Pipe(
		Exec("ls"),
		Exec("tail", "-1"),
		Exec("wc", "-l"),
	)
	s := &State{
		Stdout: os.Stdout,
	}
	ctx := context.Background()
	err := p.Run(ctx, s)
	if err != nil {
		panic(err)
	}
	d := &Description{}
	p.Describe(d)
	fmt.Println(d.String())
	// Output: 1
	//  1: ls | tail -1 | wc -l

}

func TestReadWrite(t *testing.T) {
	tmpdir := t.TempDir()

	r, err := os.Open("/dev/zero")
	if err != nil {
		t.Fatal(err)
	}
	w, err := os.OpenFile("/dev/null", os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0666)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		t       []Job
		ctxErr  bool
		fileErr bool
	}{
		{
			name: "okay-readfile-writefile",
			t: []Job{
				ReadFile("/dev/zero"),
				WriteFile("/dev/null", 0666),
			},
			ctxErr: true,
		},
		{
			name: "error-readfile-writefile",
			t: []Job{
				ReadFile("/does-not-exist/foo"),
				WriteFile("/does-not-exist/bar", 0666),
			},
			fileErr: true,
		},
		{
			name: "okay-read-write",
			t: []Job{
				Read(r),
				Write(w),
			},
			ctxErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
			defer cancel()
			s := New()
			s.Dir = tmpdir

			c := Pipe(tt.t...)
			err := c.Run(ctx, s)
			if tt.fileErr && !strings.Contains(err.Error(), "no such file or directory") {
				t.Errorf("pipeJob.Run() wrong error = %v, want 'no such file or directory'", err)
			}
			if tt.ctxErr && err != context.DeadlineExceeded {
				t.Errorf("pipeJob.Run() wrong error = %v, want %v", err, context.DeadlineExceeded)
			}
		})
	}
}

func TestState(t *testing.T) {
	t.Run("SetState", func(t *testing.T) {
		s := New()
		origDir := s.Dir
		if p := s.SetDir("/"); p != origDir {
			t.Errorf("SetDir() wrong previous value; got %q, want %q", p, origDir)
		}
		s.SetEnv("FOO", "BAR")
		if p := s.GetEnv("FOO"); p != "BAR" {
			t.Errorf("SetEnv() found wrong value; got %q, want %q", p, "BAR")
		}
		// Set the same variable with a new value.
		s.SetEnv("FOO", "BAR2")
		if p := s.GetEnv("FOO"); p != "BAR2" {
			t.Errorf("SetEnv() found wrong value; got %q, want %q", p, "BAR2")
		}
		if p := s.GetEnv("NOTFOUND"); p != "" {
			t.Errorf("GetEnv() found value; got %q, want %q", p, "")
		}
		if p := s.Path(); p != "/" {
			t.Errorf("Path() wrong value; got %q, want %q", p, "/")
		}
		if p := s.Path("/"); p != "/" {
			t.Errorf("Path() wrong value; got %q, want %q", p, "/")
		}
		if p := s.Path("relative"); p != "/relative" {
			t.Errorf("Path() wrong value; got %q, want %q", p, "/relative")
		}
		if p := s.Path("relative", "path"); p != "/relative/path" {
			t.Errorf("Path() wrong value; got %q, want %q", p, "/relative/path")
		}
	})
}
