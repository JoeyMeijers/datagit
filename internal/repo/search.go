package repo

import "github.com/joeymeij/rowen/internal/index"

// Search finds datasets by path or column name across indexed commits.
func (r *Repo) Search(term string) ([]index.SearchHit, error) {
	return r.idx.Search(term)
}
