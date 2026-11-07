//go:build !windows

package main

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestRunRequiresApprovalAndCriteria(t *testing.T) {
	var out bytes.Buffer
	err := dispatch([]string{"run", "--repo", "o/n", "--title", "t", "--task-file", "/nonexistent", "--criteria-file", "/nonexistent"}, &out)
	if err == nil || !strings.Contains(err.Error(), "approved-by-human") {
		t.Fatalf("err=%v", err)
	}
}

func TestUnknownCommand(t *testing.T) {
	var out bytes.Buffer
	if err := dispatch([]string{"unknown"}, &out); err == nil {
		t.Fatal("unknown command was accepted")
	}
}

func TestProfileOnlyAllowsCodexImplementer(t *testing.T) {
	if _, err := profile("claude", "m", "medium"); err == nil {
		t.Fatal("claude implementer must stay disabled until isolated")
	}
	p, err := profile("codex", "gpt-6.1-sol", "high")
	if err != nil || p.Reviewer == "" {
		t.Fatalf("profile=%+v err=%v", p, err)
	}
}

func TestProfileRejectsInvalidEffort(t *testing.T) {
	if _, err := profile("codex", "gpt-6.1-sol", "extreme"); err == nil {
		t.Fatal("invalid effort was accepted")
	}
}

func TestSetupProfileRefusesToOverwriteDifferentFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	if err := installProfile(); err != nil {
		t.Fatal(err)
	}
	if err := installProfile(); err != nil {
		t.Fatalf("identical file: %v", err)
	}
	if err := writeForTest(dir + "/.codex/agent-harness.config.toml"); err != nil {
		t.Fatal(err)
	}
	if err := installProfile(); err == nil {
		t.Fatal("different file was overwritten")
	}
}

func writeForTest(path string) error {
	return os.WriteFile(path, []byte("changed\n"), 0o600)
}

func TestClaudeLoggedIn(t *testing.T) {
	if err := claudeLoggedIn(`{"loggedIn":true}`); err != nil {
		t.Fatal(err)
	}
	if err := claudeLoggedIn(`{"loggedIn":false}`); err == nil {
		t.Fatal("logged out state was accepted")
	}
	if err := claudeLoggedIn(`not json`); err == nil {
		t.Fatal("invalid output was accepted")
	}
}
