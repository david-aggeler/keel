package log_test

// redaction_precision_test.go — both directions of redaction (keel/issue-260):
// every row plants a secret AND a non-secret neighbor, and asserts the secret
// is masked while the neighbor survives byte-identical. Tests that only check
// "the secret is gone" cannot see over-redaction.

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	logging "github.com/david-aggeler/keel/log"
)

// DHF-TEST: keel/requirement-177
func TestRedactString_MasksOnlySecretSpan(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		want   string // exact output; "" = check secret/keep only
		secret string
		keep   []string
	}{
		{
			// ac-764
			name:   "DSN user and host survive, password masked",
			input:  "postgres://app:pw-9f8e7d@db.internal:5432/openbrain",
			want:   "postgres://app:***@db.internal:5432/openbrain",
			secret: "pw-9f8e7d",
		},
		{
			name:   "DSN inside an error message",
			input:  "connect postgres://admin:s3cret@db.host:5432/mydb: connection refused",
			want:   "connect postgres://admin:***@db.host:5432/mydb: connection refused",
			secret: "s3cret",
		},
		{
			name:   "password containing a colon",
			input:  "redis://svc:pa:ss@cache:6379/",
			want:   "redis://svc:***@cache:6379/",
			secret: "pa:ss",
		},
		{
			// ac-767
			name:   "token-only userinfo fully masked",
			input:  "https://ghp_abc123@github.com/o/r.git",
			secret: "ghp_abc123",
			keep:   []string{"github.com/o/r.git"},
		},
		{
			// ac-767
			name:   "token-prefixed username masked with the password",
			input:  "https://ghp_abc123:x-oauth-basic@github.com/o/r.git",
			secret: "ghp_abc123",
			keep:   []string{"github.com/o/r.git"},
		},
		{
			name:   "fine-grained GitHub PAT as username",
			input:  "https://github_pat_11AB:x@github.com/o/r.git",
			secret: "github_pat_11AB",
			keep:   []string{"github.com/o/r.git"},
		},
		{
			name:   "GitLab PAT as username",
			input:  "https://" + "glpat-" + "xyz789:x@gitlab.local/o/r.git",
			secret: "glpat-" + "xyz789",
			keep:   []string{"gitlab.local/o/r.git"},
		},
		{
			name:   "token query param keeps host and path",
			input:  "https://gitea.local/api/v1/repos/x?token=ghp_ABCD",
			want:   "https://gitea.local/api/v1/repos/x?token=***",
			secret: "ghp_ABCD",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := logging.RedactString(tc.input)
			if strings.Contains(got, tc.secret) {
				t.Fatalf("RedactString(%q) = %q, leaked secret %q", tc.input, got, tc.secret)
			}
			if tc.want != "" && got != tc.want {
				t.Fatalf("RedactString(%q) = %q, want %q", tc.input, got, tc.want)
			}
			for _, k := range tc.keep {
				if !strings.Contains(got, k) {
					t.Fatalf("RedactString(%q) = %q, dropped non-secret %q", tc.input, got, k)
				}
			}
		})
	}
}

// DHF-TEST: keel/requirement-177
func TestRedaction_AttrKeyClassificationBothDirections(t *testing.T) {
	// ac-766
	const value = "v-4c1b"
	masked := []string{
		"token", "access_token", "mcp_auth_token", "apiToken", "X-Auth-Token",
		"password", "db_password", "dbPassword", "passwd", "secret", "client_secret",
		"Authorization", "pat", "github_pat", "api_key", "apiKey", "apikey",
		"private_key", "access_key", "signing_key",
	}
	kept := []string{
		"token_kind", "tokens_total", "max_tokens", "password_policy",
		"secret_ref_count", "sort_key", "cache_key", "idempotency_key", "path", "key",
	}

	extra := newCaptureHandler()
	var console bytes.Buffer
	logger := mustNewLogger(t, logging.Config{
		Service:  "svc",
		Console:  logging.ConsoleJSON,
		Writer:   &console,
		Handlers: []slog.Handler{extra},
	})
	args := make([]any, 0, 2*(len(masked)+len(kept)))
	for _, k := range append(append([]string{}, masked...), kept...) {
		args = append(args, k, value)
	}
	logger.Info("keys", args...)

	if len(extra.state.records) != 1 {
		t.Fatalf("extra handler records = %d, want 1", len(extra.state.records))
	}
	got := map[string]string{}
	extra.state.records[0].Attrs(func(a slog.Attr) bool {
		got[a.Key] = a.Value.Resolve().String()
		return true
	})
	for _, k := range masked {
		if got[k] != "[REDACTED]" {
			t.Errorf("credential key %q = %q, want [REDACTED]", k, got[k])
		}
	}
	for _, k := range kept {
		if got[k] != value {
			t.Errorf("non-credential key %q = %q, want %q byte-identical", k, got[k], value)
		}
	}
	if strings.Contains(console.String(), `"token_kind":"[REDACTED]"`) {
		t.Errorf("console sink over-redacted token_kind: %s", console.String())
	}
}

// DHF-TEST: keel/requirement-177
func TestRedaction_TokenCountersSurviveHandlerFanOut(t *testing.T) {
	// ac-765: the openbrain recent-log shape.
	extra := newCaptureHandler()
	var console bytes.Buffer
	logger := mustNewLogger(t, logging.Config{
		Service:  "svc",
		Console:  logging.ConsoleJSON,
		Writer:   &console,
		Handlers: []slog.Handler{extra},
	})
	logger.Warn("m", "token_kind", "input", "tokens_total", "12", "mcp_auth_token", "zz-secret")

	if len(extra.state.records) != 1 {
		t.Fatalf("extra handler records = %d, want 1", len(extra.state.records))
	}
	got := map[string]string{}
	extra.state.records[0].Attrs(func(a slog.Attr) bool {
		got[a.Key] = a.Value.Resolve().String()
		return true
	})
	want := map[string]string{"token_kind": "input", "tokens_total": "12", "mcp_auth_token": "[REDACTED]"}
	for k, w := range want {
		if got[k] != w {
			t.Errorf("attr %q = %q, want %q", k, got[k], w)
		}
	}
	if strings.Contains(console.String(), "zz-secret") {
		t.Errorf("console sink leaked mcp_auth_token: %s", console.String())
	}
}
