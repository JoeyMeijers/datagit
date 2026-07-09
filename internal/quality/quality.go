// Package quality validates datasets against configurable rules in a
// single streaming pass: missing values, duplicate keys, invalid
// datatypes, empty columns, constant columns, duplicate rows, and
// outliers. See ADR-0006 for rule semantics.
package quality

import (
	"fmt"
	"hash/fnv"
	"io"
	"math"
	"math/rand"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/joeymeij/dit/internal/tabular"
)

// Rule types. A RuleSet is a list of rules; each rule yields zero or
// more violations.
const (
	// RuleMaxMissing fails when a column's missing-value ratio exceeds
	// Ratio. Columns selects targets ("*" = all).
	RuleMaxMissing = "max-missing"
	// RuleUnique fails when a column contains duplicate non-missing values.
	RuleUnique = "unique"
	// RuleDatatype fails when values in a column do not parse as Datatype
	// (integer | float | boolean | date | datetime).
	RuleDatatype = "datatype"
	// RuleNoEmptyColumns fails for columns with no non-missing values.
	RuleNoEmptyColumns = "no-empty-columns"
	// RuleNoConstantColumns fails for columns with exactly one distinct value.
	RuleNoConstantColumns = "no-constant-columns"
	// RuleNoDuplicateRows fails when any row repeats exactly.
	RuleNoDuplicateRows = "no-duplicate-rows"
	// RuleNoOutliers fails for numeric values with |z-score| > ZScore
	// (default 4). Checked against a bounded sample for huge columns.
	RuleNoOutliers = "no-outliers"
)

// Rule is one configured validation rule.
type Rule struct {
	Type     string   `json:"type"`
	Columns  []string `json:"columns,omitempty"`  // default ["*"]
	Ratio    float64  `json:"ratio,omitempty"`    // max-missing
	Datatype string   `json:"datatype,omitempty"` // datatype
	ZScore   float64  `json:"zscore,omitempty"`   // no-outliers
}

// RuleSet is the rules applied to a dataset.
type RuleSet struct {
	Rules []Rule `json:"rules"`
}

// DefaultRules is applied when a repository has no rules.json.
func DefaultRules() RuleSet {
	return RuleSet{Rules: []Rule{
		{Type: RuleNoEmptyColumns},
		{Type: RuleNoConstantColumns},
		{Type: RuleNoDuplicateRows},
		{Type: RuleMaxMissing, Ratio: 0.5},
		{Type: RuleNoOutliers, ZScore: 4},
	}}
}

// Violation is one failed rule instance.
type Violation struct {
	Rule    string
	Column  string // empty for dataset-level rules
	Message string
}

func (v Violation) String() string {
	if v.Column == "" {
		return fmt.Sprintf("[%s] %s", v.Rule, v.Message)
	}
	return fmt.Sprintf("[%s] column %s: %s", v.Rule, v.Column, v.Message)
}

const (
	uniqueCap  = 1 << 22 // tracked values per unique-rule column
	sampleCap  = 100000  // outlier sample bound
	sampleSeed = 1
)

var datatypeParsers = map[string]func(string) bool{
	"integer":  func(v string) bool { _, err := strconv.ParseInt(v, 10, 64); return err == nil },
	"float":    func(v string) bool { _, err := strconv.ParseFloat(v, 64); return err == nil },
	"boolean":  func(v string) bool { s := strings.ToLower(v); return s == "true" || s == "false" },
	"date":     isDate,
	"datetime": isDateTime,
}

func isDate(v string) bool {
	for _, l := range []string{"2006-01-02", "2006/01/02"} {
		if _, err := time.Parse(l, v); err == nil {
			return true
		}
	}
	return false
}

func isDateTime(v string) bool {
	for _, l := range []string{time.RFC3339, "2006-01-02 15:04:05", "2006-01-02T15:04:05"} {
		if _, err := time.Parse(l, v); err == nil {
			return true
		}
	}
	return false
}

// Check validates the dataset streamed by r against rules, in one pass.
func Check(r tabular.Reader, rules RuleSet) ([]Violation, error) {
	cols := r.Columns()
	colIndex := map[string]int{}
	for i, c := range cols {
		colIndex[c] = i
	}

	// Validate rule configuration before reading any data.
	for _, rule := range rules.Rules {
		if err := validateRule(rule, colIndex); err != nil {
			return nil, err
		}
	}

	state := newCheckState(cols, rules)
	h := fnv.New128a()
	var rows int64
	for {
		rec, err := r.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("row %d: %w", rows+2, err)
		}
		rows++
		state.observeRow(rec, h)
	}
	return state.violations(rows), nil
}

func validateRule(rule Rule, colIndex map[string]int) error {
	switch rule.Type {
	case RuleMaxMissing, RuleUnique, RuleDatatype, RuleNoEmptyColumns,
		RuleNoConstantColumns, RuleNoDuplicateRows, RuleNoOutliers:
	default:
		return fmt.Errorf("unknown rule type %q", rule.Type)
	}
	if rule.Type == RuleDatatype {
		if _, ok := datatypeParsers[rule.Datatype]; !ok {
			return fmt.Errorf("rule %s: unknown datatype %q", rule.Type, rule.Datatype)
		}
	}
	for _, c := range rule.Columns {
		if c == "*" {
			continue
		}
		if _, ok := colIndex[c]; !ok {
			return fmt.Errorf("rule %s: unknown column %q", rule.Type, c)
		}
	}
	return nil
}

// targets expands a rule's column selector to column indices.
func targets(rule Rule, cols []string) []int {
	sel := rule.Columns
	if len(sel) == 0 {
		sel = []string{"*"}
	}
	var out []int
	for _, s := range sel {
		if s == "*" {
			for i := range cols {
				out = append(out, i)
			}
			continue
		}
		for i, c := range cols {
			if c == s {
				out = append(out, i)
			}
		}
	}
	return out
}

// colState accumulates everything any rule needs about one column.
type colState struct {
	name       string
	missing    int64
	nonMissing int64

	firstValue string
	constant   bool

	// unique rule
	trackUnique bool
	values      map[[16]byte]struct{}
	uniqueDups  int64
	uniqueBlown bool // cardinality exceeded uniqueCap; result unknown

	// datatype rule
	datatype string
	badType  int64

	// outlier rule
	trackOutliers bool
	zscore        float64
	numeric       bool
	count         int64
	mean, m2      float64
	sample        []float64
	seen          int64
	rng           *rand.Rand
}

type checkState struct {
	cols   []string
	states []*colState
	rules  RuleSet

	checkDupRows bool
	rowHashes    map[[16]byte]struct{}
	dupRows      int64
}

func newCheckState(cols []string, rules RuleSet) *checkState {
	s := &checkState{cols: cols, rules: rules}
	for _, name := range cols {
		s.states = append(s.states, &colState{name: name, constant: true, numeric: true})
	}
	for _, rule := range rules.Rules {
		switch rule.Type {
		case RuleNoDuplicateRows:
			s.checkDupRows = true
			s.rowHashes = make(map[[16]byte]struct{})
		case RuleUnique:
			for _, i := range targets(rule, cols) {
				s.states[i].trackUnique = true
				s.states[i].values = make(map[[16]byte]struct{})
			}
		case RuleDatatype:
			for _, i := range targets(rule, cols) {
				s.states[i].datatype = rule.Datatype
			}
		case RuleNoOutliers:
			z := rule.ZScore
			if z == 0 {
				z = 4
			}
			for _, i := range targets(rule, cols) {
				cs := s.states[i]
				cs.trackOutliers = true
				cs.zscore = z
				cs.rng = rand.New(rand.NewSource(sampleSeed))
			}
		}
	}
	return s
}

func (s *checkState) observeRow(rec []string, h fnv128) {
	if s.checkDupRows {
		key := rowKey(rec, h)
		if _, dup := s.rowHashes[key]; dup {
			s.dupRows++
		} else {
			s.rowHashes[key] = struct{}{}
		}
	}
	for i, cs := range s.states {
		v := ""
		if i < len(rec) {
			v = strings.TrimSpace(rec[i])
		}
		cs.observe(v, h)
	}
}

func (cs *colState) observe(v string, h fnv128) {
	if v == "" {
		cs.missing++
		return
	}
	if cs.nonMissing == 0 {
		cs.firstValue = v
	} else if cs.constant && v != cs.firstValue {
		cs.constant = false
	}
	cs.nonMissing++

	if cs.trackUnique && !cs.uniqueBlown {
		key := valueKey(v, h)
		if _, dup := cs.values[key]; dup {
			cs.uniqueDups++
		} else if len(cs.values) >= uniqueCap {
			cs.uniqueBlown = true
			cs.values = nil
		} else {
			cs.values[key] = struct{}{}
		}
	}
	if cs.datatype != "" && !datatypeParsers[cs.datatype](v) {
		cs.badType++
	}
	if cs.trackOutliers && cs.numeric {
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			cs.numeric = false // outlier rule silently skips non-numeric columns
			return
		}
		cs.count++
		delta := f - cs.mean
		cs.mean += delta / float64(cs.count)
		cs.m2 += delta * (f - cs.mean)
		cs.seen++
		if len(cs.sample) < sampleCap {
			cs.sample = append(cs.sample, f)
		} else if j := cs.rng.Int63n(cs.seen); j < sampleCap {
			cs.sample[j] = f
		}
	}
}

func (s *checkState) violations(rows int64) []Violation {
	var out []Violation
	for _, rule := range s.rules.Rules {
		switch rule.Type {
		case RuleNoDuplicateRows:
			if s.dupRows > 0 {
				out = append(out, Violation{Rule: rule.Type,
					Message: fmt.Sprintf("%d duplicate rows", s.dupRows)})
			}
		case RuleNoEmptyColumns:
			for _, i := range targets(rule, s.cols) {
				if cs := s.states[i]; rows > 0 && cs.nonMissing == 0 {
					out = append(out, Violation{Rule: rule.Type, Column: cs.name, Message: "column is empty"})
				}
			}
		case RuleNoConstantColumns:
			for _, i := range targets(rule, s.cols) {
				if cs := s.states[i]; cs.nonMissing > 1 && cs.constant {
					out = append(out, Violation{Rule: rule.Type, Column: cs.name,
						Message: fmt.Sprintf("constant value %q", cs.firstValue)})
				}
			}
		case RuleMaxMissing:
			for _, i := range targets(rule, s.cols) {
				cs := s.states[i]
				if rows == 0 {
					continue
				}
				ratio := float64(cs.missing) / float64(rows)
				if ratio > rule.Ratio {
					out = append(out, Violation{Rule: rule.Type, Column: cs.name,
						Message: fmt.Sprintf("%.1f%% missing (max %.1f%%)", ratio*100, rule.Ratio*100)})
				}
			}
		case RuleUnique:
			for _, i := range targets(rule, s.cols) {
				cs := s.states[i]
				if cs.uniqueBlown {
					out = append(out, Violation{Rule: rule.Type, Column: cs.name,
						Message: fmt.Sprintf("cardinality exceeds %d; uniqueness not verified", uniqueCap)})
				} else if cs.uniqueDups > 0 {
					out = append(out, Violation{Rule: rule.Type, Column: cs.name,
						Message: fmt.Sprintf("%d duplicate values", cs.uniqueDups)})
				}
			}
		case RuleDatatype:
			for _, i := range targets(rule, s.cols) {
				if cs := s.states[i]; cs.badType > 0 {
					out = append(out, Violation{Rule: rule.Type, Column: cs.name,
						Message: fmt.Sprintf("%d values are not %s", cs.badType, rule.Datatype)})
				}
			}
		case RuleNoOutliers:
			for _, i := range targets(rule, s.cols) {
				cs := s.states[i]
				if !cs.numeric || cs.count < 3 {
					continue
				}
				stddev := math.Sqrt(cs.m2 / float64(cs.count-1))
				if stddev == 0 {
					continue
				}
				var n int64
				var worst float64
				for _, f := range cs.sample {
					if z := math.Abs(f-cs.mean) / stddev; z > cs.zscore {
						n++
						worst = math.Max(worst, z)
					}
				}
				if n > 0 {
					msg := fmt.Sprintf("%d outliers (max |z|=%.1f, threshold %.1f)", n, worst, cs.zscore)
					if cs.count > int64(len(cs.sample)) {
						msg += ", from sample"
					}
					out = append(out, Violation{Rule: rule.Type, Column: cs.name, Message: msg})
				}
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Column != out[j].Column {
			return out[i].Column < out[j].Column
		}
		return out[i].Rule < out[j].Rule
	})
	return out
}
