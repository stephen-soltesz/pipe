package main

import (
	"context"
	"fmt"

	. "github.com/stephen-soltesz/pipe/shx"
)

func main() {
	s := New()
	// s.SetDryRun(true)
	// s.SetDir("/")

	sc := Script(
		SetEnv("FOO", "TEST"),
		Exec("pwd"),
		Exec("/bin/false"),
		Exec("env"),
		Pipe(
			Exec("ls"),
			Exec("cat"),
			WriteFile("output.log", 0777),
		),
	)
	ctx := context.Background()
	err := sc.Run(ctx, s)
	fmt.Println(err)

}
