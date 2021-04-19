package shx

import (
	"bytes"
	"context"
	"io/ioutil"
	"log"
	"os"
	"path"
	"testing"
)

func init() {
	log.SetFlags(log.LUTC | log.Llongfile)
}

func Test_scriptJob_Run(t *testing.T) {
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
				//System("pwd"),
				//Chdir("/"),
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
					t.Fatalf("continued executing script after error.")
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
				Stdin:  os.Stdin,
				Stdout: b,
				Stderr: os.Stderr,
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

func Test_scriptJob_String(t *testing.T) {
	tests := []struct {
		name string
		t    []Job
		want string
	}{
		{
			name: "success",
			t: []Job{
				Chdir("/"),
				System("pwd"),
			},
			want: "# Script;\nline  0: chdir(/);\nline  1: /bin/sh -c pwd",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := Script(tt.t...)
			if got := c.String(); got != tt.want {
				t.Errorf("scriptJob.String() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_pipeJob_Run(t *testing.T) {
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

func Example() {
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
	// Output: FOO=BAR
}
