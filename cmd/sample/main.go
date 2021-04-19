package main

import (
	"context"
	"flag"
	"fmt"

	. "github.com/stephen-soltesz/pipe/shx"
)

var (
	dryrun bool
)

func init() {
	flag.BoolVar(&dryrun, "dryrun", false, "enable dryrun mode for script")
}

func main() {
	flag.Parse()

	s := New()
	s.SetDryRun(dryrun)
	// s.SetDir("/")

	sc := Script(
		SetEnv("FOO", "TEST"),
		Exec("pwd"),
		// Exec("/bin/false"),
		Exec("env"),
		Pipe(
			Exec("ls"),
			Exec("cat"),
			WriteFile("output.log", 0777),
		),
	)
	ctx := context.Background()
	err := sc.Run(ctx, s)
	if err != nil {
		fmt.Println(err)
	}

}
