//go:build !windows

package harness

import (
	"context"
	"strings"
	"testing"
	"time"
)

type fixedToken string

func (f fixedToken) Token(context.Context) (string, error) { return string(f), nil }

func forgeWith(responses map[string]response) (*GitHub, *scriptRunner) {
	runner := &scriptRunner{responses: responses}
	return &GitHub{Run: runner, Tokens: fixedToken("t0k3n"), Repo: "o/n", Poll: time.Millisecond}, runner
}

func TestCreatePRParsesNumberAndPassesTokenOnlyByEnv(t *testing.T) {
	forge, runner := forgeWith(map[string]response{"gh pr": {out: "https://github.com/o/n/pull/17\n"}})
	number, err := forge.CreatePR(context.Background(), "agent/r1", "main", "title", "body")
	if err != nil || number != 17 {
		t.Fatalf("number=%d err=%v", number, err)
	}
	call := runner.calls[0]
	if call.Env[0] != "GH_TOKEN=t0k3n" || call.Secrets[0] != "t0k3n" || strings.Contains(strings.Join(call.Args, " "), "t0k3n") {
		t.Fatalf("call=%+v", call)
	}
}

func TestArmMergeFixesHeadSHA(t *testing.T) {
	forge, runner := forgeWith(map[string]response{})
	if err := forge.ArmMerge(context.Background(), 17, "abc"); err != nil {
		t.Fatal(err)
	}
	if !runner.called("gh", "pr merge 17", "--auto", "--squash", "--match-head-commit abc") {
		t.Fatalf("calls=%+v", runner.calls)
	}
}

func TestWaitCheckReadsConclusion(t *testing.T) {
	cases := map[string]bool{"success": true, "failure": false}
	for conclusion, want := range cases {
		body := `{"check_runs":[{"status":"completed","conclusion":"` + conclusion + `"}]}`
		forge, _ := forgeWith(map[string]response{"gh api": {out: body}})
		got, err := forge.WaitCheck(context.Background(), "abc", "workflow-result")
		if err != nil || got != want {
			t.Errorf("%s: got=%v err=%v", conclusion, got, err)
		}
	}
}

func TestWaitCheckStopsAtContextDeadline(t *testing.T) {
	forge, _ := forgeWith(map[string]response{"gh api": {out: `{"check_runs":[]}`}})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()
	if _, err := forge.WaitCheck(ctx, "abc", "workflow-result"); err == nil {
		t.Fatal("timeout was ignored")
	}
}

func TestWaitMergedRejectsHeadChange(t *testing.T) {
	forge, _ := forgeWith(map[string]response{"gh pr": {out: `{"state":"OPEN","headRefOid":"other","mergeCommit":null}`}})
	if _, err := forge.WaitMerged(context.Background(), 17, "abc"); err == nil {
		t.Fatal("head change was ignored")
	}
}

func TestWaitMergedReturnsMergeSHA(t *testing.T) {
	forge, _ := forgeWith(map[string]response{"gh pr": {out: `{"state":"MERGED","headRefOid":"abc","mergeCommit":{"oid":"def"}}`}})
	sha, err := forge.WaitMerged(context.Background(), 17, "abc")
	if err != nil || sha != "def" {
		t.Fatalf("sha=%q err=%v", sha, err)
	}
}

func TestMarkNeedsHuman(t *testing.T) {
	forge, runner := forgeWith(map[string]response{})
	if err := forge.MarkNeedsHuman(context.Background(), 17, "理由"); err != nil {
		t.Fatal(err)
	}
	if !runner.called("gh", "label create needs-human") || !runner.called("gh", "pr edit 17", "--add-label needs-human") || !runner.called("gh", "pr comment 17", "理由") {
		t.Fatalf("calls=%+v", runner.calls)
	}
}
