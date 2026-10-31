// Package devstandards embeds the versioned policy inputs used by the CLI.
package devstandards

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"sort"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"go.yaml.in/yaml/v3"
)

const Version = "0.2.3"
const Repository = "tqer39/dev-standards"

//go:embed schemas rules guidance profiles templates
var Assets embed.FS

var schemas sync.Map

func Read(name string) string {
	b, err := Assets.ReadFile(name)
	if err != nil {
		panic(err)
	}
	return string(b)
}

func Digest() string {
	h := sha256.New()
	_ = fs.WalkDir(Assets, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			fmt.Fprintf(h, "%s\x00%s\x00", p, Read(p))
		}
		return nil
	})
	return hex.EncodeToString(h.Sum(nil))
}

// Decode accepts exactly one JSON-compatible YAML document without aliases,
// explicit tags, merge keys or duplicate keys. JSON is a subset of this format.
func Decode(data []byte, schemaName string, out any) error {
	if len(data) > 1<<20 {
		return fmt.Errorf("document exceeds 1 MiB")
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	var n yaml.Node
	if err := dec.Decode(&n); err != nil {
		return err
	}
	count := 0
	if err := nodeCheck(&n, 0, &count); err != nil {
		return err
	}
	var extra yaml.Node
	if err := dec.Decode(&extra); err != io.EOF {
		return fmt.Errorf("exactly one document is required")
	}
	var value any
	if err := n.Decode(&value); err != nil {
		return err
	}
	normalized, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("JSON-compatible values required: %w", err)
	}
	if err := json.Unmarshal(normalized, &value); err != nil {
		return err
	}
	if schemaName != "" {
		validator, err := schema(schemaName)
		if err != nil {
			return err
		}
		if err := validator.Validate(value); err != nil {
			return err
		}
	}
	if out != nil {
		return json.Unmarshal(normalized, out)
	}
	return nil
}

func nodeCheck(n *yaml.Node, depth int, count *int) error {
	*count++
	if depth > 64 || *count > 20000 {
		return fmt.Errorf("document is too complex")
	}
	if n.Anchor != "" || n.Alias != nil || n.Kind == yaml.AliasNode || n.Style&yaml.TaggedStyle != 0 {
		return fmt.Errorf("anchors, aliases and explicit tags are not allowed")
	}
	if n.Kind == yaml.MappingNode {
		seen := map[string]bool{}
		for i := 0; i < len(n.Content); i += 2 {
			key := n.Content[i]
			if key.Kind != yaml.ScalarNode || key.Tag != "!!str" || key.Value == "<<" || seen[key.Value] {
				return fmt.Errorf("mapping keys must be unique strings: %q", key.Value)
			}
			seen[key.Value] = true
		}
	}
	for _, child := range n.Content {
		if err := nodeCheck(child, depth+1, count); err != nil {
			return err
		}
	}
	return nil
}

func schema(name string) (*jsonschema.Schema, error) {
	if s, ok := schemas.Load(name); ok {
		return s.(*jsonschema.Schema), nil
	}
	var doc any
	if err := json.Unmarshal([]byte(Read("schemas/"+name+".schema.json")), &doc); err != nil {
		return nil, err
	}
	c := jsonschema.NewCompiler()
	c.AssertFormat()
	uri := "https://dev-standards.invalid/" + name + ".schema.json"
	if err := c.AddResource(uri, doc); err != nil {
		return nil, err
	}
	s, err := c.Compile(uri)
	if err != nil {
		return nil, err
	}
	schemas.Store(name, s)
	return s, nil
}

type Rule struct {
	ID       string `json:"id"`
	Revision int    `json:"revision"`
	Guidance string `json:"guidance"`
	Checks   []struct {
		ID         string         `json:"id"`
		Type       string         `json:"type"`
		Handler    string         `json:"handler"`
		Parameters map[string]any `json:"parameters"`
	} `json:"checks"`
}

func Catalog() ([]Rule, error) {
	var rules []Rule
	seen := map[string]bool{}
	err := fs.WalkDir(Assets, "rules", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		var rule Rule
		if err := Decode([]byte(Read(p)), "rule", &rule); err != nil {
			return fmt.Errorf("%s: %w", p, err)
		}
		if seen[rule.ID] {
			return fmt.Errorf("duplicate rule: %s", rule.ID)
		}
		seen[rule.ID] = true
		if _, err := Assets.ReadFile(rule.Guidance); err != nil {
			return err
		}
		checks := map[string]bool{}
		for _, c := range rule.Checks {
			switch c.Handler {
			case "editorconfig", "gitignore", "mise", "bootstrap", "lint", "format", "language":
			default:
				return fmt.Errorf("unknown handler: %s", c.Handler)
			}
			if c.Type != "builtin" || len(c.Parameters) != 0 || checks[c.ID] {
				return fmt.Errorf("invalid check in %s", rule.ID)
			}
			checks[c.ID] = true
		}
		rules = append(rules, rule)
		return nil
	})
	sort.Slice(rules, func(i, j int) bool { return rules[i].ID < rules[j].ID })
	return rules, err
}

type Profile struct {
	ID        string            `json:"id"`
	Rules     []string          `json:"rules"`
	Platforms []string          `json:"platforms"`
	Tools     map[string]string `json:"tools"`
}

// ResolveProfile validates both shared and language-specific references.
func ResolveProfile(id string) (Profile, error) {
	if id != "go" && id != "python-uv" {
		return Profile{}, fmt.Errorf("unsupported profile: %s", id)
	}
	rules, err := Catalog()
	if err != nil {
		return Profile{}, err
	}
	known := map[string]bool{}
	for _, r := range rules {
		known[r.ID] = true
	}
	var base, p Profile
	if err := Decode([]byte(Read("profiles/base.yml")), "profile", &base); err != nil {
		return p, err
	}
	if err := Decode([]byte(Read("profiles/"+id+".yml")), "profile", &p); err != nil {
		return p, err
	}
	if base.ID != "base" || p.ID != id {
		return p, fmt.Errorf("profile ID does not match its path")
	}
	p.Rules = append(base.Rules, p.Rules...)
	seen := map[string]bool{}
	for _, rule := range p.Rules {
		if !known[rule] || seen[rule] {
			return p, fmt.Errorf("invalid profile rule reference: %s", rule)
		}
		seen[rule] = true
	}
	if len(seen) != len(known) {
		return p, fmt.Errorf("basic profile must select all current rules")
	}
	if id == "go" && p.Tools["go"] == "" {
		return p, fmt.Errorf("Go profile must pin its tool")
	}
	if id == "python-uv" && (p.Tools["python"] == "" || p.Tools["uv"] == "") {
		return p, fmt.Errorf("Python profile must pin python and uv")
	}
	return p, nil
}
