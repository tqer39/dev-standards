package devstandards

import (
	"strings"
	"testing"
)

func TestCatalog(t *testing.T) {
	rules, err := Catalog()
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 7 {
		t.Fatalf("got %d rules", len(rules))
	}
	if Digest() == "" || !strings.Contains(Read("templates/base/gitignore.txt"), "Created by https://www.toptal.com/developers/gitignore/api/") {
		t.Fatal("missing provenance")
	}
}
func TestStrictDocuments(t *testing.T) {
	for name, s := range map[string]string{
		"duplicates": "schemaVersion: 1\nschemaVersion: 1\n",
		"alias":      "a: &anchor one\nb: *anchor\n",
		"tag":        "a: !!str value\n",
		"merge":      "a: {<<: {b: value}}\n",
		"multi":      "a: one\n---\nb: two\n",
		"unknown":    "schemaVersion: 1\nexceptions: []\nunknown: true\n",
		"format":     "schemaVersion: 1\nexceptions:\n  - id: legacy\n    rule: CORE-LINT\n    component: main\n    check: configuration\n    reason: rationale\n    mitigation: fallback\n    owner: maintainer\n    issue: https://example.com/123\n    expiresAt: never\n",
	} {
		t.Run(name, func(t *testing.T) {
			if err := Decode([]byte(s), "exceptions", nil); err == nil {
				t.Fatal("accepted invalid document")
			}
		})
	}
	if err := Decode([]byte("schemaVersion: 1\nexceptions: []\n"), "exceptions", nil); err != nil {
		t.Fatal(err)
	}
}
