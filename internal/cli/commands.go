package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/joeymeij/datagit/internal/repo"
)

func builtins() []*Command {
	return []*Command{
		cmdInit(), cmdAdd(), cmdCommit(), cmdLog(), cmdStatus(),
		cmdCheckout(), cmdSchema(), cmdVerify(), cmdDiff(), cmdProfile(),
		cmdCheck(), cmdSearch(), cmdGC(), cmdBranch(), cmdSwitch(), cmdMerge(),
		cmdRemote(), cmdPublish(), cmdFetch(),
	}
}

// openRepo opens the repository containing the current directory.
func openRepo() (*repo.Repo, error) {
	wd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	return repo.Open(wd)
}

func cmdInit() *Command {
	return &Command{
		Name:    "init",
		Summary: "Create an empty dgit repository in the current directory",
		Usage:   "dgit init",
		Run: func(out io.Writer, args []string) error {
			wd, err := os.Getwd()
			if err != nil {
				return err
			}
			if err := repo.Init(wd); err != nil {
				return err
			}
			fmt.Fprintf(out, "Initialized empty dgit repository in %s/%s\n", wd, repo.DirName)
			return nil
		},
	}
}

func cmdAdd() *Command {
	return &Command{
		Name:    "add",
		Summary: "Stage dataset files for the next commit",
		Usage:   "dgit add <file>...",
		Run: func(out io.Writer, args []string) error {
			if len(args) == 0 {
				return fmt.Errorf("usage: dgit add <file>...")
			}
			r, err := openRepo()
			if err != nil {
				return err
			}
			defer r.Close()
			for _, path := range args {
				e, err := r.Add(path)
				if err != nil {
					return err
				}
				fmt.Fprintf(out, "added %s (%s, %d bytes", e.Path, e.Format, e.Size)
				if e.Rows > 0 || e.Cols > 0 {
					fmt.Fprintf(out, ", %d rows × %d cols", e.Rows, e.Cols)
				}
				fmt.Fprintln(out, ")")
			}
			return nil
		},
	}
}

func cmdCommit() *Command {
	return &Command{
		Name:    "commit",
		Summary: "Record staged files as a new commit",
		Usage:   "dgit commit -m <message>",
		Run: func(out io.Writer, args []string) error {
			fs := newFlagSet("commit", out)
			msg := fs.String("m", "", "commit message (required)")
			if err := fs.Parse(args); err != nil {
				return err
			}
			if *msg == "" {
				return fmt.Errorf("usage: dgit commit -m <message>")
			}
			r, err := openRepo()
			if err != nil {
				return err
			}
			defer r.Close()
			c, err := r.Commit(*msg)
			if err != nil {
				return err
			}
			fmt.Fprintf(out, "[%s] %s (%d files)\n", c.ID[:12], c.Message, len(c.Files))
			return nil
		},
	}
}

func cmdLog() *Command {
	return &Command{
		Name:    "log",
		Summary: "Show commit history, newest first",
		Usage:   "dgit log",
		Run: func(out io.Writer, args []string) error {
			r, err := openRepo()
			if err != nil {
				return err
			}
			defer r.Close()
			commits, err := r.Log()
			if err != nil {
				return err
			}
			if len(commits) == 0 {
				fmt.Fprintln(out, "no commits yet")
				return nil
			}
			for _, c := range commits {
				fmt.Fprintf(out, "commit %s\n", c.ID)
				fmt.Fprintf(out, "Author: %s\n", c.Author)
				fmt.Fprintf(out, "Date:   %s\n", c.CreatedAt.Format("2006-01-02 15:04:05 MST"))
				fmt.Fprintf(out, "Files:  %d\n\n    %s\n\n", len(c.Files), c.Message)
			}
			return nil
		},
	}
}

func cmdStatus() *Command {
	return &Command{
		Name:    "status",
		Summary: "Show staged and modified files",
		Usage:   "dgit status",
		Run: func(out io.Writer, args []string) error {
			r, err := openRepo()
			if err != nil {
				return err
			}
			defer r.Close()
			if branch, err := r.CurrentBranch(); err == nil && branch != "" {
				fmt.Fprintf(out, "on branch %s\n", branch)
			} else if err == nil {
				fmt.Fprintln(out, "HEAD detached")
			}
			states, err := r.Status()
			if err != nil {
				return err
			}
			if len(states) == 0 {
				fmt.Fprintln(out, "working tree clean")
				return nil
			}
			for _, s := range states {
				fmt.Fprintf(out, "%-9s %s\n", s.State+":", s.Path)
			}
			return nil
		},
	}
}

func cmdCheckout() *Command {
	return &Command{
		Name:    "checkout",
		Summary: "Restore all files from a commit into the working tree",
		Usage:   "dgit checkout [--force] <commit>",
		Run: func(out io.Writer, args []string) error {
			fs := newFlagSet("checkout", out)
			force := fs.Bool("force", false, "discard staged changes and overwrite local edits")
			if err := fs.Parse(args); err != nil {
				return err
			}
			if fs.NArg() != 1 {
				return fmt.Errorf("usage: dgit checkout [--force] <commit>")
			}
			r, err := openRepo()
			if err != nil {
				return err
			}
			defer r.Close()
			c, err := r.Checkout(fs.Arg(0), *force)
			if err != nil {
				return err
			}
			fmt.Fprintf(out, "restored %d files from %s\n", len(c.Files), c.ID[:12])
			return nil
		},
	}
}

func cmdSchema() *Command {
	return &Command{
		Name:    "schema",
		Summary: "Show the detected schema of a file at HEAD",
		Usage:   "dgit schema <file>",
		Run: func(out io.Writer, args []string) error {
			if len(args) != 1 {
				return fmt.Errorf("usage: dgit schema <file>")
			}
			r, err := openRepo()
			if err != nil {
				return err
			}
			defer r.Close()
			head, err := r.Head()
			if err != nil {
				return err
			}
			if head == "" {
				return fmt.Errorf("no commits yet")
			}
			c, err := r.LoadCommit(head)
			if err != nil {
				return err
			}
			for _, f := range c.Files {
				if f.Path != args[0] {
					continue
				}
				if f.Schema == nil || len(f.Schema.Columns) == 0 {
					fmt.Fprintf(out, "%s: no schema detected (format: %s)\n", f.Path, f.Format)
					return nil
				}
				fmt.Fprintf(out, "%s (%s, %d rows)\n", f.Path, f.Format, f.Rows)
				for _, col := range f.Schema.Columns {
					fmt.Fprintf(out, "  %-24s %s\n", col.Name, col.Type)
				}
				return nil
			}
			return fmt.Errorf("%s is not tracked at HEAD", args[0])
		},
	}
}

func cmdVerify() *Command {
	return &Command{
		Name:    "verify",
		Summary: "Check integrity of every stored chunk and commit",
		Usage:   "dgit verify",
		Run: func(out io.Writer, args []string) error {
			r, err := openRepo()
			if err != nil {
				return err
			}
			defer r.Close()
			bad, err := r.Verify()
			if err != nil {
				return err
			}
			if len(bad) > 0 {
				for _, problem := range bad {
					fmt.Fprintln(out, problem)
				}
				return fmt.Errorf("%d integrity problem(s) found", len(bad))
			}
			fmt.Fprintln(out, "all objects and commits verified OK")
			return nil
		},
	}
}
