package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	bundle "github.com/tqer39/dev-standards"
	"github.com/tqer39/dev-standards/internal/engine"
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }
func run(args []string, out, errOut io.Writer) int {
	err := execute(args, out, errOut)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		fmt.Fprintln(errOut, string(engine.JSON(map[string]any{"error": map[string]any{"code": "operation_failed", "message": err.Error()}})))
	}
	return engine.ExitCode(err)
}
func execute(args []string, out, errOut io.Writer) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" {
		fmt.Fprintln(out, "dev-standards inspect|adopt|plan|apply|check|verify|format-check|catalog|version\n\n対象は Git リポジトリのルート。各コマンドの --help で引数を確認できます。")
		return nil
	}
	if args[0] == "version" || args[0] == "--version" {
		fmt.Fprintf(out, "dev-standards %s bundle=%s\n", bundle.Version, bundle.Digest())
		return nil
	}
	command := args[0]
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(errOut)
	root := flags.String("repo", ".", "対象リポジトリのルート")
	var output, profile, decision, planPath string
	switch command {
	case "adopt":
		flags.StringVar(&profile, "profile", "", "言語プロファイル。初期版は go")
		flags.StringVar(&decision, "decision", "", "要件・候補・選定理由を記したリポジトリ相対の Markdown")
		flags.StringVar(&output, "output", "", "計画の保存先。省略すると JSON を標準出力へ出力")
	case "plan":
		flags.StringVar(&output, "output", "", "再適用計画の保存先")
	case "apply":
		flags.StringVar(&planPath, "plan", "", "保存済みの計画 JSON")
	case "inspect", "check", "verify", "format-check", "catalog":
	default:
		return fmt.Errorf("unknown command: %s", command)
	}
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments")
	}
	if command == "catalog" {
		rules, err := bundle.Catalog()
		if err != nil {
			return err
		}
		_, err = out.Write(engine.JSON(rules))
		return err
	}
	tree, err := engine.Open(*root)
	if err != nil {
		return err
	}
	defer tree.Close()
	switch command {
	case "inspect":
		r, err := tree.Inspect()
		if err != nil {
			return err
		}
		_, err = out.Write(engine.JSON(r))
		return err
	case "adopt", "plan":
		req := engine.Request{Mode: "reapply"}
		if command == "adopt" {
			req = engine.Request{Mode: "adopt", Profile: profile, DecisionPath: decision}
		}
		p, err := tree.Plan(req)
		if err != nil {
			return err
		}
		if output != "" {
			if err := engine.SavePlan(output, p); err != nil {
				return err
			}
		}
		if _, err := out.Write(engine.JSON(p)); err != nil {
			return err
		}
		if len(p.Conflicts) > 0 {
			return &engine.Failure{Code: 2, Message: "plan contains conflicts"}
		}
		if len(p.Changes) > 0 {
			return &engine.Failure{Code: 1, Message: "plan contains changes"}
		}
		return nil
	case "apply":
		if planPath == "" {
			return fmt.Errorf("--plan is required")
		}
		p, err := engine.ReadPlan(planPath)
		if err != nil {
			return err
		}
		if err := tree.Apply(p); err != nil {
			return err
		}
		_, err = fmt.Fprintf(out, "applied %d changes\n", len(p.Changes))
		return err
	case "check", "verify":
		var r engine.Report
		var err error
		if command == "verify" {
			r, err = tree.Verify(errOut)
		} else {
			r, err = tree.Check()
		}
		if _, writeErr := out.Write(engine.JSON(r)); writeErr != nil {
			return writeErr
		}
		return err
	case "format-check":
		paths, err := tree.FormatCheck()
		if err != nil {
			return err
		}
		if len(paths) > 0 {
			return &engine.Failure{Code: 1, Message: fmt.Sprintf("gofmt required: %v", paths)}
		}
	}
	return nil
}
