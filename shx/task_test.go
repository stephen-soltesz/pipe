package shx

import (
	"bytes"
	"context"
	"log"
	"os"
	"testing"
)

func init() {
	log.SetFlags(log.LUTC | log.Llongfile)
}

func TestScript(t *testing.T) {
	tests := []struct {
		name string
		t    []Task
		want string
	}{
		{
			name: "success",
			t: []Task{
				Chdir("../"),
				System("pwd"),
				Chdir("/"),
				System("pwd"),
			},
			want: "/Users/soltesz/src/github.com/stephen-soltesz/pipe\n/\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			b := bytes.NewBuffer(nil)
			s := &State{
				Stdin:  os.Stdin,
				Stdout: NopCloser{b},
				Stderr: os.Stderr,
			}
			sc := Script(tt.t...)
			err := sc.Run(ctx, s)
			if err != nil {
				t.Fatalf("failed to run test: %s", err)
			}
			// t.Log(err)
			if b.String() != tt.want {
				t.Errorf("Script() wrong pwd output; got %s, want %s", b.String(), tt.want)
			}
			// fmt.Println(b.String())

			//t.Errorf("Script() = %v, want %v", got, tt.want)
			//}
		})
	}
}
