//go:build !windows

package harness

import (
	"os"
	"path/filepath"
	"testing"
)

const goodVerdict = `{"sha":"abc","approved":true,"security_findings":[],"findings":[],"criteria":[{"criterion":"c","met":true}],"reason":"ok"}`

func TestVerdictDecisions(t *testing.T) {
	cases := []struct {
		name string
		body string
		want Decision
	}{
		{"合格", goodVerdict, DecisionPass},
		{"指摘", `{"sha":"abc","approved":false,"security_findings":[],"findings":[{"summary":"x"}],"criteria":[{"criterion":"c","met":false}],"reason":"r"}`, DecisionRepair},
		{"セキュリティ", `{"sha":"abc","approved":true,"security_findings":[{"summary":"x"}],"findings":[],"criteria":[{"criterion":"c","met":true}],"reason":"r"}`, DecisionStop},
		{"未達なのに合格", `{"sha":"abc","approved":true,"security_findings":[],"findings":[],"criteria":[{"criterion":"c","met":false}],"reason":"r"}`, DecisionRepair},
		{"条件なし", `{"sha":"abc","approved":true,"security_findings":[],"findings":[],"criteria":[],"reason":"r"}`, DecisionStop},
	}
	for _, c := range cases {
		verdict, err := ParseVerdict([]byte(c.body), "abc")
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if got := verdict.Decide(); got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}

func TestParseVerdictReadsEnvelope(t *testing.T) {
	verdict, err := ParseVerdict([]byte(`{"type":"result","structured_output":`+goodVerdict+`}`), "abc")
	if err != nil || !verdict.Approved {
		t.Fatalf("verdict=%+v err=%v", verdict, err)
	}
}

func TestParseVerdictRejectsWrongSHAAndInvalid(t *testing.T) {
	if _, err := ParseVerdict([]byte(goodVerdict), "other"); err == nil {
		t.Fatal("wrong sha was accepted")
	}
	if _, err := ParseVerdict([]byte(`not json`), "abc"); err == nil {
		t.Fatal("invalid json was accepted")
	}
}

func TestReadResultMatchesRunAndRound(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".agent-run", "result.json")
	if _, ok, err := ReadResult(dir, "r1", 0); ok || err != nil {
		t.Fatalf("missing file: ok=%v err=%v", ok, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"run_id":"r1","round":0,"status":"done","summary":"s"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := ReadResult(dir, "r1", 1); ok || err != nil {
		t.Fatalf("old round: ok=%v err=%v", ok, err)
	}
	result, ok, err := ReadResult(dir, "r1", 0)
	if !ok || err != nil || result.Status != "done" {
		t.Fatalf("result=%+v ok=%v err=%v", result, ok, err)
	}
}
