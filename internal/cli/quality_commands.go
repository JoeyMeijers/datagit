package cli

import (
	"errors"
	"fmt"
	"io"

	"github.com/joeymeij/dit/internal/quality"
	"github.com/joeymeij/dit/internal/tabular"
)

func cmdCheck() *Command {
	return &Command{
		Name:    "check",
		Summary: "Validate dataset files against quality rules",
		Usage:   "dit check <file>...",
		Run: func(out io.Writer, args []string) error {
			if len(args) == 0 {
				return fmt.Errorf("usage: dit check <file>...")
			}
			r, err := openRepo()
			if err != nil {
				return err
			}
			defer r.Close()
			rules, err := r.LoadRules()
			if err != nil {
				return err
			}

			var total int
			for _, path := range args {
				tr, err := tabular.Open(path)
				if errors.Is(err, tabular.ErrUnsupported) {
					fmt.Fprintf(out, "%s: skipped (%s not checkable)\n", path, tabular.FormatOf(path))
					continue
				}
				if err != nil {
					return err
				}
				violations, err := quality.Check(tr, rules)
				tr.Close()
				if err != nil {
					return fmt.Errorf("checking %s: %w", path, err)
				}
				if len(violations) == 0 {
					fmt.Fprintf(out, "%s: OK\n", path)
					continue
				}
				fmt.Fprintf(out, "%s:\n", path)
				for _, v := range violations {
					fmt.Fprintf(out, "  %s\n", v)
				}
				total += len(violations)
			}
			if total > 0 {
				return fmt.Errorf("%d rule violation(s)", total)
			}
			return nil
		},
	}
}

func cmdSearch() *Command {
	return &Command{
		Name:    "search",
		Summary: "Find datasets by path or column name",
		Usage:   "dit search <term>",
		Run: func(out io.Writer, args []string) error {
			if len(args) != 1 {
				return fmt.Errorf("usage: dit search <term>")
			}
			r, err := openRepo()
			if err != nil {
				return err
			}
			defer r.Close()
			hits, err := r.Search(args[0])
			if err != nil {
				return err
			}
			if len(hits) == 0 {
				fmt.Fprintln(out, "no matches")
				return nil
			}
			for _, h := range hits {
				fmt.Fprintf(out, "%-30s %s  %s", h.Path, h.CommitID[:12], h.Entry.Format)
				if h.Entry.Rows > 0 || h.Entry.Cols > 0 {
					fmt.Fprintf(out, ", %d rows × %d cols", h.Entry.Rows, h.Entry.Cols)
				}
				fmt.Fprintln(out)
			}
			return nil
		},
	}
}

func cmdGC() *Command {
	return &Command{
		Name:    "gc",
		Summary: "Delete objects not referenced by any commit",
		Usage:   "dit gc [--dry-run]",
		Run: func(out io.Writer, args []string) error {
			fs := newFlagSet("gc", out)
			dry := fs.Bool("dry-run", false, "report without deleting")
			if err := fs.Parse(args); err != nil {
				return err
			}
			r, err := openRepo()
			if err != nil {
				return err
			}
			defer r.Close()
			res, err := r.GC(*dry)
			if err != nil {
				return err
			}
			verb := "removed"
			if *dry {
				verb = "would remove"
			}
			fmt.Fprintf(out, "%s %d object(s), freeing %d bytes (%d kept)\n",
				verb, res.Removed, res.FreedBytes, res.Kept)
			return nil
		},
	}
}
