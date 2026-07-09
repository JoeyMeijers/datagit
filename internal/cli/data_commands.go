package cli

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/joeymeij/dit/internal/diff"
	"github.com/joeymeij/dit/internal/profile"
	"github.com/joeymeij/dit/internal/tabular"
)

func cmdDiff() *Command {
	return &Command{
		Name:    "diff",
		Summary: "Compare working tree or commits (files, schemas, rows)",
		Usage:   "dit diff [--rows N] [<old-commit> [<new-commit>]]",
		Run: func(out io.Writer, args []string) error {
			fs := newFlagSet("diff", out)
			rows := fs.Int("rows", 0, "show up to N added/removed rows per file")
			if err := fs.Parse(args); err != nil {
				return err
			}
			if fs.NArg() > 2 {
				return fmt.Errorf("usage: dit diff [--rows N] [<old-commit> [<new-commit>]]")
			}
			if *rows < 0 {
				return fmt.Errorf("--rows must be >= 0")
			}
			r, err := openRepo()
			if err != nil {
				return err
			}
			defer r.Close()

			var changes []diff.FileChange
			switch fs.NArg() {
			case 0: // working tree vs HEAD, like `git diff HEAD`
				changes, err = r.DiffWorkTree(*rows)
			case 1: // commit vs HEAD
				newRef, herr := r.Head()
				if herr != nil {
					return herr
				}
				if newRef == "" {
					return fmt.Errorf("no commits yet")
				}
				changes, err = r.Diff(fs.Arg(0), newRef, *rows)
			case 2:
				changes, err = r.Diff(fs.Arg(0), fs.Arg(1), *rows)
			}
			if err != nil {
				return err
			}
			printChanges(out, changes)
			return nil
		},
	}
}

func printChanges(out io.Writer, changes []diff.FileChange) {
	if len(changes) == 0 {
		fmt.Fprintln(out, "no changes")
		return
	}
	for _, ch := range changes {
		switch ch.Status {
		case "added":
			fmt.Fprintf(out, "added:    %s (%d bytes)\n", ch.Path, ch.New.Size)
		case "removed":
			fmt.Fprintf(out, "removed:  %s\n", ch.Path)
		case "modified":
			fmt.Fprintf(out, "modified: %s\n", ch.Path)
			for _, c := range ch.ColsAdded {
				fmt.Fprintf(out, "  + column %s (%s)\n", c.Name, c.Type)
			}
			for _, name := range ch.ColsRemoved {
				fmt.Fprintf(out, "  - column %s\n", name)
			}
			for _, tc := range ch.TypeChanges {
				fmt.Fprintf(out, "  ~ column %s: %s → %s\n", tc.Column, tc.Old, tc.New)
			}
			if ch.RowDiff {
				fmt.Fprintf(out, "  rows: +%d added, -%d removed (%d → %d)\n",
					ch.RowsAdded, ch.RowsRemoved, ch.Old.Rows, ch.New.Rows)
				printSample(out, "+", ch.SampleAdded, ch.RowsAdded)
				printSample(out, "-", ch.SampleRemoved, ch.RowsRemoved)
			} else {
				fmt.Fprintf(out, "  rows: %d → %d (content diff unavailable for %s)\n",
					ch.Old.Rows, ch.New.Rows, ch.New.Format)
			}
		}
	}
}

func printSample(out io.Writer, sign string, sample [][]string, total int64) {
	for _, row := range sample {
		fmt.Fprintf(out, "  %s %s\n", sign, strings.Join(row, ","))
	}
	if n := total - int64(len(sample)); len(sample) > 0 && n > 0 {
		fmt.Fprintf(out, "  %s ... %d more\n", sign, n)
	}
}

func cmdProfile() *Command {
	return &Command{
		Name:    "profile",
		Summary: "Compute column statistics for a dataset file",
		Usage:   "dit profile [--hist] <file>",
		Run: func(out io.Writer, args []string) error {
			fs := newFlagSet("profile", out)
			hist := fs.Bool("hist", false, "print histograms for numeric columns")
			if err := fs.Parse(args); err != nil {
				return err
			}
			if fs.NArg() != 1 {
				return fmt.Errorf("usage: dit profile [--hist] <file>")
			}
			path := fs.Arg(0)
			r, err := tabular.Open(path)
			if errors.Is(err, tabular.ErrUnsupported) {
				return fmt.Errorf("profile supports csv/tsv files (got %s)", tabular.FormatOf(path))
			}
			if err != nil {
				return err
			}
			defer r.Close()

			rep, err := profile.Profile(r)
			if err != nil {
				return err
			}
			printReport(out, path, rep, *hist)
			return nil
		},
	}
}

func printReport(out io.Writer, path string, rep profile.Report, hist bool) {
	fmt.Fprintf(out, "%s: %d rows × %d cols, %d duplicate rows\n\n", path, rep.Rows, rep.Cols, rep.DuplicateRows)
	fmt.Fprintf(out, "%-20s %-8s %8s %9s %10s %10s %10s %10s %10s\n",
		"column", "type", "missing", "distinct", "min", "max", "mean", "median", "stddev")
	for _, c := range rep.Columns {
		typ, distinct := "string", fmt.Sprint(c.Distinct)
		if c.DistinctCapped {
			distinct += "+"
		}
		if c.Numeric {
			typ = "numeric"
			fmt.Fprintf(out, "%-20s %-8s %8d %9s %10.4g %10.4g %10.4g %10.4g %10.4g\n",
				c.Name, typ, c.Missing, distinct, c.Min, c.Max, c.Mean, c.Median, c.StdDev)
		} else {
			fmt.Fprintf(out, "%-20s %-8s %8d %9s %10s %10s %10s %10s %10s\n",
				c.Name, typ, c.Missing, distinct, "-", "-", "-", "-", "-")
		}
	}
	if !hist {
		return
	}
	for _, c := range rep.Columns {
		if len(c.Histogram) == 0 {
			continue
		}
		var max int64
		for _, b := range c.Histogram {
			if b.Count > max {
				max = b.Count
			}
		}
		fmt.Fprintf(out, "\n%s", c.Name)
		if c.QuantilesFromSample {
			fmt.Fprint(out, " (sampled)")
		}
		fmt.Fprintln(out)
		for _, b := range c.Histogram {
			bar := strings.Repeat("█", int(b.Count*40/max))
			fmt.Fprintf(out, "  %10.4g – %-10.4g %8d %s\n", b.Lo, b.Hi, b.Count, bar)
		}
	}
}
