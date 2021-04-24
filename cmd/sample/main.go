package main

import (
	"context"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"strings"

	"github.com/m-lab/go/rtx"
	"github.com/stephen-soltesz/pipe/shx"
)

var (
	dryrun bool
)

func init() {
	flag.BoolVar(&dryrun, "dryrun", false, "enable dryrun mode for script")
}

func main() {
	flag.Parse()

	wd, err := os.Getwd()
	rtx.Must(err, "failed to get working directory")

	s := shx.New()
	s.Env = []string{}
	s.SetDir(wd)
	sc1 := shx.Script(
		shx.SetEnv("FOO", "TEST"),
		shx.Exec("pwd"),
		shx.Exec("env"),
		shx.Pipe(
			shx.System("ls -l"),
			shx.Exec("cat"),
			shx.WriteFile("output.log", 0777),
		),
	)
	sc2 := shx.Script(
		shx.SetEnv("SECOND", "VARIABLE"),
		shx.Exec("env"),
		shx.Pipe(
			shx.ReadFile("output.log"),
			shx.Exec("tr", "a-z", "A-Z"),
			shx.WriteFile("output2.log", 0777),
		),
		shx.Chdir(".."),
		shx.Exec("pwd"),
		shx.System("false"),
	)

	sc3 := shx.Script(
		sc1,
		sc2,
	)
	sc4 := shx.Script(
		shx.System("echo ok"),
		sc3,
		shx.Func("func_to_uppercase",
			func(ctx context.Context, s *shx.State) error {
				b, err := ioutil.ReadFile("output.log")
				if err != nil {
					return err
				}
				s.Stdout.Write([]byte(strings.ToUpper(string(b))))
				return nil
			}),
		// shx.System("false"),
	)
	if dryrun {
		d := &shx.Description{}
		sc4.Describe(d)
		fmt.Println(d.String())
		return
	}

	ctx := context.Background()
	err = sc4.Run(ctx, s)
	if err != nil {
		fmt.Println(err)
	}
}
