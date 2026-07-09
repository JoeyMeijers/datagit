package repo

import (
	"time"

	"github.com/joeymeij/rowen/internal/model"
)

// modelCommitFixture returns a fixed commit for deterministic ID tests.
func modelCommitFixture() model.Commit {
	return model.Commit{
		Parent:    "abc",
		Message:   "fixture",
		Author:    "tester",
		CreatedAt: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
		Files: []model.FileEntry{{
			Path:   "a.csv",
			Size:   10,
			SHA256: "deadbeef",
		}},
	}
}
