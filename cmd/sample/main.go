package main

import (
	"context"
	"flag"
	"fmt"
	"os"

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
	s.SetDryRun(dryrun)
	s.SetDir(wd)

	sc := shx.Script(
		shx.SetEnv("FOO", "TEST"),
		shx.Exec("pwd"),
		shx.Exec("env"),
		shx.Pipe(
			shx.Exec("ls"),
			shx.Exec("cat"),
			shx.WriteFile("output.log", 0777),
		),
		// Exec("false"),
	)
	ctx := context.Background()
	err = sc.Run(ctx, s)
	if err != nil {
		fmt.Println(err)
	}

}
