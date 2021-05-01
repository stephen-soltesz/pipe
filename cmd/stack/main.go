package main

import (
	"fmt"
)

type cmd struct {
	s string
}

type cmds []cmd

type line interface {
	Push(s string)
	Append(s string)
}

func main() {
	fmt.Println("ok")

}
