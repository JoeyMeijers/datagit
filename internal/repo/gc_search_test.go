package repo

import (
	"os"
	"testing"
)

func TestGCRemovesUnreferencedObjects(t *testing.T) {
	r := newRepo(t)
	p := write(t, r, "a.csv", "x\n1\n2\n")
	if _, err := r.Add(p); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Commit("v1"); err != nil {
		t.Fatal(err)
	}

	// Stage a second version but never commit it, then re-stage the
	// original content: the middle version's chunk becomes garbage
	// only after staging no longer references it.
	write(t, r, "a.csv", "x\n999\n")
	if _, err := r.Add(p); err != nil {
		t.Fatal(err)
	}
	write(t, r, "a.csv", "x\n1\n2\n")
	if _, err := r.Add(p); err != nil {
		t.Fatal(err)
	}

	dry, err := r.GC(true)
	if err != nil {
		t.Fatal(err)
	}
	if dry.Removed != 1 || dry.FreedBytes <= 0 {
		t.Fatalf("dry run = %+v, want 1 removable", dry)
	}
	// Dry run must not delete anything.
	if bad, err := r.Verify(); err != nil || len(bad) != 0 {
		t.Fatalf("store changed by dry run: bad=%v err=%v", bad, err)
	}

	res, err := r.GC(false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Removed != 1 || res.Kept != dry.Kept {
		t.Fatalf("gc = %+v", res)
	}
	// Everything still referenced must remain intact and restorable.
	if bad, err := r.Verify(); err != nil || len(bad) != 0 {
		t.Fatalf("after gc: bad=%v err=%v", bad, err)
	}
	head, _ := r.Head()
	if _, err := r.Checkout(head, true); err != nil {
		t.Fatalf("checkout after gc: %v", err)
	}

	// A second run finds nothing.
	res, err = r.GC(false)
	if err != nil || res.Removed != 0 {
		t.Fatalf("second gc = %+v err=%v", res, err)
	}
}

func TestGCEmptyRepo(t *testing.T) {
	r := newRepo(t)
	res, err := r.GC(false)
	if err != nil || res.Removed != 0 || res.Kept != 0 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}

func TestSearchFindsPathAndColumns(t *testing.T) {
	r := newRepo(t)
	p1 := write(t, r, "income_survey.csv", "household,income\n1,50000\n")
	p2 := write(t, r, "weather.csv", "day,temp\nmon,20\n")
	for _, p := range []string{p1, p2} {
		if _, err := r.Add(p); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := r.Commit("v1"); err != nil {
		t.Fatal(err)
	}

	// Match by path.
	hits, err := r.Search("survey")
	if err != nil || len(hits) != 1 || hits[0].Path != "income_survey.csv" {
		t.Fatalf("hits=%v err=%v", hits, err)
	}
	// Match by column name, case-insensitive.
	hits, err = r.Search("TEMP")
	if err != nil || len(hits) != 1 || hits[0].Path != "weather.csv" {
		t.Fatalf("hits=%v err=%v", hits, err)
	}
	// income matches both file name and column.
	hits, err = r.Search("income")
	if err != nil || len(hits) != 1 {
		t.Fatalf("hits=%v err=%v", hits, err)
	}
	// No match.
	hits, err = r.Search("nonexistent")
	if err != nil || len(hits) != 0 {
		t.Fatalf("hits=%v err=%v", hits, err)
	}
	// Empty term is an error.
	if _, err := r.Search("  "); err == nil {
		t.Fatal("empty term should fail")
	}
}

func TestSearchReturnsLatestVersion(t *testing.T) {
	r := newRepo(t)
	p := write(t, r, "d.csv", "a\n1\n")
	if _, err := r.Add(p); err != nil {
		t.Fatal(err)
	}
	c1, err := r.Commit("v1")
	if err != nil {
		t.Fatal(err)
	}
	write(t, r, "d.csv", "a\n1\n2\n3\n")
	if _, err := r.Add(p); err != nil {
		t.Fatal(err)
	}
	c2, err := r.Commit("v2")
	if err != nil {
		t.Fatal(err)
	}
	if c1.CreatedAt.Equal(c2.CreatedAt) {
		t.Skip("commits share a timestamp; ordering undefined at 1s resolution")
	}
	hits, err := r.Search("d.csv")
	if err != nil || len(hits) != 1 {
		t.Fatalf("hits=%v err=%v", hits, err)
	}
	if hits[0].CommitID != c2.ID || hits[0].Entry.Rows != 3 {
		t.Fatalf("hit = %+v, want newest commit %s", hits[0], c2.ID[:8])
	}
}

func TestLoadRulesDefaultAndCustom(t *testing.T) {
	r := newRepo(t)
	rs, err := r.LoadRules()
	if err != nil || len(rs.Rules) == 0 {
		t.Fatalf("default rules: %+v err=%v", rs, err)
	}

	cfg := `{"rules":[{"type":"unique","columns":["id"]}]}`
	if err := os.WriteFile(r.Root+"/.rowen/config/rules.json", []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	rs, err = r.LoadRules()
	if err != nil || len(rs.Rules) != 1 || rs.Rules[0].Type != "unique" {
		t.Fatalf("custom rules: %+v err=%v", rs, err)
	}

	// Invalid JSON fails loudly.
	if err := os.WriteFile(r.Root+"/.rowen/config/rules.json", []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := r.LoadRules(); err == nil {
		t.Fatal("invalid rules.json should fail")
	}
}
