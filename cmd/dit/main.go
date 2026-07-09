// Command dit is a Git-like version control CLI for datasets.
package main

import (
	"fmt"
	"os"

	"github.com/joeymeij/dit/internal/cli"
)

// version is stamped by the build (see Makefile ldflags).
var version = "dev"

func main() {
	if len(os.Args) > 1 && os.Args[1] == "version" {
		fmt.Printf("dit %s\n", version)
		return
	}
	app := cli.NewApp(os.Stdout)
	if err := app.Run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "dit: %v\n", err)
		os.Exit(1)
	}
}
