//go:build !windows

// agent-harness は受入条件の承認済みタスクを無人で処理する。
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	harnessassets "github.com/tqer39/dev-standards/harness"
	"github.com/tqer39/dev-standards/internal/harness"
)

type runProfile struct{ Agent, Model, Effort, Reviewer string }

func profile(agent, model, effort string) (runProfile, error) {
	// Claude を実装担当にする分離は未検証なので、確認できるまで受け付けない。
	if agent != "codex" {
		return runProfile{}, fmt.Errorf("実装担当 %q はまだ使えません", agent)
	}
	if !slices.Contains([]string{"low", "medium", "high"}, effort) {
		return runProfile{}, fmt.Errorf("エフォート %q は使えません", effort)
	}
	return runProfile{Agent: agent, Model: model, Effort: effort, Reviewer: "claude-opus-5-5"}, nil
}

func main() {
	if err := dispatch(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		switch {
		case errors.Is(err, harness.ErrQuota):
			os.Exit(3)
		case errors.Is(err, harness.ErrStopped):
			os.Exit(4)
		}
		os.Exit(1)
	}
}

func dispatch(args []string, out io.Writer) error {
	if len(args) == 0 {
		return errors.New("使い方: agent-harness <setup|doctor|run|resume|reconcile|clear-stop>")
	}
	switch args[0] {
	case "setup":
		return installProfile()
	case "doctor":
		return doctor(args[1:], out)
	case "run":
		return runCommand(args[1:], out)
	case "resume":
		return resumeCommand(args[1:], out)
	case "reconcile":
		return reconcileCommand(args[1:], out)
	case "clear-stop":
		return clearStop(args[1:], out)
	}
	return fmt.Errorf("不明なコマンドです: %s", args[0])
}

func stateRoot() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "state", "agent-harness"), nil
}

func installProfile() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	path := filepath.Join(home, ".codex", "agent-harness.config.toml")
	existing, err := os.ReadFile(path)
	if err == nil {
		if bytes.Equal(existing, harnessassets.CodexProfile) {
			return nil
		}
		return fmt.Errorf("%s は内容が異なるので上書きしません。新しい版を導入するには、内容を確認してから `mv %s %s.bak` で退避し、`agent-harness setup` を再実行してください", path, path, path)
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, harnessassets.CodexProfile, 0o600)
}

func resolve(name string) (string, error) {
	// Herdr の PATH には mise の実行ファイルが入らないので、絶対パスで渡す。
	path, err := exec.LookPath(name)
	if err != nil {
		return "", fmt.Errorf("%s が見つかりません: %w", name, err)
	}
	return filepath.Abs(path)
}

func build(repo string) (*harness.Orchestrator, error) {
	root, err := stateRoot()
	if err != nil {
		return nil, err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	paths := map[string]string{}
	for _, name := range []string{"hc", "codex", "claude", "op", "gh", "git", "gitleaks"} {
		path, err := resolve(name)
		if err != nil {
			return nil, err
		}
		paths[name] = path
	}
	runner := harness.OSRunner{}
	tokens := &harness.AppTokens{
		Secrets: harness.OPSecrets{Run: runner},
		Repo:    repo,
		API:     harness.GitHubAPI,
		HTTP:    &http.Client{Timeout: 30 * time.Second},
		Now:     time.Now,
	}
	tree := &harness.GitTree{
		Run:        runner,
		Tokens:     tokens,
		Codex:      paths["codex"],
		StateRoot:  root,
		WorkRoot:   filepath.Join(home, "workspace", "tqer39"),
		RemoteURL:  func(repo string) string { return "https://github.com/" + repo + ".git" },
		AuthHeader: true,
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, err
	}
	return &harness.Orchestrator{
		Store:  harness.Store{Root: root},
		Agents: &harness.Herdr{Run: runner, HC: paths["hc"], Codex: paths["codex"], Claude: paths["claude"], Poll: 5 * time.Second},
		Tree:   tree,
		Forge:  &harness.GitHub{Run: runner, Tokens: tokens, Repo: repo, Poll: 15 * time.Second},
		Scanner: harness.Gitleaks{
			Run: runner, Bin: paths["gitleaks"], Dir: root,
		},
		Config:        harness.LoadRepoConfig,
		RoundLimit:    30 * time.Minute,
		MergeLimit:    30 * time.Minute,
		ExternalGrace: harness.ExternalReviewGrace,
	}, nil
}

func runCommand(args []string, out io.Writer) error {
	flags := flag.NewFlagSet("run", flag.ContinueOnError)
	repo := flags.String("repo", "", "owner/name")
	base := flags.String("base", "main", "基準ブランチ")
	title := flags.String("title", "", "コミットと PR の題名")
	kind := flags.String("kind", "feature", "feature, bugfix, refactor")
	taskFile := flags.String("task-file", "", "タスクの説明")
	criteriaFile := flags.String("criteria-file", "", "人間が承認した受入条件")
	acceptancePatch := flags.String("acceptance-patch", "", "固定する受入テストの差分")
	approved := flags.Bool("approved-by-human", false, "受入条件を人間が承認した")
	agent := flags.String("agent", "codex", "実装担当")
	model := flags.String("model", "gpt-6.1-sol", "モデル")
	effort := flags.String("effort", "medium", "エフォート")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if !*approved {
		return errors.New("--approved-by-human がありません。受入条件の承認後に実行してください")
	}
	if *repo == "" || *title == "" {
		return errors.New("--repo と --title は必須です")
	}
	if !slices.Contains([]string{"feature", "bugfix", "refactor"}, *kind) {
		return fmt.Errorf("タスク種別 %q は使えません", *kind)
	}
	selected, err := profile(*agent, *model, *effort)
	if err != nil {
		return err
	}
	task, err := os.ReadFile(*taskFile)
	if err != nil {
		return err
	}
	criteria, err := os.ReadFile(*criteriaFile)
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(task)) == "" {
		return errors.New("タスクの説明が空です")
	}
	if strings.TrimSpace(string(criteria)) == "" {
		return errors.New("受入条件が空です")
	}
	var acceptance []byte
	if *acceptancePatch != "" {
		acceptance, err = os.ReadFile(*acceptancePatch)
		if err != nil {
			return err
		}
	}
	orchestrator, err := build(*repo)
	if err != nil {
		return err
	}
	rec := &harness.Record{
		RunID: harness.NewRunID(time.Now()), Repo: *repo, Base: *base, Title: *title, Kind: *kind,
		Task: strings.TrimSpace(string(task)), Criteria: strings.TrimSpace(string(criteria)),
		Agent: selected.Agent, Model: selected.Model, Effort: selected.Effort, ReviewerModel: selected.Reviewer,
		Status: harness.StatusImplementing,
	}
	fmt.Fprintf(out, "実行 ID: %s\n", rec.RunID)
	err = orchestrator.Run(context.Background(), rec, acceptance, *approved)
	return report(out, rec, err)
}

func report(out io.Writer, rec *harness.Record, err error) error {
	fmt.Fprintf(out, "状態: %s\nPR: %d\n", rec.Status, rec.PR)
	if rec.StopReason != "" {
		fmt.Fprintf(out, "停止理由: %s\n", rec.StopReason)
	}
	return err
}

func resumeCommand(args []string, out io.Writer) error {
	flags := flag.NewFlagSet("resume", flag.ContinueOnError)
	id := flags.String("run", "", "実行 ID")
	if err := flags.Parse(args); err != nil {
		return err
	}
	root, err := stateRoot()
	if err != nil {
		return err
	}
	rec, err := harness.Store{Root: root}.Load(*id)
	if err != nil {
		return err
	}
	orchestrator, err := build(rec.Repo)
	if err != nil {
		return err
	}
	return report(out, rec, orchestrator.Resume(context.Background(), rec))
}

func reconcileCommand(args []string, out io.Writer) error {
	flags := flag.NewFlagSet("reconcile", flag.ContinueOnError)
	repo := flags.String("repo", "", "owner/name")
	if err := flags.Parse(args); err != nil {
		return err
	}
	orchestrator, err := build(*repo)
	if err != nil {
		return err
	}
	if err := orchestrator.Reconcile(context.Background(), *repo); err != nil {
		return err
	}
	fmt.Fprintln(out, "未確認のマージを処理しました。")
	return nil
}

func clearStop(args []string, out io.Writer) error {
	flags := flag.NewFlagSet("clear-stop", flag.ContinueOnError)
	repo := flags.String("repo", "", "owner/name")
	if err := flags.Parse(args); err != nil {
		return err
	}
	root, err := stateRoot()
	if err != nil {
		return err
	}
	if err := (harness.Store{Root: root}).ClearStop(*repo); err != nil {
		return err
	}
	fmt.Fprintf(out, "%s の停止を解除しました。\n", *repo)
	return nil
}

// claudeLoggedIn は無人のレビューに必要な CLI のログインを確認する。デスクトップアプリとは別に必要である。
func claudeLoggedIn(status string) error {
	var state struct {
		LoggedIn bool `json:"loggedIn"`
	}
	if err := json.Unmarshal([]byte(status), &state); err != nil {
		return fmt.Errorf("claude auth status を解釈できません: %w", err)
	}
	if !state.LoggedIn {
		return errors.New("ターミナルの claude がログインしていません")
	}
	return nil
}

// doctor は準備の状態を確認する。秘密情報の値は表示しない。
func doctor(args []string, out io.Writer) error {
	flags := flag.NewFlagSet("doctor", flag.ContinueOnError)
	repo := flags.String("repo", "", "owner/name")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *repo == "" {
		return errors.New("--repo は必須です")
	}
	failed := false
	check := func(name string, err error) {
		if err != nil {
			failed = true
			fmt.Fprintf(out, "FAIL %s: %v\n", name, err)
			return
		}
		fmt.Fprintf(out, "PASS %s\n", name)
	}
	for _, name := range []string{"hc", "codex", "claude", "op", "gh", "git", "gitleaks"} {
		_, err := resolve(name)
		check(name, err)
	}
	check("Codex の権限プロファイル", installProfile())
	status, err := harness.OSRunner{}.Run(context.Background(), harness.Cmd{Name: "claude", Args: []string{"auth", "status"}})
	if err == nil {
		err = claudeLoggedIn(status)
	}
	check("Claude のログイン", err)
	tokens := &harness.AppTokens{
		Secrets: harness.OPSecrets{Run: harness.OSRunner{}},
		Repo:    *repo,
		API:     harness.GitHubAPI,
		HTTP:    &http.Client{Timeout: 30 * time.Second},
		Now:     time.Now,
	}
	_, err = tokens.Token(context.Background())
	check("GitHub App の短命トークン", err)
	if failed {
		return errors.New("準備が完了していません")
	}
	return nil
}
