package profile

import (
	"fmt"
	"io"
	"math"
	"strings"
	"testing"

	"github.com/joeymeij/rowen/internal/tabular"
)

// sliceReader is an in-memory tabular.Reader for tests.
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

func almostEq(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestNumericStatsExact(t *testing.T) {
	r := &sliceReader{cols: []string{"x"}, rows: [][]string{{"2"}, {"4"}, {"4"}, {"4"}, {"5"}, {"5"}, {"7"}, {"9"}}}
	rep, err := Profile(r)
	if err != nil {
		t.Fatal(err)
	}
	c := rep.Columns[0]
	if !c.Numeric || c.Count != 8 {
		t.Fatalf("stats = %+v", c)
	}
	if c.Min != 2 || c.Max != 9 || !almostEq(c.Mean, 5) {
		t.Fatalf("min/max/mean = %v/%v/%v", c.Min, c.Max, c.Mean)
	}
	// Sample stddev of this classic dataset is ~2.138.
	if !almostEq(c.StdDev, math.Sqrt(32.0/7.0)) {
		t.Fatalf("stddev = %v", c.StdDev)
	}
	if !almostEq(c.Median, 4.5) {
		t.Fatalf("median = %v", c.Median)
	}
	if c.QuantilesFromSample {
		t.Fatal("small input must be exact, not sampled")
	}
}

func TestQuartilesInterpolation(t *testing.T) {
	r := &sliceReader{cols: []string{"x"}, rows: [][]string{{"1"}, {"2"}, {"3"}, {"4"}}}
	rep, err := Profile(r)
	if err != nil {
		t.Fatal(err)
	}
	c := rep.Columns[0]
	if !almostEq(c.Q1, 1.75) || !almostEq(c.Median, 2.5) || !almostEq(c.Q3, 3.25) {
		t.Fatalf("q1/med/q3 = %v/%v/%v", c.Q1, c.Median, c.Q3)
	}
}

func TestMissingValues(t *testing.T) {
	r := &sliceReader{cols: []string{"x", "y"}, rows: [][]string{{"1", ""}, {" ", "b"}, {"3", "c"}}}
	rep, err := Profile(r)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Columns[0].Missing != 1 || rep.Columns[1].Missing != 1 {
		t.Fatalf("missing = %d,%d", rep.Columns[0].Missing, rep.Columns[1].Missing)
	}
	if rep.Columns[0].Count != 2 {
		t.Fatalf("count = %d", rep.Columns[0].Count)
	}
}

func TestShortRowsCountAsMissing(t *testing.T) {
	r := &sliceReader{cols: []string{"x", "y"}, rows: [][]string{{"1"}, {"2", "b"}}}
	rep, err := Profile(r)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Columns[1].Missing != 1 {
		t.Fatalf("missing = %d, want 1", rep.Columns[1].Missing)
	}
}

func TestNonNumericColumn(t *testing.T) {
	r := &sliceReader{cols: []string{"s"}, rows: [][]string{{"a"}, {"b"}, {"a"}}}
	rep, err := Profile(r)
	if err != nil {
		t.Fatal(err)
	}
	c := rep.Columns[0]
	if c.Numeric || c.Distinct != 2 {
		t.Fatalf("stats = %+v", c)
	}
}

func TestMixedColumnDemotedToString(t *testing.T) {
	r := &sliceReader{cols: []string{"x"}, rows: [][]string{{"1"}, {"2"}, {"oops"}, {"4"}}}
	rep, err := Profile(r)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Columns[0].Numeric {
		t.Fatal("column with non-numeric value must not be numeric")
	}
	if rep.Columns[0].Distinct != 4 {
		t.Fatalf("distinct = %d", rep.Columns[0].Distinct)
	}
}

func TestDuplicateRows(t *testing.T) {
	r := &sliceReader{cols: []string{"a", "b"}, rows: [][]string{
		{"1", "x"}, {"1", "x"}, {"2", "y"}, {"1", "x"},
	}}
	rep, err := Profile(r)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Rows != 4 || rep.DuplicateRows != 2 {
		t.Fatalf("rows=%d dups=%d, want 4/2", rep.Rows, rep.DuplicateRows)
	}
}

func TestRowHashUnambiguous(t *testing.T) {
	// ("ab","c") must not collide with ("a","bc").
	r := &sliceReader{cols: []string{"a", "b"}, rows: [][]string{{"ab", "c"}, {"a", "bc"}}}
	rep, err := Profile(r)
	if err != nil {
		t.Fatal(err)
	}
	if rep.DuplicateRows != 0 {
		t.Fatal("distinct rows reported as duplicates")
	}
}

func TestEmptyInput(t *testing.T) {
	rep, err := Profile(&sliceReader{})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Rows != 0 || rep.Cols != 0 || len(rep.Columns) != 0 {
		t.Fatalf("rep = %+v", rep)
	}
}

func TestConstantColumnNoHistogram(t *testing.T) {
	r := &sliceReader{cols: []string{"x"}, rows: [][]string{{"5"}, {"5"}}}
	rep, err := Profile(r)
	if err != nil {
		t.Fatal(err)
	}
	c := rep.Columns[0]
	if c.Histogram != nil {
		t.Fatal("constant column must have no histogram")
	}
	if c.StdDev != 0 {
		t.Fatalf("stddev = %v", c.StdDev)
	}
}

func TestHistogramBuckets(t *testing.T) {
	var rows [][]string
	for i := 0; i <= 100; i++ {
		rows = append(rows, []string{fmt.Sprint(i)})
	}
	rep, err := Profile(&sliceReader{cols: []string{"x"}, rows: rows})
	if err != nil {
		t.Fatal(err)
	}
	h := rep.Columns[0].Histogram
	if len(h) != 10 {
		t.Fatalf("buckets = %d", len(h))
	}
	var total int64
	for _, b := range h {
		total += b.Count
	}
	if total != 101 {
		t.Fatalf("histogram total = %d, want 101", total)
	}
	if h[0].Lo != 0 || h[9].Hi != 100 {
		t.Fatalf("range = [%v,%v]", h[0].Lo, h[9].Hi)
	}
	// Max value must land in the last bucket, not overflow.
	if h[9].Count != 11 {
		t.Fatalf("last bucket = %d, want 11 (90..100)", h[9].Count)
	}
}

func TestDistinctCap(t *testing.T) {
	var rows [][]string
	for i := 0; i < distinctCap+50; i++ {
		rows = append(rows, []string{fmt.Sprintf("v%d", i)})
	}
	rep, err := Profile(&sliceReader{cols: []string{"s"}, rows: rows})
	if err != nil {
		t.Fatal(err)
	}
	c := rep.Columns[0]
	if !c.DistinctCapped || c.Distinct != distinctCap {
		t.Fatalf("distinct=%d capped=%v", c.Distinct, c.DistinctCapped)
	}
}

func TestReservoirSampling(t *testing.T) {
	// More values than sampleCap: quantiles must be flagged as sampled
	// and still be close to truth for a uniform sequence.
	var rows [][]string
	n := sampleCap + 20000
	for i := 0; i < n; i++ {
		rows = append(rows, []string{fmt.Sprint(i)})
	}
	rep, err := Profile(&sliceReader{cols: []string{"x"}, rows: rows})
	if err != nil {
		t.Fatal(err)
	}
	c := rep.Columns[0]
	if !c.QuantilesFromSample {
		t.Fatal("expected sampled quantiles")
	}
	mid := float64(n-1) / 2
	if math.Abs(c.Median-mid) > float64(n)*0.02 {
		t.Fatalf("median = %v, want ≈ %v", c.Median, mid)
	}
	if c.Count != int64(n) || c.Min != 0 || c.Max != float64(n-1) {
		t.Fatalf("exact stats wrong: %+v", c)
	}
}

func TestProfileFromRealCSV(t *testing.T) {
	r, err := tabular.ForFormat("csv", io.NopCloser(strings.NewReader("a,b\n1,x\n2,\n")))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	rep, err := Profile(r)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Rows != 2 || rep.Columns[1].Missing != 1 || !rep.Columns[0].Numeric {
		t.Fatalf("rep = %+v", rep)
	}
}
