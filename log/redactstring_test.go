package log_test

// redactstring_test.go — Tests for RedactString (new exported symbol, KD7)
// and a regression pin for RedactErr's flatten-no-wrap contract.
//
// TestRedactErr_FlattenNoWrap MUST be GREEN against the current codebase —
// it pins existing behavior so the KD7 refactor (extracting redactString,
// making RedactErr delegate) cannot silently introduce %w.
//
// TestRedactString cases MUST FAIL TO COMPILE until RedactString is added to
// pkg/logging/logging.go (or operror.go) — that is the intended red state.

import (
	"errors"
	"strings"
	"testing"

	logging "github.com/david-aggeler/keel/log"
)

// ---------------------------------------------------------------------------
// TestRedactErr_FlattenNoWrap  (regression PIN — must be GREEN now)
//
// Asserts that errors.Is(RedactErr(sentinel), sentinel) == false.
// RedactErr documents: "Returns a flattened fmt.Errorf("%s", s) — a fresh
// error with no wrap, so errors.Is/errors.As do NOT see through a redacted
// error." The KD7 refactor must keep this byte-identical.
//
// If this test fails after the refactor, the coder introduced %w.
// ---------------------------------------------------------------------------

func TestRedactErr_FlattenNoWrap(t *testing.T) {
	sentinel := errors.New("original sensitive error")
	redacted := logging.RedactErr(sentinel)

	if redacted == nil {
		t.Fatal("RedactErr(non-nil) returned nil")
	}

	// The core assertion: errors.Is must NOT traverse through a redacted error.
	// If it does, RedactErr's flatten-no-wrap contract is broken.
	if errors.Is(redacted, sentinel) {
		t.Error("errors.Is(RedactErr(sentinel), sentinel) = true — RedactErr must NOT wrap; contract is flatten-no-wrap")
	}

	// Sanity: the error string itself is non-empty (RedactErr didn't erase the message).
	if redacted.Error() == "" {
		t.Error("RedactErr returned an error with empty string — should preserve the message")
	}

	// Confirm it IS a different error object, not the same pointer.
	if redacted == sentinel {
		t.Error("RedactErr returned the same error pointer — it must create a new flattened error")
	}
}

// ---------------------------------------------------------------------------
// TestRedactString  (will FAIL TO COMPILE until RedactString is exported — RED)
//
// Four cases covering the full contract of the string→string sibling of RedactErr:
//   - DSN with user:pass@ — password stripped
//   - Bearer token — token stripped
//   - Clean string — returned unchanged
//   - Empty string — returned unchanged (not nil panic, not empty-becomes-something)
// ---------------------------------------------------------------------------

func TestRedactString(t *testing.T) {
	tests := []struct {
		name  string
		input string
		check func(t *testing.T, got string)
	}{
		{
			name:  "DSN password stripped",
			input: "connect postgres://admin:s3cret@db.host:5432/mydb: connection refused",
			check: func(t *testing.T, got string) {
				if strings.Contains(got, "s3cret") {
					t.Errorf("RedactString output still contains password: %s", got)
				}
				if !strings.Contains(got, "://***:***@") {
					t.Errorf("RedactString missing redacted DSN marker, got: %s", got)
				}
				// Non-sensitive parts must be preserved.
				if !strings.Contains(got, "connection refused") {
					t.Errorf("RedactString dropped non-sensitive content, got: %s", got)
				}
			},
		},
		{
			name:  "bearer token stripped",
			input: "auth failed: Bearer xyz-token-abc",
			check: func(t *testing.T, got string) {
				if strings.Contains(got, "xyz-token-abc") {
					t.Errorf("RedactString output still contains bearer token: %s", got)
				}
				if !strings.Contains(got, "Bearer [REDACTED]") {
					t.Errorf("RedactString missing bearer redaction marker, got: %s", got)
				}
				if !strings.Contains(got, "auth failed:") {
					t.Errorf("RedactString dropped non-sensitive prefix, got: %s", got)
				}
			},
		},
		{
			name:  "clean string returned unchanged",
			input: "no secrets here, just a regular error message",
			check: func(t *testing.T, got string) {
				want := "no secrets here, just a regular error message"
				if got != want {
					t.Errorf("RedactString(%q) = %q, want %q — clean strings must not be altered", want, got, want)
				}
			},
		},
		{
			name:  "empty string returned unchanged",
			input: "",
			check: func(t *testing.T, got string) {
				if got != "" {
					t.Errorf("RedactString(\"\") = %q, want empty string", got)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := logging.RedactString(tc.input)
			tc.check(t, got)
		})
	}
}

// TestRedactString_TokenOnlyUserinfo asserts that a token-only userinfo PAT
// (https://TOKEN@host/path) is redacted by RedactString (finding #11).
// This form has no colon separator — it is distinct from the user:pass@host form.
func TestRedactString_TokenOnlyUserinfo(t *testing.T) {
	pat := "ghp_abc123XYZ"
	input := "https://" + pat + "@github.com/org/repo.git"
	got := logging.RedactString(input)
	if strings.Contains(got, pat) {
		t.Errorf("RedactString must strip token-only userinfo PAT %q; got: %s", pat, got)
	}
	if !strings.Contains(got, "github.com") {
		t.Errorf("RedactString must preserve hostname; got: %s", got)
	}
}

// TestRedactString_TokenQueryParam asserts that a ?token= query-param PAT
// is redacted by RedactString (finding #11).
func TestRedactString_TokenQueryParam(t *testing.T) {
	pat := "myPAT_secret99"
	input := "https://gitea.internal/vault/repo.git?token=" + pat
	got := logging.RedactString(input)
	if strings.Contains(got, pat) {
		t.Errorf("RedactString must strip ?token= PAT %q; got: %s", pat, got)
	}
}

// TestRedactString_AccessTokenQueryParam asserts that ?access_token= PAT form
// is also redacted (finding #11, complement of token= form).
func TestRedactString_AccessTokenQueryParam(t *testing.T) {
	pat := "gho_accessToken42"
	input := "https://api.github.com/repos/x?access_token=" + pat
	got := logging.RedactString(input)
	if strings.Contains(got, pat) {
		t.Errorf("RedactString must strip ?access_token= PAT %q; got: %s", pat, got)
	}
}
