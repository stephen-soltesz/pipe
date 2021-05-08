package main

import (
	"context"
	"flag"
	"fmt"
	"io/ioutil"
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

	sc1 := shx.Script(
		shx.SetEnv("VARIABLE", "FIRST"),
		shx.Exec("pwd"),
		shx.Exec("env"),
		shx.Pipe(
			shx.System("ls -l"),
			shx.Exec("cat"),
			shx.WriteFile("output.log", 0777),
		),
		shx.SetEnvFromJob("VARIABLE",
			shx.System("echo $(( 10 * 13 ))"),
		),
		shx.Exec("env"),
		shx.Pipe(
			shx.Exec("cat", "output.log"),
			shx.Pipe(
				shx.Exec("echo", "ok"),
				shx.Exec("cat"),
			),
		),
		shx.SetEnvFromJob("VARIABLE",
			shx.Pipe(
				shx.Exec("cat", "output.log"),
				shx.Pipe(
					shx.Exec("echo", "ok"),
					shx.Exec("cat"),
				),
			),
		),
	)
	sc2 := shx.Script(
		shx.SetEnv("VARIABLE", "SECOND"),
		shx.Exec("env"),
		shx.Pipe(
			shx.ReadFile("output.log"),
			shx.Exec("tr", "a-z", "A-Z"),
			shx.WriteFile("output2.log", 0777),
		),
		shx.IfFileMissing("file-is-missing. ^ _ []",
			shx.Println("FILE IS MISSING!!")),
		shx.Chdir(".."),
		shx.Exec("pwd"),
		shx.Println("About to fail.."),
		shx.System("false"), // this will fail.
	)

	sc3 := shx.Script(
		sc1,
		sc2,
	)
	sc4 := shx.Script(
		shx.System("echo ok"),
		shx.IfVarEmpty("MISSING_VAR",
			shx.System("echo 'var was missing'"),
		),
		shx.IfFileMissing("foo.list",
			shx.System("echo 'file was missing'"),
		),
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
		fmt.Print(shx.Describe(sc4))
		return
	}

	ctx := context.Background()
	/*	wd, err := os.Getwd()
		rtx.Must(err, "failed to get working directory")
		s := shx.New()
		s.Env = []string{}
		s.SetDir(wd)
		err = sc4.Run(ctx, s)
		if err != nil {
			fmt.Println(err)
		}
	*/
	rtx.Must(shx.Run(ctx, sc4), "failed to run job")
}
