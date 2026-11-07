//go:build !windows

package harness

import (
	"context"
	"errors"
)

// Gitleaks は差分を検査する。PR 側の設定で検査を緩められないよう、Dir で実行する。
type Gitleaks struct {
	Run Runner
	Bin string
	Dir string
}

func (g Gitleaks) Scan(ctx context.Context, patch []byte) (bool, error) {
	_, err := g.Run.Run(ctx, Cmd{
		Dir:   g.Dir,
		Name:  g.Bin,
		Args:  []string{"stdin", "--no-banner", "--redact", "--exit-code", "1"},
		Stdin: string(patch),
	})
	if err == nil {
		return false, nil
	}
	var runErr *RunError
	if errors.As(err, &runErr) && runErr.Code == 1 {
		return true, nil
	}
	return false, err
}
