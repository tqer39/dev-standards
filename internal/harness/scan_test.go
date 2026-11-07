//go:build !windows

package harness

import (
	"context"
	"strings"
	"testing"
)

func TestGitleaksReportsLeakOnlyOnDedicatedExitCode(t *testing.T) {
	runner := &scriptRunner{responses: map[string]response{"mise": {err: &RunError{Name: "mise", Code: gitleaksLeakExit}}}}
	leak, err := Gitleaks{Run: runner, Bin: "mise", Prefix: []string{"exec", "gitleaks@1", "--", "gitleaks"}, Dir: "/tmp"}.Scan(context.Background(), []byte("+x"))
	if err != nil || !leak {
		t.Fatalf("leak=%v err=%v", leak, err)
	}
	call := runner.calls[0]
	if call.Dir != "/tmp" || call.Stdin != "+x" {
		t.Fatalf("call=%+v", call)
	}
	if !strings.HasPrefix(strings.Join(call.Args, " "), "exec gitleaks@1 -- gitleaks stdin") {
		t.Fatalf("args=%v", call.Args)
	}
}

func TestGitleaksTreatsToolFailureAsError(t *testing.T) {
	// 起動エラーは 1 で終わることがある。秘密情報の検出と区別する。
	for _, code := range []int{1, 2, 127} {
		runner := &scriptRunner{responses: map[string]response{"gitleaks": {err: &RunError{Name: "gitleaks", Code: code}}}}
		if _, err := (Gitleaks{Run: runner, Bin: "gitleaks", Dir: "/tmp"}).Scan(context.Background(), []byte("+x")); err == nil {
			t.Errorf("exit %d was ignored", code)
		}
	}
}

func TestGitleaksClean(t *testing.T) {
	runner := &scriptRunner{responses: map[string]response{}}
	leak, err := Gitleaks{Run: runner, Bin: "gitleaks", Dir: "/tmp"}.Scan(context.Background(), []byte("+x"))
	if err != nil || leak {
		t.Fatalf("leak=%v err=%v", leak, err)
	}
}

func TestVerifyScannerRequiresDetection(t *testing.T) {
	if err := VerifyScanner(context.Background(), stubScanner(func(patch []byte) bool { return strings.Contains(string(patch), "ghp_") })); err != nil {
		t.Fatal(err)
	}
	if err := VerifyScanner(context.Background(), stubScanner(func([]byte) bool { return false })); err == nil {
		t.Fatal("a scanner that detects nothing was accepted")
	}
	if err := VerifyScanner(context.Background(), stubScanner(func([]byte) bool { return true })); err == nil {
		t.Fatal("a scanner that flags everything was accepted")
	}
}

type stubScanner func([]byte) bool

func (s stubScanner) Scan(_ context.Context, patch []byte) (bool, error) { return s(patch), nil }
