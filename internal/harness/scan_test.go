//go:build !windows

package harness

import (
	"context"
	"testing"
)

func TestGitleaksReportsLeakOnExitOne(t *testing.T) {
	runner := &scriptRunner{responses: map[string]response{"gitleaks": {err: &RunError{Name: "gitleaks", Code: 1}}}}
	leak, err := Gitleaks{Run: runner, Bin: "gitleaks", Dir: "/tmp"}.Scan(context.Background(), []byte("+x"))
	if err != nil || !leak {
		t.Fatalf("leak=%v err=%v", leak, err)
	}
	if runner.calls[0].Dir != "/tmp" || runner.calls[0].Stdin != "+x" {
		t.Fatalf("call=%+v", runner.calls[0])
	}
}

func TestGitleaksTreatsOtherFailuresAsErrors(t *testing.T) {
	runner := &scriptRunner{responses: map[string]response{"gitleaks": {err: &RunError{Name: "gitleaks", Code: 2}}}}
	if _, err := (Gitleaks{Run: runner, Bin: "gitleaks", Dir: "/tmp"}).Scan(context.Background(), []byte("+x")); err == nil {
		t.Fatal("scanner failure was ignored")
	}
}

func TestGitleaksClean(t *testing.T) {
	runner := &scriptRunner{responses: map[string]response{}}
	leak, err := Gitleaks{Run: runner, Bin: "gitleaks", Dir: "/tmp"}.Scan(context.Background(), []byte("+x"))
	if err != nil || leak {
		t.Fatalf("leak=%v err=%v", leak, err)
	}
}
