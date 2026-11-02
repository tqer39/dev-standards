package automation

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

type Executor interface {
	Run(context.Context, string, string, []string, string) (string, error)
}
type Exec struct{ Log io.Writer }
type limited struct{ bytes.Buffer }

func (b *limited) Write(p []byte) (int, error) {
	n := len(p)
	if b.Len() < 1<<20 {
		keep := min(n, (1<<20)-b.Len())
		b.Buffer.Write(p[:keep])
	}
	return n, nil
}
func (e Exec) Run(ctx context.Context, dir, exe string, args []string, input string) (string, error) {
	if e.Log != nil {
		fmt.Fprintf(e.Log, "[maintenance] %s\n", exe)
	}
	cmd := exec.CommandContext(ctx, exe, args...)
	cmd.Dir = dir
	cmd.Stdin = strings.NewReader(input)
	env := []string{}
	for _, v := range os.Environ() {
		key := strings.ToUpper(strings.SplitN(v, "=", 2)[0])
		sensitive := strings.Contains(key, "TOKEN") || strings.Contains(key, "SECRET") || strings.Contains(key, "PASSWORD") || strings.Contains(key, "CREDENTIAL") || strings.Contains(key, "API_KEY") || strings.HasPrefix(key, "AWS_") || key == "SSH_AUTH_SOCK"
		privileged := exe == "gh" || exe == "git"
		if key == "OPENAI_API_KEY" || key == "CODEX_API_KEY" || key == "CODEX_ACCESS_TOKEN" || (sensitive && !privileged) {
			continue
		}
		env = append(env, v)
	}
	cmd.Env = append(env, "DEV_STANDARDS_MAINTENANCE=1", "MISE_YES=1", "GIT_TERMINAL_PROMPT=0")
	var out, stderr limited
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return out.String(), fmt.Errorf("%s failed: %w\nstdout: %s\nstderr: %s", exe, err, out.String(), stderr.String())
	}
	// Codex reports a successful login status on stderr. Keep other commands'
	// stdout clean, since GitHub and agent responses are parsed as JSON.
	if exe == "codex" && len(args) == 2 && args[0] == "login" && args[1] == "status" {
		return out.String() + stderr.String(), nil
	}
	return out.String(), nil
}
func git(ctx context.Context, x Executor, dir string, args ...string) (string, error) {
	base := []string{"-c", "credential.helper=", "-c", "credential.helper=!gh auth git-credential"}
	return x.Run(ctx, dir, "git", append(base, args...), "")
}
