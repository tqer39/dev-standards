package engine

import (
	"fmt"
	"regexp"

	bundle "github.com/tqer39/dev-standards"
)

// AppliedStandard identifies the locked adoption, independently of this CLI.
type AppliedStandard struct {
	Repository string `json:"repository"`
	Version    string `json:"version"`
	Bundle     string `json:"bundleSha256"`
}

func (t *Tree) AppliedStandard() (*AppliedStandard, error) {
	s, err := t.Read(".standards/lock.json")
	if err != nil || !s.Exists {
		return nil, err
	}
	var lock Lock
	if err := strictJSON(s.Content, &lock); err != nil {
		return nil, fmt.Errorf("lock.json: %w", err)
	}
	if lock.SchemaVersion != 1 || lock.Repository != bundle.Repository || !pinnedGo(lock.Version) || !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(lock.Bundle) {
		return nil, fmt.Errorf("lock.json has an invalid standard identity")
	}
	return &AppliedStandard{lock.Repository, lock.Version, lock.Bundle}, nil
}
