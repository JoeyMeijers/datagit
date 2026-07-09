package repo

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/joeymeij/rowen/internal/quality"
)

// rulesFile is the optional per-repository validation configuration.
const rulesFile = "rules.json"

// LoadRules returns the repository's validation rules, falling back to
// quality.DefaultRules when no rules.json exists.
func (r *Repo) LoadRules() (quality.RuleSet, error) {
	path := filepath.Join(metaDir(r.Root), "config", rulesFile)
	blob, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return quality.DefaultRules(), nil
	}
	if err != nil {
		return quality.RuleSet{}, fmt.Errorf("reading %s: %w", rulesFile, err)
	}
	var rs quality.RuleSet
	if err := json.Unmarshal(blob, &rs); err != nil {
		return quality.RuleSet{}, fmt.Errorf("parsing %s: %w", rulesFile, err)
	}
	if len(rs.Rules) == 0 {
		return quality.RuleSet{}, fmt.Errorf("%s contains no rules", rulesFile)
	}
	return rs, nil
}
