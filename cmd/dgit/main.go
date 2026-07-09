// Command dgit is a Git-like version control CLI for datasets.
package main

import (
	"fmt"
	"os"

	"github.com/joeymeij/datagit/internal/cli"
)

// version is stamped by the build (see Makefile ldflags).
var version = "dev"

func main() {
	if len(os.Args) > 1 && os.Args[1] == "version" {
		fmt.Printf("dgit %s\n", version)
		return
	}
	app := cli.NewApp(os.Stdout)
	if err := app.Run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "dgit: %v\n", err)
		os.Exit(1)
	}
}
