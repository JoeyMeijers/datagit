package quality

import (
	"fmt"
	"io"
	"strings"
	"testing"
)

type sliceReader struct {
	cols []string
	rows [][]string
	i    int
}

func (s *sliceReader) Columns() []string { return s.cols }
func (s *sliceReader) Close() error      { return nil }
func (s *sliceReader) Next() ([]string, error) {
	if s.i >= len(s.rows) {
		return nil, io.EOF
	}
	s.i++
	return s.rows[s.i-1], nil
}

func rules(rs ...Rule) RuleSet { return RuleSet{Rules: rs} }

func check(t *testing.T, r *sliceReader, rs RuleSet) []Violation {
	t.Helper()
	v, err := Check(r, rs)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func hasViolation(vs []Violation, rule, col string) bool {
	for _, v := range vs {
		if v.Rule == rule && v.Column == col {
			return true
		}
	}
	return false
}

func TestCleanDatasetPasses(t *testing.T) {
	r := &sliceReader{cols: []string{"id", "v"}, rows: [][]string{{"1", "a"}, {"2", "b"}, {"3", "c"}}}
	if vs := check(t, r, DefaultRules()); len(vs) != 0 {
		t.Fatalf("violations = %v", vs)
	}
}

func TestUnknownRuleRejected(t *testing.T) {
	if _, err := Check(&sliceReader{}, rules(Rule{Type: "frobnicate"})); err == nil {
		t.Fatal("unknown rule should fail")
	}
}

func TestUnknownColumnRejected(t *testing.T) {
	r := &sliceReader{cols: []string{"a"}}
	if _, err := Check(r, rules(Rule{Type: RuleUnique, Columns: []string{"nope"}})); err == nil {
		t.Fatal("unknown column should fail")
	}
}

func TestUnknownDatatypeRejected(t *testing.T) {
	r := &sliceReader{cols: []string{"a"}}
	if _, err := Check(r, rules(Rule{Type: RuleDatatype, Columns: []string{"a"}, Datatype: "quux"})); err == nil {
		t.Fatal("unknown datatype should fail")
	}
}

func TestMaxMissing(t *testing.T) {
	r := &sliceReader{cols: []string{"x"}, rows: [][]string{{""}, {""}, {"1"}, {""}}}
	vs := check(t, r, rules(Rule{Type: RuleMaxMissing, Ratio: 0.5}))
	if !hasViolation(vs, RuleMaxMissing, "x") {
		t.Fatalf("violations = %v", vs)
	}
	// 75% missing is fine with a 0.8 threshold.
	r = &sliceReader{cols: []string{"x"}, rows: [][]string{{""}, {""}, {"1"}, {""}}}
	if vs := check(t, r, rules(Rule{Type: RuleMaxMissing, Ratio: 0.8})); len(vs) != 0 {
		t.Fatalf("violations = %v", vs)
	}
}

func TestUnique(t *testing.T) {
	r := &sliceReader{cols: []string{"id"}, rows: [][]string{{"1"}, {"2"}, {"1"}, {"1"}}}
	vs := check(t, r, rules(Rule{Type: RuleUnique, Columns: []string{"id"}}))
	if !hasViolation(vs, RuleUnique, "id") {
		t.Fatalf("violations = %v", vs)
	}
	if !strings.Contains(vs[0].Message, "2 duplicate") {
		t.Fatalf("message = %q", vs[0].Message)
	}
	// Missing values do not violate uniqueness.
	r = &sliceReader{cols: []string{"id"}, rows: [][]string{{"1"}, {""}, {""}}}
	if vs := check(t, r, rules(Rule{Type: RuleUnique, Columns: []string{"id"}})); len(vs) != 0 {
		t.Fatalf("violations = %v", vs)
	}
}

func TestDatatype(t *testing.T) {
	r := &sliceReader{cols: []string{"age"}, rows: [][]string{{"30"}, {"abc"}, {"40"}, {"1.5"}}}
	vs := check(t, r, rules(Rule{Type: RuleDatatype, Columns: []string{"age"}, Datatype: "integer"}))
	if !hasViolation(vs, RuleDatatype, "age") || !strings.Contains(vs[0].Message, "2 values") {
		t.Fatalf("violations = %v", vs)
	}
	r = &sliceReader{cols: []string{"d"}, rows: [][]string{{"2024-01-01"}, {"not-a-date"}}}
	vs = check(t, r, rules(Rule{Type: RuleDatatype, Columns: []string{"d"}, Datatype: "date"}))
	if !hasViolation(vs, RuleDatatype, "d") {
		t.Fatalf("violations = %v", vs)
	}
}

func TestEmptyColumn(t *testing.T) {
	r := &sliceReader{cols: []string{"a", "b"}, rows: [][]string{{"1", ""}, {"2", ""}}}
	vs := check(t, r, rules(Rule{Type: RuleNoEmptyColumns}))
	if !hasViolation(vs, RuleNoEmptyColumns, "b") || hasViolation(vs, RuleNoEmptyColumns, "a") {
		t.Fatalf("violations = %v", vs)
	}
	// A zero-row dataset has no empty-column violations.
	if vs := check(t, &sliceReader{cols: []string{"a"}}, rules(Rule{Type: RuleNoEmptyColumns})); len(vs) != 0 {
		t.Fatalf("violations = %v", vs)
	}
}

func TestConstantColumn(t *testing.T) {
	r := &sliceReader{cols: []string{"c"}, rows: [][]string{{"x"}, {"x"}, {"x"}}}
	vs := check(t, r, rules(Rule{Type: RuleNoConstantColumns}))
	if !hasViolation(vs, RuleNoConstantColumns, "c") {
		t.Fatalf("violations = %v", vs)
	}
	// A single value is not "constant" (nothing to vary against).
	r = &sliceReader{cols: []string{"c"}, rows: [][]string{{"x"}}}
	if vs := check(t, r, rules(Rule{Type: RuleNoConstantColumns})); len(vs) != 0 {
		t.Fatalf("violations = %v", vs)
	}
}

func TestDuplicateRows(t *testing.T) {
	r := &sliceReader{cols: []string{"a", "b"}, rows: [][]string{{"1", "x"}, {"1", "x"}, {"2", "y"}}}
	vs := check(t, r, rules(Rule{Type: RuleNoDuplicateRows}))
	if len(vs) != 1 || !strings.Contains(vs[0].Message, "1 duplicate") {
		t.Fatalf("violations = %v", vs)
	}
}

func TestOutliers(t *testing.T) {
	rows := make([][]string, 0, 101)
	for i := 0; i < 100; i++ {
		rows = append(rows, []string{fmt.Sprint(100 + i%3)}) // tight cluster
	}
	rows = append(rows, []string{"100000"}) // massive outlier
	r := &sliceReader{cols: []string{"v"}, rows: rows}
	vs := check(t, r, rules(Rule{Type: RuleNoOutliers, ZScore: 4}))
	if !hasViolation(vs, RuleNoOutliers, "v") {
		t.Fatalf("violations = %v", vs)
	}
	// Non-numeric columns are skipped silently.
	r = &sliceReader{cols: []string{"s"}, rows: [][]string{{"a"}, {"b"}, {"c"}}}
	if vs := check(t, r, rules(Rule{Type: RuleNoOutliers})); len(vs) != 0 {
		t.Fatalf("violations = %v", vs)
	}
}

func TestColumnSelectorStar(t *testing.T) {
	r := &sliceReader{cols: []string{"a", "b"}, rows: [][]string{{"", ""}, {"", ""}}}
	vs := check(t, r, rules(Rule{Type: RuleMaxMissing, Ratio: 0.1, Columns: []string{"*"}}))
	if len(vs) != 2 {
		t.Fatalf("violations = %v, want 2", vs)
	}
}

func TestShortRowsCountAsMissing(t *testing.T) {
	r := &sliceReader{cols: []string{"a", "b"}, rows: [][]string{{"1"}, {"2"}}}
	vs := check(t, r, rules(Rule{Type: RuleNoEmptyColumns}))
	if !hasViolation(vs, RuleNoEmptyColumns, "b") {
		t.Fatalf("violations = %v", vs)
	}
}

func TestViolationString(t *testing.T) {
	v := Violation{Rule: "unique", Column: "id", Message: "boom"}
	if got := v.String(); !strings.Contains(got, "unique") || !strings.Contains(got, "id") {
		t.Fatalf("string = %q", got)
	}
	v = Violation{Rule: "no-duplicate-rows", Message: "boom"}
	if got := v.String(); strings.Contains(got, "column") {
		t.Fatalf("string = %q", got)
	}
}

func TestIsDateTime(t *testing.T) {
	for v, want := range map[string]bool{
		"2026-01-02T03:04:05Z":      true,
		"2026-01-02T03:04:05+02:00": true,
		"2026-01-02 03:04:05":       true,
		"2026-01-02T03:04:05":       true,
		"2026-01-02":                false,
		"":                          false,
		"2026-13-02 03:04:05":       false,
	} {
		if got := isDateTime(v); got != want {
			t.Errorf("isDateTime(%q) = %v, want %v", v, got, want)
		}
	}
}
