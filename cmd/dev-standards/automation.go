package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/tqer39/dev-standards/internal/automation"
	"github.com/tqer39/dev-standards/internal/engine"
)

func automationCommand(command string, args []string, out, errOut io.Writer) error {
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(errOut)
	repo := flags.String("repo", ".", "対象リポジトリ")
	fleet := flags.String("fleet", "", "初回設定で固定する保守台帳 JSON")
	profile := flags.String("profile", "go", "言語プロファイル: go, python-uv, node-npm")
	instructions := flags.String("instructions", "", "AGENTS.md を生成する外部ツール: rulesync。省略すると dev-standards が管理")
	componentPath := flags.String("component-path", "", "単一コンポーネントの相対ディレクトリ")
	decision := flags.String("decision", "", "言語選定記録")
	rule := flags.String("rule", "observation", "問題に関係するルール")
	message := flags.String("message", "", "共通の問題の観測内容")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments")
	}
	root, err := automation.Root(*repo)
	if err != nil {
		return err
	}
	if command == "prepare" || command == "finish" || command == "hook" || command == "session-verify" {
		if handled, err := forwardRuntime(root, command, args, out, errOut); handled || err != nil {
			return err
		}
	}
	if command == "host-init" {
		if *fleet == "" {
			return fmt.Errorf("--fleet is required")
		}
		name, err := filepath.Abs(*fleet)
		if err != nil {
			return err
		}
		if _, err := os.Stat(name); err == nil {
			return fmt.Errorf("fleet already exists; enrollment must preserve the existing host policy")
		}
		remote, err := exec.Command("git", "-C", root, "remote", "get-url", "origin").Output()
		if err != nil {
			return err
		}
		c := automation.Default(automation.Entry{Path: root, Remote: strings.TrimSpace(string(remote)), Branch: "main", Role: "source"})
		if err = automation.Save(name, c); err != nil {
			return err
		}
		binary, err := os.Executable()
		if err != nil {
			return err
		}
		if err = automation.InstallRuntime(root, binary, filepath.Dir(name)); err != nil {
			return err
		}
		b, _ := json.MarshalIndent(c, "", "  ")
		_, err = out.Write(append(b, '\n'))
		return err
	}
	if command == "maintain" {
		if *fleet == "" {
			return fmt.Errorf("--fleet is required")
		}
		c, err := automation.Load(*fleet)
		if err != nil {
			return err
		}
		state, err := filepath.Abs(filepath.Dir(*fleet))
		if err != nil {
			return err
		}
		report, runErr := (automation.Maintainer{Commands: automation.Exec{Log: errOut}}).Run(context.Background(), c, state)
		_, writeErr := out.Write(engine.JSON(report))
		if writeErr != nil {
			return writeErr
		}
		return runErr
	}
	if command == "feedback" {
		return automation.Feedback(root, *rule, *message)
	}
	if command == "prepare" {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		s, err := automation.Prepare(ctx, root)
		fmt.Fprintln(out, s)
		return err
	}
	if command == "finish" {
		return automation.Finish(root)
	}
	if command == "hook" {
		var input automation.HookInput
		if err := json.NewDecoder(io.LimitReader(os.Stdin, 65536)).Decode(&input); err != nil {
			return err
		}
		result, err := automation.Hook(root, input)
		if err != nil {
			return err
		}
		_, err = out.Write(engine.JSON(result))
		return err
	}
	if command == "session-verify" {
		tree, err := engine.Open(root)
		if err != nil {
			return err
		}
		defer tree.Close()
		fingerprint, err := tree.Fingerprint()
		if err != nil {
			return err
		}
		cmd := exec.Command("mise", "run", "check")
		cmd.Dir = root
		cmd.Stdout = errOut
		cmd.Stderr = errOut
		if err = cmd.Run(); err != nil {
			return err
		}
		return tree.WriteReceipt(fingerprint)
	}
	tree, err := engine.Open(root)
	if err != nil {
		return err
	}
	defer tree.Close()
	if command == "audit" {
		inspection, err := tree.Inspect()
		if err != nil {
			return err
		}
		missing := []string{}
		for _, name := range []string{".editorconfig", ".gitignore", "mise.toml", ".standards/repository.yml"} {
			found := false
			for _, p := range inspection.Files {
				found = found || p == name
			}
			if !found {
				missing = append(missing, name)
			}
		}
		_, err = out.Write(engine.JSON(map[string]any{"repository": root, "missing": missing, "adopted": inspection.Adopted}))
		if err != nil {
			return err
		}
		if len(missing) > 0 {
			return &engine.Failure{Code: 1, Message: "required baseline is missing"}
		}
		return nil
	}
	if command == "sync" || command == "onboard" {
		var enrollment automation.Config
		if command == "onboard" {
			if *fleet == "" {
				return fmt.Errorf("--fleet is required for autonomous onboarding")
			}
			enrollment, err = automation.Load(*fleet)
			if err != nil {
				return err
			}
			remote, e := exec.Command("git", "-C", root, "remote", "get-url", "origin").Output()
			if e != nil {
				return e
			}
			found := false
			for _, entry := range enrollment.Projects {
				found = found || entry.Path == root
			}
			if !found {
				enrollment.Projects = append(enrollment.Projects, automation.Entry{Path: root, Remote: strings.TrimSpace(string(remote)), Branch: "main", Role: "project", Canary: len(enrollment.Projects) == 0})
			}
			if err = enrollment.Validate(); err != nil {
				return err
			}
		}
		snapshot, err := tree.Read(".standards/repository.yml")
		if err != nil {
			return err
		}
		req := engine.Request{Mode: "upgrade", Automation: true}
		if !snapshot.Exists {
			if command == "sync" {
				return fmt.Errorf("initial onboarding is required")
			}
			req = engine.Request{Mode: "adopt", Profile: *profile, ComponentPath: *componentPath, DecisionPath: *decision, Instructions: *instructions, Automation: true}
		}
		plan, err := tree.Plan(req)
		if err != nil {
			return err
		}
		if err = tree.Apply(plan); err != nil {
			return err
		}
		binary, err := os.Executable()
		if err != nil {
			return err
		}
		state := ""
		if *fleet != "" {
			name, err := filepath.Abs(*fleet)
			if err != nil {
				return err
			}
			state = filepath.Dir(name)
		}
		if err = automation.InstallRuntime(root, binary, state); err != nil {
			return err
		}
		if command == "onboard" {
			if err = automation.Save(*fleet, enrollment); err != nil {
				return err
			}
		}
		_, err = fmt.Fprintf(out, "applied %d changes; autonomous adapters installed\n", len(plan.Changes))
		return err
	}
	return fmt.Errorf("unknown automation command")
}

func forwardRuntime(root, command string, args []string, out, errOut io.Writer) (bool, error) {
	if os.Getenv("DEV_STANDARDS_MAINTENANCE") == "1" {
		return false, nil
	}
	binary, err := automation.MatchingRuntime(root)
	if err != nil || binary == "" {
		return false, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, append([]string{command}, args...)...)
	cmd.Env = append(os.Environ(), "DEV_STANDARDS_DELEGATED=1")
	cmd.Stdin = os.Stdin
	cmd.Stdout = out
	cmd.Stderr = errOut
	return true, cmd.Run()
}
