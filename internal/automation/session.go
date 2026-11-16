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

	bundle "github.com/tqer39/dev-standards"
	"github.com/tqer39/dev-standards/internal/engine"
)

type HookInput struct {
	Event  string `json:"hook_event_name"`
	Active bool   `json:"stop_hook_active"`
}

const feedbackObservationLimit = 2048

var feedbackSecretPattern = regexp.MustCompile(`(?i)(sk-[a-z0-9_-]{16,}|-----BEGIN .*PRIVATE KEY|(?:api[_-]?key|access[_-]?token)\s*[:=])`)

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
		executable, err := matchingRuntime(ctx, repo)
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
	standard, err := tree.AppliedStandard()
	if err != nil {
		return "", err
	}
	if standard == nil {
		return "", fmt.Errorf("applied standard is missing")
	}
	return fmt.Sprintf("dev-standards 適用版: %s (bundle=%s)\n共通ルールの設定は採用版に一致しています。.standards/rules.md と言語選定記録を読み、変更後に mise run standards:verify を実行してください。共通の問題は feedback に記録し、人間の保守指示を待たず自律保守に引き継いでください。", standard.Version, standard.Bundle), nil
}
func Finish(repo string) error {
	if os.Getenv("DEV_STANDARDS_MAINTENANCE") == "1" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	executable, err := matchingRuntime(ctx, repo)
	if err != nil {
		return err
	}
	if executable != "" {
		output, err := exec.CommandContext(ctx, executable, "finish", "--repo", repo).CombinedOutput()
		if err != nil {
			return fmt.Errorf("finish failed: %w\n%s", err, output)
		}
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
	if len(message) == 0 || len(message) > feedbackObservationLimit || len(rule) > 100 {
		return fmt.Errorf("feedback must contain a bounded observation")
	}
	if feedbackSecretPattern.MatchString(message) {
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
func matchingRuntime(ctx context.Context, repo string) (string, error) {
	return matchingRuntimeFor(ctx, repo, runtimeIdentity{Version: bundle.Version, Bundle: bundle.Digest()})
}

func matchingRuntimeFor(ctx context.Context, repo string, current runtimeIdentity) (string, error) {
	// ソースの検証は checkout 内のコードを対象にし、保守台帳の公開版へ転送しない。
	if isSource(repo) {
		return "", nil
	}
	state := hostState(repo)
	if state == "" {
		return "", nil
	}
	tree, err := engine.Open(repo)
	if err != nil {
		return "", err
	}
	locked, err := tree.AppliedStandard()
	tree.Close()
	if err != nil || locked == nil {
		return "", err
	}
	wanted := runtimeIdentity{Version: locked.Version, Bundle: locked.Bundle}
	// 一致する実行環境自身は、旧形式の共有キャッシュへ戻らない。
	if wanted == current {
		return "", nil
	}
	paths := []string{wanted.path(state), filepath.Join(state, "bundles", wanted.Bundle, executableName())}
	for _, p := range paths {
		if err := checkRuntimeParents(state, p); err != nil {
			return "", err
		}
		info, err := os.Lstat(p)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return "", err
		}
		if !info.Mode().IsRegular() {
			return "", fmt.Errorf("runtime must be a regular file: %s", p)
		}
		probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		identity, err := inspectRuntime(probeCtx, Exec{}, p)
		cancel()
		if err != nil {
			return "", err
		}
		if identity != wanted {
			if p == paths[1] {
				continue
			}
			return "", fmt.Errorf("cached runtime identity does not match lock: %s", p)
		}
		if p == paths[1] {
			if err := protectLegacyRuntime(p); err != nil {
				return "", err
			}
			protectedCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			protected, err := inspectRuntime(protectedCtx, Exec{}, p)
			cancel()
			if err != nil || protected != wanted {
				return "", fmt.Errorf("legacy runtime changed while protecting it: %v", err)
			}
		}
		return p, nil
	}
	return "", fmt.Errorf("runtime for dev-standards@%s bundle=%s is unavailable; restore that exact runtime or install a launcher with that exact version and bundle", wanted.Version, wanted.Bundle)
}

// MatchingRuntime はロックの CLI 版とバンドルに一致する実行環境を選択する。
func MatchingRuntime(repo string) (string, error) { return matchingRuntime(context.Background(), repo) }

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
		if sameDirectory(entries[i].Path, repo) {
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
