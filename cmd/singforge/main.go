package main

import (
	"fmt"
	"os"

	"github.com/howardshaw/singforge/internal/cli"
)

func main() {
	if err := cli.Run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "singforge:", err)
		os.Exit(1)
	}
}
