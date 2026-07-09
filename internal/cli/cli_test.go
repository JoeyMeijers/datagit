package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// run executes the app in dir and returns its output.
func run(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(wd) //nolint:errcheck

	var buf bytes.Buffer
	err = NewApp(&buf).Run(args)
	return buf.String(), err
}

func TestUnknownCommand(t *testing.T) {
	if _, err := run(t, t.TempDir(), "frobnicate"); err == nil {
		t.Fatal("unknown command should fail")
	}
}

func TestNoArgsShowsUsageAndFails(t *testing.T) {
	out, err := run(t, t.TempDir())
	if err == nil {
		t.Fatal("no args should return an error")
	}
	if !strings.Contains(out, "Usage:") {
		t.Fatalf("usage not printed: %q", out)
	}
}

func TestHelp(t *testing.T) {
	out, err := run(t, t.TempDir(), "help")
	if err != nil {
		t.Fatal(err)
	}
	for _, cmd := range []string{"init", "add", "commit", "log", "status", "checkout", "schema", "verify"} {
		if !strings.Contains(out, cmd) {
			t.Errorf("help missing command %s", cmd)
		}
	}
}

func TestCommandsOutsideRepoFail(t *testing.T) {
	dir := t.TempDir()
	for _, args := range [][]string{
		{"status"}, {"log"}, {"add", "x.csv"}, {"commit", "-m", "x"}, {"verify"},
	} {
		if _, err := run(t, dir, args...); err == nil {
			t.Errorf("%v outside a repo should fail", args)
		}
	}
}

// TestFullWorkflow drives the CLI end to end: init → add → commit →
// status → log → modify → add → commit → schema → checkout → verify.
func TestFullWorkflow(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	csv := filepath.Join(dir, "population.csv")
	if err := os.WriteFile(csv, []byte("region,population\nnorth,100\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := run(t, dir, "init")
	if err != nil || !strings.Contains(out, "Initialized") {
		t.Fatalf("init: %v %q", err, out)
	}

	out, err = run(t, dir, "add", "population.csv")
	if err != nil || !strings.Contains(out, "added population.csv") {
		t.Fatalf("add: %v %q", err, out)
	}

	out, err = run(t, dir, "status")
	if err != nil || !strings.Contains(out, "staged") {
		t.Fatalf("status: %v %q", err, out)
	}

	if _, err = run(t, dir, "commit", "-m", "Initial import"); err != nil {
		t.Fatalf("commit: %v", err)
	}

	out, err = run(t, dir, "log")
	if err != nil || !strings.Contains(out, "Initial import") {
		t.Fatalf("log: %v %q", err, out)
	}

	// Second version.
	if err := os.WriteFile(csv, []byte("region,population\nnorth,100\nsouth,250\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err = run(t, dir, "add", "population.csv"); err != nil {
		t.Fatal(err)
	}
	if _, err = run(t, dir, "commit", "-m", "Add south"); err != nil {
		t.Fatal(err)
	}

	out, err = run(t, dir, "schema", "population.csv")
	if err != nil || !strings.Contains(out, "integer") {
		t.Fatalf("schema: %v %q", err, out)
	}

	// Find first commit ID from log, diff it against HEAD, then roll back.
	out, _ = run(t, dir, "log")
	lines := strings.Split(out, "\n")
	var firstID string
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.HasPrefix(lines[i], "commit ") {
			firstID = strings.TrimPrefix(lines[i], "commit ")
			break
		}
	}
	if firstID == "" {
		t.Fatalf("no commit ID found in log: %q", out)
	}

	// diff old commit vs HEAD (implied second argument).
	out, err = run(t, dir, "diff", firstID[:8])
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	if !strings.Contains(out, "modified: population.csv") || !strings.Contains(out, "+1 added") {
		t.Fatalf("diff output: %q", out)
	}

	if _, err = run(t, dir, "checkout", firstID[:12]); err != nil {
		t.Fatalf("checkout: %v", err)
	}
	blob, err := os.ReadFile(csv)
	if err != nil {
		t.Fatal(err)
	}
	if string(blob) != "region,population\nnorth,100\n" {
		t.Fatalf("rollback content = %q", blob)
	}

	out, err = run(t, dir, "verify")
	if err != nil || !strings.Contains(out, "OK") {
		t.Fatalf("verify: %v %q", err, out)
	}

	if _, err = run(t, dir, "commit", "-m", "nothing"); err == nil {
		t.Fatal("commit with empty staging should fail")
	}

	// After rollback HEAD == first commit, so diff reports no changes.
	out, err = run(t, dir, "diff", firstID[:8])
	if err != nil || !strings.Contains(out, "no changes") {
		t.Fatalf("diff after rollback: %v %q", err, out)
	}
}

func TestProfileCommand(t *testing.T) {
	dir := t.TempDir()
	csv := filepath.Join(dir, "d.csv")
	if err := os.WriteFile(csv, []byte("x,label\n1,a\n2,b\n3,\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := run(t, dir, "profile", "d.csv")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"3 rows × 2 cols", "numeric", "string"} {
		if !strings.Contains(out, want) {
			t.Errorf("profile output missing %q: %q", want, out)
		}
	}
	out, err = run(t, dir, "profile", "--hist", "d.csv")
	if err != nil || !strings.Contains(out, "█") {
		t.Fatalf("hist: %v %q", err, out)
	}
	if _, err := run(t, dir, "profile", "nope.csv"); err == nil {
		t.Fatal("missing file should fail")
	}
	if _, err := run(t, dir, "profile"); err == nil {
		t.Fatal("no args should fail")
	}
}

func TestDiffCommandErrors(t *testing.T) {
	dir := t.TempDir()
	if _, err := run(t, dir, "init"); err != nil {
		t.Fatal(err)
	}
	// No args now means worktree vs HEAD: valid, and clean in a fresh repo.
	out, err := run(t, dir, "diff")
	if err != nil || !strings.Contains(out, "no changes") {
		t.Fatalf("no-arg diff: %v %q", err, out)
	}
	if _, err := run(t, dir, "diff", "abcd"); err == nil {
		t.Fatal("diff against a commit ref with no commits should fail")
	}
	if _, err := run(t, dir, "diff", "--rows", "-1"); err == nil {
		t.Fatal("negative --rows should fail")
	}
}

func TestDiffWorkTreeAndRowsCommand(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, dir, "init"); err != nil {
		t.Fatal(err)
	}
	csv := filepath.Join(dir, "d.csv")
	if err := os.WriteFile(csv, []byte("x\n1\n2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, dir, "add", "d.csv"); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, dir, "commit", "-m", "v1"); err != nil {
		t.Fatal(err)
	}

	// Uncommitted change shows up in the no-arg diff, with row content.
	if err := os.WriteFile(csv, []byte("x\n1\n3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := run(t, dir, "diff", "--rows", "10")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"modified: d.csv", "+ 3", "- 2"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q: %q", want, out)
		}
	}
}

func TestCheckCommand(t *testing.T) {
	dir := t.TempDir()
	if _, err := run(t, dir, "init"); err != nil {
		t.Fatal(err)
	}
	bad := filepath.Join(dir, "bad.csv")
	// Empty column + duplicate rows violate the default rules.
	if err := os.WriteFile(bad, []byte("x,empty\n1,\n1,\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := run(t, dir, "check", "bad.csv")
	if err == nil {
		t.Fatal("check with violations should exit non-zero")
	}
	if !strings.Contains(out, "no-empty-columns") || !strings.Contains(out, "no-duplicate-rows") {
		t.Fatalf("output: %q", out)
	}

	good := filepath.Join(dir, "good.csv")
	if err := os.WriteFile(good, []byte("x,y\n1,a\n2,b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err = run(t, dir, "check", "good.csv")
	if err != nil || !strings.Contains(out, "OK") {
		t.Fatalf("check: %v %q", err, out)
	}

	// Custom rules file drives behavior.
	rules := `{"rules":[{"type":"unique","columns":["x"]}]}`
	if err := os.WriteFile(filepath.Join(dir, ".datagit", "config", "rules.json"), []byte(rules), 0o644); err != nil {
		t.Fatal(err)
	}
	dup := filepath.Join(dir, "dup.csv")
	if err := os.WriteFile(dup, []byte("x\n1\n1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, dir, "check", "dup.csv"); err == nil {
		t.Fatal("unique violation should fail")
	}

	if _, err := run(t, dir, "check"); err == nil {
		t.Fatal("check without args should fail")
	}
}

func TestSearchAndGCCommands(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, dir, "init"); err != nil {
		t.Fatal(err)
	}
	csv := filepath.Join(dir, "income.csv")
	if err := os.WriteFile(csv, []byte("household,income\n1,100\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, dir, "add", "income.csv"); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, dir, "commit", "-m", "v1"); err != nil {
		t.Fatal(err)
	}

	out, err := run(t, dir, "search", "income")
	if err != nil || !strings.Contains(out, "income.csv") {
		t.Fatalf("search: %v %q", err, out)
	}
	out, err = run(t, dir, "search", "zzz")
	if err != nil || !strings.Contains(out, "no matches") {
		t.Fatalf("search miss: %v %q", err, out)
	}
	if _, err := run(t, dir, "search"); err == nil {
		t.Fatal("search without args should fail")
	}

	out, err = run(t, dir, "gc", "--dry-run")
	if err != nil || !strings.Contains(out, "would remove 0") {
		t.Fatalf("gc dry: %v %q", err, out)
	}
	out, err = run(t, dir, "gc")
	if err != nil || !strings.Contains(out, "removed 0") {
		t.Fatalf("gc: %v %q", err, out)
	}
}

func TestBranchAndMergeCommands(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, dir, "init"); err != nil {
		t.Fatal(err)
	}
	out, err := run(t, dir, "branch")
	if err != nil || !strings.Contains(out, "no branches yet") {
		t.Fatalf("branch list: %v %q", err, out)
	}

	csv := filepath.Join(dir, "d.csv")
	if err := os.WriteFile(csv, []byte("x\n1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, dir, "add", "d.csv"); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, dir, "commit", "-m", "v1"); err != nil {
		t.Fatal(err)
	}

	out, err = run(t, dir, "status")
	if err != nil || !strings.Contains(out, "on branch main") {
		t.Fatalf("status: %v %q", err, out)
	}

	if _, err := run(t, dir, "branch", "dev"); err != nil {
		t.Fatal(err)
	}
	out, err = run(t, dir, "branch")
	if err != nil || !strings.Contains(out, "* main") || !strings.Contains(out, "  dev") {
		t.Fatalf("branch list: %v %q", err, out)
	}

	// Work on dev, then fast-forward main onto it.
	if _, err := run(t, dir, "checkout", "dev"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(csv, []byte("x\n1\n2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, dir, "add", "d.csv"); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, dir, "commit", "-m", "v2"); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, dir, "checkout", "main"); err != nil {
		t.Fatal(err)
	}
	blob, _ := os.ReadFile(csv)
	if string(blob) != "x\n1\n" {
		t.Fatalf("main worktree = %q", blob)
	}
	out, err = run(t, dir, "merge", "dev")
	if err != nil || !strings.Contains(out, "fast-forwarded") {
		t.Fatalf("merge: %v %q", err, out)
	}
	blob, _ = os.ReadFile(csv)
	if string(blob) != "x\n1\n2\n" {
		t.Fatalf("after merge worktree = %q", blob)
	}
	out, err = run(t, dir, "merge", "dev")
	if err != nil || !strings.Contains(out, "already up to date") {
		t.Fatalf("re-merge: %v %q", err, out)
	}

	if _, err := run(t, dir, "branch", "-d", "dev"); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, dir, "merge"); err == nil {
		t.Fatal("merge without args should fail")
	}

	// switch -c creates and switches in one step.
	out, err = run(t, dir, "switch", "-c", "feature")
	if err != nil || !strings.Contains(out, "switched to branch feature") {
		t.Fatalf("switch -c: %v %q", err, out)
	}
	out, err = run(t, dir, "status")
	if err != nil || !strings.Contains(out, "on branch feature") {
		t.Fatalf("status: %v %q", err, out)
	}
	// Plain switch requires the branch to exist.
	if _, err := run(t, dir, "switch", "nope"); err == nil {
		t.Fatal("switch to missing branch should fail")
	}
	out, err = run(t, dir, "switch", "main")
	if err != nil || !strings.Contains(out, "switched to branch main") {
		t.Fatalf("switch back: %v %q", err, out)
	}
	// -c on an existing branch fails.
	if _, err := run(t, dir, "switch", "-c", "feature"); err == nil {
		t.Fatal("switch -c existing branch should fail")
	}
	if _, err := run(t, dir, "switch"); err == nil {
		t.Fatal("switch without args should fail")
	}
}

func TestCommitRequiresMessage(t *testing.T) {
	dir := t.TempDir()
	if _, err := run(t, dir, "init"); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, dir, "commit"); err == nil {
		t.Fatal("commit without -m should fail")
	}
}
