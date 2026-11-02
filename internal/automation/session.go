package automation

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/tqer39/dev-standards/internal/engine"
)

type HookInput struct {
	Event  string `json:"hook_event_name"`
	Active bool   `json:"stop_hook_active"`
}

func Root(cwd string) (string, error) {
	b, err := exec.Command("git", "-C", cwd, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}
func Prepare(ctx context.Context, repo string) (string, error) {
	if os.Getenv("DEV_STANDARDS_MAINTENANCE") == "1" {
		return "保守用の独立 checkout を検証しています。", nil
	}
	if err := advanceClean(ctx, repo); err != nil {
		return "", err
	}
	{
		executable, err := matchingRuntime(repo)
		if err != nil {
			return "", err
		}
		if executable != "" {
			cmd := exec.CommandContext(ctx, executable, "prepare", "--repo", repo)
			cmd.Env = append(os.Environ(), "DEV_STANDARDS_DELEGATED=1")
			b, err := cmd.CombinedOutput()
			return string(b), err
		}
	}
	tree, err := engine.Open(repo)
	if err != nil {
		return "", err
	}
	defer tree.Close()
	if isSource(repo) {
		if _, err := exec.CommandContext(ctx, "go", "version").Output(); err != nil {
			return "", err
		}
		return "dev-standards 自体の開発です。README.md と設計判断を読み、mise run check を実行してください。共通ルールの変更は固定された検証条件を維持してください。", nil
	}
	_, err = tree.Check()
	if err != nil {
		_ = Feedback(repo, "configuration", err.Error())
		return "共通ルールの適用状態に問題があります。正本を保持して sync と verify を実行してください。", err
	}
	return "共通ルールの設定は採用版に一致しています。.standards/rules.md と言語選定記録を読み、変更後に mise run standards:verify を実行してください。共通の問題は feedback に記録し、人間の保守指示を待たず自律保守に引き継いでください。", nil
}
func Finish(repo string) error {
	if os.Getenv("DEV_STANDARDS_MAINTENANCE") == "1" {
		return nil
	}
	tree, err := engine.Open(repo)
	if err != nil {
		return err
	}
	defer tree.Close()
	if !isSource(repo) {
		if _, err = tree.Check(); err != nil {
			_ = Feedback(repo, "configuration", err.Error())
			return err
		}
	}
	if !tree.ReceiptValid() {
		return fmt.Errorf("現在の変更に対する検証記録がありません。%s を実行してください", verificationCommand(repo))
	}
	return nil
}
func verificationCommand(repo string) string {
	if isSource(repo) {
		return "mise run session:verify"
	}
	return "mise run standards:verify"
}
func isSource(repo string) bool {
	b, err := os.ReadFile(filepath.Join(repo, "go.mod"))
	return err == nil && strings.Contains(string(b), "module github.com/tqer39/dev-standards\n")
}
func Feedback(repo, rule, message string) error {
	if len(message) == 0 || len(message) > 2048 || len(rule) > 100 {
		return fmt.Errorf("feedback must contain a bounded observation")
	}
	if regexp.MustCompile(`(?i)(sk-[a-z0-9_-]{16,}|-----BEGIN .*PRIVATE KEY|(?:api[_-]?key|access[_-]?token)\s*[:=])`).MatchString(message) {
		return fmt.Errorf("secret-like content must not be recorded as feedback")
	}
	tree, err := engine.Open(repo)
	if err != nil {
		return err
	}
	defer tree.Close()
	b, _ := json.Marshal(map[string]any{"rule": rule, "observation": message})
	dir := filepath.Join(repo, ".standards/cache/feedback")
	for _, p := range []string{filepath.Join(repo, ".standards"), filepath.Join(repo, ".standards/cache"), dir} {
		if info, e := os.Lstat(p); e == nil && info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("feedback path must not be a symlink")
		}
	}
	if err = os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	p := filepath.Join(dir, hash(string(b))+".json")
	if info, err := os.Lstat(p); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("feedback file must not be a symlink")
	}
	if _, err := os.Stat(p); err == nil {
		return nil
	}
	return os.WriteFile(p, append(b, '\n'), 0600)
}
func Hook(repo string, input HookInput) (map[string]any, error) {
	if os.Getenv("DEV_STANDARDS_MAINTENANCE") == "1" {
		return map[string]any{}, nil
	}
	if input.Event == "Stop" {
		if err := Finish(repo); err != nil {
			if input.Active {
				return map[string]any{"systemMessage": "検証未完了として終了します。自律保守に問題を記録しました。"}, Feedback(repo, "verification", err.Error())
			}
			return map[string]any{"decision": "block", "reason": err.Error() + "。成功を確認してから完了してください。"}, nil
		}
		return map[string]any{}, nil
	}
	if input.Event != "SessionStart" && input.Event != "UserPromptSubmit" {
		return nil, fmt.Errorf("unsupported hook event")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	message, err := Prepare(ctx, repo)
	if err != nil {
		message += "\n診断: " + err.Error()
	}
	return map[string]any{"hookSpecificOutput": map[string]any{"hookEventName": input.Event, "additionalContext": message}}, nil
}
func hostState(repo string) string {
	b, err := os.ReadFile(filepath.Join(repo, ".standards/cache/host.json"))
	if err != nil {
		return ""
	}
	var c struct {
		State string `json:"state"`
	}
	if json.Unmarshal(b, &c) != nil || !filepath.IsAbs(c.State) {
		return ""
	}
	return c.State
}
func matchingRuntime(repo string) (string, error) {
	state := hostState(repo)
	if state == "" {
		return "", nil
	}
	b, err := os.ReadFile(filepath.Join(repo, ".standards/lock.json"))
	var lock struct {
		Bundle string `json:"bundleSha256"`
	}
	if err == nil {
		if err = json.Unmarshal(b, &lock); err != nil {
			return "", err
		}
	} else if isSource(repo) {
		b, err = os.ReadFile(filepath.Join(state, "latest.json"))
		if err != nil {
			return "", nil
		}
		var ledger Ledger
		if err = json.Unmarshal(b, &ledger); err != nil {
			return "", err
		}
		lock.Bundle = ledger.Bundle
		if lock.Bundle == "" {
			return "", nil
		}
	} else {
		return "", nil
	}
	if !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(lock.Bundle) {
		return "", fmt.Errorf("invalid locked bundle")
	}
	name := "dev-standards"
	if runtimeWindows() {
		name += ".exe"
	}
	p := filepath.Join(state, "bundles", lock.Bundle, name)
	if _, err := os.Stat(p); os.IsNotExist(err) {
		return "", nil
	} else if err != nil {
		return "", err
	}
	self, _ := os.Executable()
	selfInfo, selfErr := os.Stat(self)
	targetInfo, targetErr := os.Stat(p)
	if self == p || (selfErr == nil && targetErr == nil && os.SameFile(selfInfo, targetInfo)) {
		return "", nil
	}
	return p, nil
}

// MatchingRuntime selects the immutable executable for the adopted bundle.
// Every verification entry point uses it; the fixed hook launcher stays stable.
func MatchingRuntime(repo string) (string, error) { return matchingRuntime(repo) }

func advanceClean(ctx context.Context, repo string) error {
	state := hostState(repo)
	if state == "" {
		return nil
	}
	b, err := os.ReadFile(filepath.Join(state, "fleet.json"))
	if err != nil {
		return nil
	}
	var c Config
	if err = json.Unmarshal(b, &c); err != nil {
		return err
	}
	if err = c.Validate(); err != nil {
		return err
	}
	ledgerBytes, err := os.ReadFile(filepath.Join(state, "latest.json"))
	if err != nil {
		return nil
	}
	var ledger Ledger
	if err = json.Unmarshal(ledgerBytes, &ledger); err != nil {
		return err
	}
	var entry *Entry
	entries := append([]Entry{c.Source}, c.Projects...)
	for i := range entries {
		if entries[i].Path == repo {
			entry = &entries[i]
			break
		}
	}
	if entry == nil {
		return nil
	}
	var wanted string
	for _, o := range ledger.Outcomes {
		if o.Repository == entry.Remote && (o.Status == "updated" || o.Status == "unchanged") {
			wanted = o.Commit
		}
	}
	if wanted == "" {
		return nil
	}
	x := Exec{}
	branch, err := git(ctx, x, repo, "branch", "--show-current")
	if err != nil {
		return err
	}
	if strings.TrimSpace(branch) != entry.Branch {
		return nil
	}
	status, err := git(ctx, x, repo, "status", "--porcelain=v1")
	if err != nil {
		return err
	}
	if status != "" {
		return nil
	}
	head, err := git(ctx, x, repo, "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	if strings.TrimSpace(head) == wanted {
		return nil
	}
	if !regexp.MustCompile(`^[0-9a-f]{40}$`).MatchString(wanted) {
		return fmt.Errorf("invalid maintenance commit")
	}
	if _, err = git(ctx, x, repo, "fetch", entry.Remote, wanted); err != nil {
		return err
	}
	if _, err = git(ctx, x, repo, "merge-base", "--is-ancestor", "HEAD", wanted); err != nil {
		return nil
	}
	_, err = git(ctx, x, repo, "merge", "--ff-only", wanted)
	return err
}
