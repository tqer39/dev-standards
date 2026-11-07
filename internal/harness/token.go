//go:build !windows

package harness

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	KeychainService = "agent-harness-op-token"
	AppIDRef        = "op://agent-harness/github-app/app-id"
	AppKeyRef       = "op://agent-harness/github-app/private-key"
	GitHubAPI       = "https://api.github.com"
)

type Secrets interface {
	Read(context.Context, string) (string, error)
}

// OPSecrets はキーチェーンのサービスアカウントのトークンで 1Password を読む。
// トークンは op の子プロセスにだけ渡し、生体認証を求めない。
type OPSecrets struct{ Run Runner }

func (s OPSecrets) Read(ctx context.Context, ref string) (string, error) {
	token, err := s.Run.Run(ctx, Cmd{Name: "security", Args: []string{"find-generic-password", "-s", KeychainService, "-w"}})
	if err != nil {
		return "", errors.New("キーチェーンからサービスアカウントのトークンを読めません")
	}
	token = strings.TrimSpace(token)
	value, err := s.Run.Run(ctx, Cmd{
		Name:    "op",
		Args:    []string{"read", ref},
		Env:     []string{"OP_SERVICE_ACCOUNT_TOKEN=" + token},
		Secrets: []string{token},
	})
	if err != nil {
		return "", fmt.Errorf("1Password から %s を読めません: %w", ref, err)
	}
	return strings.TrimSpace(value), nil
}

type TokenSource interface {
	Token(context.Context) (string, error)
}

// AppTokens は対象リポジトリに限定した短命トークンを発行する。
type AppTokens struct {
	Secrets Secrets
	Repo    string
	API     string
	HTTP    *http.Client
	Now     func() time.Time

	token   string
	expires time.Time
}

func (a *AppTokens) Token(ctx context.Context) (string, error) {
	now := a.Now()
	if a.token != "" && now.Before(a.expires) {
		return a.token, nil
	}
	appID, err := a.Secrets.Read(ctx, AppIDRef)
	if err != nil {
		return "", err
	}
	keyPEM, err := a.Secrets.Read(ctx, AppKeyRef)
	if err != nil {
		return "", err
	}
	jwt, err := signJWT(appID, keyPEM, now)
	if err != nil {
		return "", err
	}
	var installation struct {
		ID int64 `json:"id"`
	}
	if err := a.call(ctx, http.MethodGet, "/repos/"+a.Repo+"/installation", jwt, nil, &installation); err != nil {
		return "", err
	}
	_, name, _ := strings.Cut(a.Repo, "/")
	request := map[string]any{
		"repositories": []string{name},
		"permissions": map[string]string{
			"contents": "write", "pull_requests": "write", "statuses": "write",
			"metadata": "read", "checks": "read", "actions": "read",
		},
	}
	var minted struct {
		Token string `json:"token"`
	}
	path := fmt.Sprintf("/app/installations/%d/access_tokens", installation.ID)
	if err := a.call(ctx, http.MethodPost, path, jwt, request, &minted); err != nil {
		return "", err
	}
	if minted.Token == "" {
		return "", errors.New("GitHub がトークンを返しませんでした")
	}
	a.token = minted.Token
	// 有効期限は 1 時間なので、余裕を持って更新する。
	a.expires = now.Add(50 * time.Minute)
	return a.token, nil
}

func (a *AppTokens) call(ctx context.Context, method, path, jwt string, body, out any) error {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(data)
	}
	request, err := http.NewRequestWithContext(ctx, method, a.API+path, reader)
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+jwt)
	request.Header.Set("Accept", "application/vnd.github+json")
	response, err := a.HTTP.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode >= 300 {
		return fmt.Errorf("GitHub API %s が %d を返しました", path, response.StatusCode)
	}
	return json.NewDecoder(response.Body).Decode(out)
}

func signJWT(appID, keyPEM string, now time.Time) (string, error) {
	block, _ := pem.Decode([]byte(keyPEM))
	if block == nil {
		return "", errors.New("GitHub App の秘密鍵を解釈できません")
	}
	key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		return "", errors.New("GitHub App の秘密鍵を解釈できません")
	}
	encode := base64.RawURLEncoding.EncodeToString
	header := encode([]byte(`{"alg":"RS256","typ":"JWT"}`))
	claims, err := json.Marshal(map[string]any{
		"iat": now.Add(-time.Minute).Unix(),
		"exp": now.Add(9 * time.Minute).Unix(),
		"iss": appID,
	})
	if err != nil {
		return "", err
	}
	signing := header + "." + encode(claims)
	digest := sha256.Sum256([]byte(signing))
	signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		return "", err
	}
	return signing + "." + encode(signature), nil
}
