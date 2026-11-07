//go:build !windows

package harness

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type fakeSecrets map[string]string

func (f fakeSecrets) Read(_ context.Context, ref string) (string, error) { return f[ref], nil }

func testKeyPEM(t *testing.T) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))
}

func TestAppTokensRestrictsRepositoryAndCaches(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
			t.Errorf("missing JWT")
		}
		switch r.URL.Path {
		case "/repos/o/n/installation":
			io.WriteString(w, `{"id":42}`)
		case "/app/installations/42/access_tokens":
			calls++
			var body struct {
				Repositories []string          `json:"repositories"`
				Permissions  map[string]string `json:"permissions"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if len(body.Repositories) != 1 || body.Repositories[0] != "n" {
				t.Errorf("repositories=%v", body.Repositories)
			}
			if _, ok := body.Permissions["workflows"]; ok {
				t.Errorf("workflows permission must not be requested")
			}
			io.WriteString(w, `{"token":"short-lived"}`)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()
	tokens := &AppTokens{
		Secrets: fakeSecrets{AppIDRef: "123", AppKeyRef: testKeyPEM(t)},
		Repo:    "o/n",
		API:     server.URL,
		HTTP:    server.Client(),
		Now:     func() time.Time { return time.Unix(1_800_000_000, 0) },
	}
	for range 2 {
		token, err := tokens.Token(context.Background())
		if err != nil || token != "short-lived" {
			t.Fatalf("token=%q err=%v", token, err)
		}
	}
	if calls != 1 {
		t.Fatalf("token was minted %d times", calls)
	}
}

func TestOPSecretsPassesTokenOnlyToOP(t *testing.T) {
	runner := &scriptRunner{responses: map[string]response{
		"security": {out: "sa-token\n"},
		"op":       {out: "value\n"},
	}}
	value, err := OPSecrets{Run: runner}.Read(context.Background(), AppIDRef)
	if err != nil || value != "value" {
		t.Fatalf("value=%q err=%v", value, err)
	}
	if runner.calls[0].Name != "security" || len(runner.calls[0].Env) != 0 {
		t.Fatalf("call=%+v", runner.calls[0])
	}
	op := runner.calls[1]
	if op.Name != "op" || op.Env[0] != "OP_SERVICE_ACCOUNT_TOKEN=sa-token" || op.Secrets[0] != "sa-token" {
		t.Fatalf("call=%+v", op)
	}
}
