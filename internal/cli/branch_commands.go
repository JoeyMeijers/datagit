package cli

import (
	"fmt"
	"io"
)

func cmdBranch() *Command {
	return &Command{
		Name:    "branch",
		Summary: "List, create, or delete branches",
		Usage:   "dgit branch [-d] [<name>]",
		Run: func(out io.Writer, args []string) error {
			fs := newFlagSet("branch", out)
			del := fs.Bool("d", false, "delete the named branch")
			if err := fs.Parse(args); err != nil {
				return err
			}
			r, err := openRepo()
			if err != nil {
				return err
			}
			defer r.Close()

			switch {
			case *del:
				if fs.NArg() != 1 {
					return fmt.Errorf("usage: dgit branch -d <name>")
				}
				if err := r.DeleteBranch(fs.Arg(0)); err != nil {
					return err
				}
				fmt.Fprintf(out, "deleted branch %s\n", fs.Arg(0))
			case fs.NArg() == 1:
				if err := r.CreateBranch(fs.Arg(0)); err != nil {
					return err
				}
				fmt.Fprintf(out, "created branch %s\n", fs.Arg(0))
			case fs.NArg() == 0:
				branches, err := r.Branches()
				if err != nil {
					return err
				}
				if len(branches) == 0 {
					fmt.Fprintln(out, "no branches yet (commit first)")
					return nil
				}
				for _, b := range branches {
					marker := " "
					if b.Current {
						marker = "*"
					}
					fmt.Fprintf(out, "%s %-20s %s\n", marker, b.Name, b.CommitID[:12])
				}
			default:
				return fmt.Errorf("usage: dgit branch [-d] [<name>]")
			}
			return nil
		},
	}
}

func cmdMerge() *Command {
	return &Command{
		Name:    "merge",
		Summary: "Fast-forward the current branch to another branch",
		Usage:   "dgit merge <branch>",
		Run: func(out io.Writer, args []string) error {
			if len(args) != 1 {
				return fmt.Errorf("usage: dgit merge <branch>")
			}
			r, err := openRepo()
			if err != nil {
				return err
			}
			defer r.Close()
			res, err := r.Merge(args[0])
			if err != nil {
				return err
			}
			switch res.Status {
			case "up-to-date":
				fmt.Fprintln(out, "already up to date")
			case "fast-forward":
				fmt.Fprintf(out, "fast-forwarded to %s (%d files)\n", res.CommitID[:12], res.Files)
			}
			return nil
		},
	}
}
