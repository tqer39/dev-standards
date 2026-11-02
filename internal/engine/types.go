package engine

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

type Manifest struct {
	SchemaVersion int `json:"schemaVersion"`
	Standard      struct {
		Repository string `json:"repository"`
		Version    string `json:"version"`
	} `json:"standard"`
	Platforms  []string    `json:"platforms"`
	Components []Component `json:"components"`
	AI         struct {
		Targets []string `json:"targets"`
		// Instructions names an external generator that owns AGENTS.md.
		Instructions string `json:"instructions,omitempty"`
	} `json:"ai"`
	Gitignore struct {
		Templates []string `json:"templates"`
		Local     string   `json:"local"`
	} `json:"gitignore"`
	Update struct {
		Channel    string `json:"channel"`
		AllowMajor bool   `json:"allowMajor"`
		Rollout    string `json:"rollout"`
	} `json:"update"`
	CI struct {
		PRBudgetSeconds int `json:"prBudgetSeconds"`
		MinimumSamples  int `json:"minimumSamples"`
		BreachWindows   int `json:"breachWindows"`
	} `json:"ci"`
}
type Component struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	Profile string `json:"profile"`
}
type Snapshot struct {
	Exists  bool   `json:"exists"`
	Content string `json:"content"`
	Mode    uint32 `json:"mode"`
}
type Change struct {
	Path   string   `json:"path"`
	Before Snapshot `json:"before"`
	After  Snapshot `json:"after"`
}
type Request struct {
	Mode          string `json:"mode"`
	Automation    bool   `json:"automation,omitempty"`
	Profile       string `json:"profile,omitempty"`
	DecisionPath  string `json:"decisionPath,omitempty"`
	ComponentPath string `json:"componentPath,omitempty"`
	Instructions  string `json:"instructions,omitempty"`
}
type Plan struct {
	SchemaVersion int                 `json:"schemaVersion"`
	Version       string              `json:"version"`
	Bundle        string              `json:"bundleSha256"`
	Root          string              `json:"root"`
	HEAD          string              `json:"head"`
	Request       Request             `json:"request"`
	Inputs        map[string]Snapshot `json:"inputs"`
	Changes       []Change            `json:"changes"`
	Conflicts     []string            `json:"conflicts"`
}
type State struct {
	SchemaVersion int                 `json:"schemaVersion"`
	Bundle        string              `json:"bundleSha256"`
	Files         map[string]Snapshot `json:"files"`
}
type Lock struct {
	SchemaVersion   int    `json:"schemaVersion"`
	Repository      string `json:"repository"`
	Version         string `json:"version"`
	Bundle          string `json:"bundleSha256"`
	GitignoreSHA256 string `json:"gitignoreSha256"`
	GitignoreURL    string `json:"gitignoreUrl"`
	GoVersion       string `json:"goVersion"`
	PythonVersion   string `json:"pythonVersion,omitempty"`
	UVVersion       string `json:"uvVersion,omitempty"`
	NodeVersion     string `json:"nodeVersion,omitempty"`
}
type Result struct {
	Rule      string `json:"rule"`
	Check     string `json:"check"`
	Component string `json:"component"`
	Status    string `json:"status"`
	Evidence  string `json:"evidence"`
}
type Report struct {
	HEAD    string   `json:"head"`
	Scope   string   `json:"scope"`
	Results []Result `json:"results"`
	Changes []Change `json:"changes,omitempty"`
}

type Failure struct {
	Code    int
	Message string
}

func (e *Failure) Error() string { return e.Message }
func fail(code int, format string, args ...any) error {
	return &Failure{code, fmt.Sprintf(format, args...)}
}
func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	if e, ok := err.(*Failure); ok {
		return e.Code
	}
	return 2
}
func JSON(v any) []byte {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		panic(err)
	}
	return append(b, '\n')
}
func sum(s string) string          { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }
func file(content string) Snapshot { return Snapshot{true, content, 0644} }
func equal(a, b Snapshot) bool {
	return a.Exists == b.Exists && a.Content == b.Content && a.Mode == b.Mode
}
func normalized(s string) string { return strings.ReplaceAll(s, "\r\n", "\n") }
