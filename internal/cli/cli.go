// Package cli implements dit's command-line interface using only the
// standard library. See ADR-0001 for why Cobra is not (yet) used; the
// command table below keeps a later migration mechanical.
package cli

import (
	"flag"
	"fmt"
	"io"
	"sort"
)

// Command is one dit subcommand.
type Command struct {
	Name    string
	Summary string
	Usage   string
	// Run executes the command. args are the arguments after the
	// subcommand name; out is where normal output is written.
	Run func(out io.Writer, args []string) error
}

// App dispatches subcommands.
type App struct {
	commands map[string]*Command
	out      io.Writer
}

// NewApp builds the application with all built-in commands registered.
func NewApp(out io.Writer) *App {
	a := &App{commands: map[string]*Command{}, out: out}
	for _, c := range builtins() {
		a.commands[c.Name] = c
	}
	return a
}

// Run executes the subcommand named in args[0]. With no arguments it
// prints usage and returns an error so the process exits non-zero.
func (a *App) Run(args []string) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		a.printUsage()
		if len(args) == 0 {
			return fmt.Errorf("no command given")
		}
		return nil
	}
	cmd, ok := a.commands[args[0]]
	if !ok {
		a.printUsage()
		return fmt.Errorf("unknown command %q", args[0])
	}
	return cmd.Run(a.out, args[1:])
}

func (a *App) printUsage() {
	fmt.Fprintln(a.out, "dit — Git-like version control for datasets")
	fmt.Fprintln(a.out, "\nUsage: dit <command> [arguments]")
	fmt.Fprintln(a.out, "\nCommands:")
	names := make([]string, 0, len(a.commands))
	for n := range a.commands {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		fmt.Fprintf(a.out, "  %-10s %s\n", n, a.commands[n].Summary)
	}
}

// newFlagSet builds a flag.FlagSet that reports errors instead of
// calling os.Exit, keeping commands testable.
func newFlagSet(name string, out io.Writer) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(out)
	return fs
}
