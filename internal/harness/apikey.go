//go:build !windows

package harness

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// OpenAIKeyRef は専用 Vault にある OpenAI の API キーである。
const OpenAIKeyRef = "op://agent-harness/openai/api-key"

// 専用ホームのキーをファイルに保存する。保存先は実装担当の読み取り拒否の範囲にある。
const apiHomeConfig = "forced_login_method = \"api\"\ncli_auth_credentials_store = \"file\"\n"

// SetupAPIKey は API キー専用の Codex ホームを作る。
// ~/.codex の ChatGPT ログインと自律保守に影響しないよう、ホームを分ける。
// キーは標準入力でだけ渡し、引数と環境変数に出さない。
func SetupAPIKey(ctx context.Context, run Runner, secrets Secrets, codex, home string, profile []byte) error {
	key, err := secrets.Read(ctx, OpenAIKeyRef)
	if err != nil {
		return err
	}
	if strings.TrimSpace(key) == "" {
		return errors.New("1Password の OpenAI の API キーが空です")
	}
	if err := os.MkdirAll(home, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(home, 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte(apiHomeConfig), 0o600); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(home, "agent-harness.config.toml"), profile, 0o600); err != nil {
		return err
	}
	if _, err := run.Run(ctx, Cmd{
		Name:    codex,
		Args:    []string{"login", "--with-api-key"},
		Env:     []string{"CODEX_HOME=" + home},
		Stdin:   key,
		Secrets: []string{key},
	}); err != nil {
		return fmt.Errorf("Codex に API キーを登録できません: %w", err)
	}
	return nil
}

// APIKeyLoggedIn は専用ホームが API キーでログインしていることを確認する。
func APIKeyLoggedIn(ctx context.Context, run Runner, codex, home string) error {
	out, err := run.Run(ctx, Cmd{Name: codex, Args: []string{"login", "status"}, Env: []string{"CODEX_HOME=" + home}})
	var runErr *RunError
	if errors.As(err, &runErr) {
		out += runErr.Stderr
	} else if err != nil {
		return err
	}
	if !strings.Contains(strings.ToLower(out), "api key") {
		return errors.New("API キーでログインしていません。agent-harness setup-api-key を実行してください")
	}
	return nil
}
