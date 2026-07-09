package cli

import (
	"fmt"
	"io"
	"sort"
)

func cmdRemote() *Command {
	return &Command{
		Name:    "remote",
		Summary: "Manage remotes (add, remove, list)",
		Usage:   "dgit remote [add <name> <url> | remove <name>]",
		Run: func(out io.Writer, args []string) error {
			r, err := openRepo()
			if err != nil {
				return err
			}
			defer r.Close()

			switch {
			case len(args) == 0:
				remotes, err := r.Remotes()
				if err != nil {
					return err
				}
				if len(remotes) == 0 {
					fmt.Fprintln(out, "no remotes configured")
					return nil
				}
				names := make([]string, 0, len(remotes))
				for n := range remotes {
					names = append(names, n)
				}
				sort.Strings(names)
				for _, n := range names {
					fmt.Fprintf(out, "%-12s %s\n", n, remotes[n])
				}
				return nil
			case args[0] == "add" && len(args) == 3:
				if err := r.AddRemote(args[1], args[2]); err != nil {
					return err
				}
				fmt.Fprintf(out, "added remote %s\n", args[1])
				return nil
			case args[0] == "remove" && len(args) == 2:
				if err := r.RemoveRemote(args[1]); err != nil {
					return err
				}
				fmt.Fprintf(out, "removed remote %s\n", args[1])
				return nil
			default:
				return fmt.Errorf("usage: dgit remote [add <name> <url> | remove <name>]")
			}
		},
	}
}

func cmdPublish() *Command {
	return &Command{
		Name:    "publish",
		Summary: "Upload a branch to a remote",
		Usage:   "dgit publish [<remote>] [<branch>]",
		Run: func(out io.Writer, args []string) error {
			if len(args) > 2 {
				return fmt.Errorf("usage: dgit publish [<remote>] [<branch>]")
			}
			remoteName, branch := "origin", ""
			if len(args) >= 1 {
				remoteName = args[0]
			}
			if len(args) == 2 {
				branch = args[1]
			}
			r, err := openRepo()
			if err != nil {
				return err
			}
			defer r.Close()
			res, err := r.Publish(remoteName, branch)
			if err != nil {
				return err
			}
			if res.UpToDate {
				fmt.Fprintf(out, "%s: already up to date\n", res.Branch)
				return nil
			}
			fmt.Fprintf(out, "published %s → %s (%d commits, %d chunks) at %s\n",
				res.Branch, remoteName, res.Commits, res.Chunks, res.CommitID[:12])
			return nil
		},
	}
}

func cmdFetch() *Command {
	return &Command{
		Name:    "fetch",
		Summary: "Download branches from a remote (tracking refs only)",
		Usage:   "dgit fetch [<remote>]",
		Run: func(out io.Writer, args []string) error {
			if len(args) > 1 {
				return fmt.Errorf("usage: dgit fetch [<remote>]")
			}
			remoteName := "origin"
			if len(args) == 1 {
				remoteName = args[0]
			}
			r, err := openRepo()
			if err != nil {
				return err
			}
			defer r.Close()
			results, err := r.Fetch(remoteName)
			if err != nil {
				return err
			}
			if len(results) == 0 {
				fmt.Fprintln(out, "remote has no branches")
				return nil
			}
			for _, res := range results {
				if res.UpToDate {
					fmt.Fprintf(out, "%s/%-16s up to date\n", remoteName, res.Branch)
				} else {
					fmt.Fprintf(out, "%s/%-16s %s (%d commits, %d chunks)\n",
						remoteName, res.Branch, res.CommitID[:12], res.Commits, res.Chunks)
				}
			}
			fmt.Fprintf(out, "merge with: dgit merge %s/<branch>\n", remoteName)
			return nil
		},
	}
}
