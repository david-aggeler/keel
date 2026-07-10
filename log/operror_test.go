package log_test

// operror_test.go — TDD tests for OperationalError (pkg/logging/operror.go).
//
// These tests are written RED-first, before operror.go exists.
// They MUST fail to compile until the implementation lands.
//
// Exception: TestRedactErr_FlattenNoWrap in redactstring_test.go is green now
// (it pins existing behavior). See that file.

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	logging "github.com/david-aggeler/keel/log"
)

// ---------------------------------------------------------------------------
// TestOperationalError_Error
// Proves Error() renders Op/Message/Err joined by ": ", skipping empty
// segments. The all-empty case documents the contract: returns "".
// ---------------------------------------------------------------------------

func TestOperationalError_Error(t *testing.T) {
	sentinel := errors.New("underlying cause")

	tests := []struct {
		name    string
		opErr   *logging.OperationalError
		wantErr string
	}{
		{
			name:    "all fields populated",
			opErr:   &logging.OperationalError{Op: "link_blocks", Message: "cross-product link rejected", Err: sentinel},
			wantErr: "link_blocks: cross-product link rejected: underlying cause",
		},
		{
			name:    "op empty",
			opErr:   &logging.OperationalError{Op: "", Message: "something went wrong", Err: sentinel},
			wantErr: "something went wrong: underlying cause",
		},
		{
			name:    "message empty",
			opErr:   &logging.OperationalError{Op: "validate_dto", Message: "", Err: sentinel},
			wantErr: "validate_dto: underlying cause",
		},
		{
			name:    "err nil",
			opErr:   &logging.OperationalError{Op: "validate_dto", Message: "bad input", Err: nil},
			wantErr: "validate_dto: bad input",
		},
		{
			name:    "op and err no message",
			opErr:   &logging.OperationalError{Op: "link_blocks", Message: "", Err: sentinel},
			wantErr: "link_blocks: underlying cause",
		},
		{
			name:    "message and err no op",
			opErr:   &logging.OperationalError{Op: "", Message: "bad input", Err: sentinel},
			wantErr: "bad input: underlying cause",
		},
		{
			name:    "only op",
			opErr:   &logging.OperationalError{Op: "link_blocks", Message: "", Err: nil},
			wantErr: "link_blocks",
		},
		{
			name:    "only message",
			opErr:   &logging.OperationalError{Op: "", Message: "something happened", Err: nil},
			wantErr: "something happened",
		},
		{
			name:    "only err",
			opErr:   &logging.OperationalError{Op: "", Message: "", Err: sentinel},
			wantErr: "underlying cause",
		},
		{
			// All empty: returns the sentinel fallback rather than an empty string.
			// Callers must set at least Op or Message; this guard prevents silent
			// empty errors in logs.
			name:    "all empty returns fallback",
			opErr:   &logging.OperationalError{Op: "", Message: "", Err: nil},
			wantErr: "operational error",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.opErr.Error()
			if got != tc.wantErr {
				t.Errorf("Error() = %q, want %q", got, tc.wantErr)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestOperationalError_UnwrapChain
// Proves errors.Is/errors.As traverse through the OperationalError carrier to
// the wrapped Err. A regression removing Unwrap() would silently break callers;
// this asserts the traversal form, not just opErr.Unwrap() != nil.
// ---------------------------------------------------------------------------

func TestOperationalError_UnwrapChain(t *testing.T) {
	ErrSentinel := errors.New("sentinel")

	t.Run("errors.Is traverses carrier to wrapped Err", func(t *testing.T) {
		opErr := &logging.OperationalError{
			Op:      "link_blocks",
			Message: "cross-product link rejected",
			Err:     ErrSentinel,
		}
		if !errors.Is(opErr, ErrSentinel) {
			t.Error("errors.Is(opErr, ErrSentinel) = false, want true — Unwrap() is not delegating correctly")
		}
	})

	t.Run("errors.Is traverses multi-level chain through carrier", func(t *testing.T) {
		// sentinel → wrapped by fmt.Errorf → wrapped by OperationalError
		inner := errors.New("inner cause")
		mid := fmt.Errorf("mid layer: %w", inner)
		opErr := &logging.OperationalError{
			Op:  "validate_dto",
			Err: mid,
		}
		if !errors.Is(opErr, inner) {
			t.Error("errors.Is through OperationalError→wrappedErr chain should be true")
		}
	})

	t.Run("errors.Is returns false for unrelated sentinel", func(t *testing.T) {
		other := errors.New("other error")
		opErr := &logging.OperationalError{
			Op:  "op",
			Err: errors.New("something unrelated"),
		}
		if errors.Is(opErr, other) {
			t.Error("errors.Is(opErr, other) = true, want false — Unwrap must not match unrelated errors")
		}
	})

	t.Run("errors.Is returns false when Err is nil", func(t *testing.T) {
		opErr := &logging.OperationalError{Op: "op", Message: "msg", Err: nil}
		if errors.Is(opErr, ErrSentinel) {
			t.Error("errors.Is on nil-Err carrier should be false")
		}
	})
}

// ---------------------------------------------------------------------------
// TestOperationalError_LogValue
// Proves LogValue emits a GroupValue nested under the caller-chosen attr key.
// Assertions are on key PRESENCE, not order — map iteration over Metadata is
// non-deterministic (reviewer P2); do not compare raw JSON byte strings.
// ---------------------------------------------------------------------------

func TestOperationalError_LogValue(t *testing.T) {
	logger, capture := newJSONCaptureLogger("test-svc")

	opErr := &logging.OperationalError{
		Op:      "link_blocks",
		Message: "cross-product link rejected",
		Err:     errors.New("product mismatch"),
		Metadata: map[string]any{
			"reltype":        "depends_on",
			"src_product_id": int64(42),
			"dst_product_id": int64(99),
		},
	}

	logger.Warn("relation rule violation", slog.Any("err", opErr))

	got := capture.LastJSON()
	if got == nil {
		t.Fatal("LastJSON returned nil — no log output captured")
	}

	// slog's JSONHandler serializes a GroupValue as a nested JSON object under
	// the attr key. Assertion is via nested map access, NOT dotted keys.
	errGroup, ok := got["err"].(map[string]any)
	if !ok {
		t.Fatalf("expected got[\"err\"] to be map[string]any (GroupValue), got %T — LogValue is not returning a GroupValue", got["err"])
	}

	// op must be present with expected value.
	if op, _ := errGroup["op"].(string); op != "link_blocks" {
		t.Errorf("err.op = %q, want %q", op, "link_blocks")
	}

	// message must be present with expected value.
	if msg, _ := errGroup["message"].(string); msg != "cross-product link rejected" {
		t.Errorf("err.message = %q, want %q", msg, "cross-product link rejected")
	}

	// root_cause must be present when Err is non-nil.
	if rc, _ := errGroup["root_cause"].(string); rc != "product mismatch" {
		t.Errorf("err.root_cause = %q, want %q", rc, "product mismatch")
	}

	// string metadata field must be present.
	if rt, _ := errGroup["reltype"].(string); rt != "depends_on" {
		t.Errorf("err.reltype = %q, want %q", rt, "depends_on")
	}

	// non-string metadata fields must be present (KD8: emitted via slog.Any without redaction).
	// JSON numbers unmarshal to float64 when decoded into map[string]any.
	if srcID, ok := errGroup["src_product_id"]; !ok {
		t.Error("err.src_product_id missing from group — non-string metadata must be emitted")
	} else if srcID != float64(42) {
		t.Errorf("err.src_product_id = %v (%T), want float64(42)", srcID, srcID)
	}
	if dstID, ok := errGroup["dst_product_id"]; !ok {
		t.Error("err.dst_product_id missing from group")
	} else if dstID != float64(99) {
		t.Errorf("err.dst_product_id = %v (%T), want float64(99)", dstID, dstID)
	}

	// G1 reserved keys must NOT appear inside the err group — LogValue must never
	// emit ts/level/msg/service (Construction constraint from Research Findings).
	for _, reserved := range []string{"ts", "level", "msg", "service"} {
		if _, present := errGroup[reserved]; present {
			t.Errorf("err group must not contain G1 reserved key %q", reserved)
		}
	}
}

func TestOperationalError_LogValue_NilErr_NoRootCause(t *testing.T) {
	// When Err is nil, root_cause must not appear in the emitted group.
	logger, capture := newJSONCaptureLogger("test-svc")

	opErr := &logging.OperationalError{
		Op:      "validate_dto",
		Message: "bad input",
		Err:     nil,
	}

	logger.Warn("validation failed", slog.Any("err", opErr))

	got := capture.LastJSON()
	if got == nil {
		t.Fatal("LastJSON returned nil")
	}

	errGroup, ok := got["err"].(map[string]any)
	if !ok {
		t.Fatalf("expected got[\"err\"] to be map[string]any, got %T", got["err"])
	}

	if _, present := errGroup["root_cause"]; present {
		t.Error("err.root_cause must not be present when Err is nil")
	}
}

// ---------------------------------------------------------------------------
// TestOperationalError_LogValue_RedactsRootCause
// Proves that Err carrying a DSN userinfo and a Bearer token is redacted
// in LogValue via RedactString. The raw Error() string is NOT redacted —
// redaction applies only at the log boundary (KD10).
// ---------------------------------------------------------------------------

func TestOperationalError_LogValue_RedactsRootCause(t *testing.T) {
	logger, capture := newJSONCaptureLogger("test-svc")

	rawErr := errors.New("connect postgres://admin:s3cret@db.host:5432/mydb — auth header: Bearer abc123")
	opErr := &logging.OperationalError{
		Op:      "db_query",
		Message: "database connection failed",
		Err:     rawErr,
	}

	logger.Error("db failure", slog.Any("err", opErr))

	got := capture.LastJSON()
	if got == nil {
		t.Fatal("LastJSON returned nil")
	}

	errGroup, ok := got["err"].(map[string]any)
	if !ok {
		t.Fatalf("expected got[\"err\"] to be map[string]any, got %T", got["err"])
	}

	rc, _ := errGroup["root_cause"].(string)

	// DSN password must be stripped.
	if strings.Contains(rc, "s3cret") {
		t.Errorf("root_cause still contains DSN password: %s", rc)
	}
	if !strings.Contains(rc, "://***:***@") {
		t.Errorf("root_cause missing redacted DSN marker, got: %s", rc)
	}

	// Bearer token must be stripped.
	if strings.Contains(rc, "abc123") {
		t.Errorf("root_cause still contains bearer token: %s", rc)
	}
	if !strings.Contains(rc, "Bearer [REDACTED]") {
		t.Errorf("root_cause missing bearer redaction marker, got: %s", rc)
	}
}

// ---------------------------------------------------------------------------
// TestOperationalError_LogValue_RedactsStringMetadata
// Proves string metadata values are routed through RedactString (KD8).
// A non-string metadata value (int) must be emitted unchanged — proving
// the type-switch is correct and non-strings are not silently dropped.
// ---------------------------------------------------------------------------

func TestOperationalError_LogValue_RedactsStringMetadata(t *testing.T) {
	logger, capture := newJSONCaptureLogger("test-svc")

	opErr := &logging.OperationalError{
		Op:      "http_call",
		Message: "upstream request failed",
		Err:     errors.New("timeout"),
		Metadata: map[string]any{
			"auth_header": "Bearer abc123", // string — must be redacted
			"retry_count": 3,               // int — must pass through unchanged (KD8)
		},
	}

	logger.Error("upstream error", slog.Any("err", opErr))

	got := capture.LastJSON()
	if got == nil {
		t.Fatal("LastJSON returned nil")
	}

	errGroup, ok := got["err"].(map[string]any)
	if !ok {
		t.Fatalf("expected got[\"err\"] to be map[string]any, got %T", got["err"])
	}

	// String metadata value: Bearer token must be redacted.
	authHeader, _ := errGroup["auth_header"].(string)
	if strings.Contains(authHeader, "abc123") {
		t.Errorf("auth_header still contains token: %s", authHeader)
	}
	if !strings.Contains(authHeader, "Bearer [REDACTED]") {
		t.Errorf("auth_header missing redaction marker, got: %s", authHeader)
	}

	// Non-string metadata value: must pass through unchanged per KD8.
	// JSON numbers unmarshal to float64.
	retryCount, ok := errGroup["retry_count"]
	if !ok {
		t.Error("retry_count missing from group — non-string metadata must be emitted (KD8)")
	} else if retryCount != float64(3) {
		t.Errorf("retry_count = %v (%T), want float64(3)", retryCount, retryCount)
	}
}

// ---------------------------------------------------------------------------
// TestOperationalError_FieldSetParityVsFlat
// The §5 pilot comparison test. Emits the same failure context in both the
// flat KD1 form and the OperationalError carrier form, captures both, and
// asserts the same six context values are present in both — at top-level for
// the flat form and under got["err"].(map[string]any) for the carrier form.
//
// Additionally asserts the flat form does NOT redact a bearer token in a
// string value (it has no redaction path), while the carrier form DOES —
// proving the redaction-by-default gap that justifies the carrier (axis 3
// of the §5 comparison).
// ---------------------------------------------------------------------------

func TestOperationalError_FieldSetParityVsFlat(t *testing.T) {
	logger, capture := newJSONCaptureLogger("test-svc")

	// Shared context values (mirrors the MakeLinkHandler cross-product rejection).
	const (
		rtSlug       = "depends_on"
		srcProduct   = "alpha"
		dstProduct   = "beta"
		sensitiveVal = "Bearer tok-secret" // only carrier redacts this
	)
	srcProductID := int64(10)
	dstProductID := int64(20)

	// --- Flat KD1 form (before, CR-0221 style) ---
	logger.Warn("relation rule violation",
		"op", "link_"+rtSlug,
		"rule", "cross_product_rejected",
		"reltype", rtSlug,
		"src_product", srcProduct,
		"dst_product", dstProduct,
		"src_product_id", srcProductID,
		"dst_product_id", dstProductID,
		"auth_note", sensitiveVal, // emitted raw — no redaction path in flat form
	)

	flat := capture.LastJSON()
	if flat == nil {
		t.Fatal("flat form: LastJSON returned nil")
	}

	capture.Reset()

	// --- OperationalError carrier form (after) ---
	opErr := &logging.OperationalError{
		Op:      "link_" + rtSlug,
		Message: "cross-product link rejected",
		Metadata: map[string]any{
			"rule":           "cross_product_rejected",
			"reltype":        rtSlug,
			"src_product":    srcProduct,
			"dst_product":    dstProduct,
			"src_product_id": srcProductID,
			"dst_product_id": dstProductID,
			"auth_note":      sensitiveVal, // string — carrier redacts this
		},
	}

	logger.Warn("relation rule violation", slog.Any("err", opErr))

	carrier := capture.LastJSON()
	if carrier == nil {
		t.Fatal("carrier form: LastJSON returned nil")
	}

	errGroup, ok := carrier["err"].(map[string]any)
	if !ok {
		t.Fatalf("carrier: expected got[\"err\"] to be map[string]any, got %T", carrier["err"])
	}

	// Assert parity: same six context field values appear in both forms.
	// Flat form has them at top level; carrier has them under err group.
	type fieldCheck struct {
		key       string
		flatVal   any
		carrierFn func() any
	}

	checks := []fieldCheck{
		{"reltype", flat["reltype"], func() any { return errGroup["reltype"] }},
		{"src_product", flat["src_product"], func() any { return errGroup["src_product"] }},
		{"dst_product", flat["dst_product"], func() any { return errGroup["dst_product"] }},
		// JSON numbers unmarshal to float64 — compare as float64.
		{"src_product_id", flat["src_product_id"], func() any { return errGroup["src_product_id"] }},
		{"dst_product_id", flat["dst_product_id"], func() any { return errGroup["dst_product_id"] }},
		{"rule", flat["rule"], func() any { return errGroup["rule"] }},
	}

	for _, c := range checks {
		if c.flatVal == nil {
			t.Errorf("flat form missing field %q", c.key)
			continue
		}
		carrierVal := c.carrierFn()
		if carrierVal == nil {
			t.Errorf("carrier form missing field %q in err group", c.key)
			continue
		}
		// String equality for string fields; numeric equality handled by float64 for JSON.
		if fmt.Sprintf("%v", c.flatVal) != fmt.Sprintf("%v", carrierVal) {
			t.Errorf("field %q: flat=%v, carrier=%v — mismatch", c.key, c.flatVal, carrierVal)
		}
	}

	// Axis 3: both flat attributes and carrier metadata redact sensitiveVal.
	// CR-0333 routes flat slog attributes through the shared sink redaction path.
	flatAuthNote, _ := flat["auth_note"].(string)
	if strings.Contains(flatAuthNote, "tok-secret") {
		t.Errorf("flat form must redact auth_note, still contains secret: %s", flatAuthNote)
	}
	if !strings.Contains(flatAuthNote, "Bearer [REDACTED]") {
		t.Errorf("flat form auth_note missing redaction marker, got: %s", flatAuthNote)
	}

	// Carrier form: auth_note (string metadata) must be redacted.
	carrierAuthNote, _ := errGroup["auth_note"].(string)
	if strings.Contains(carrierAuthNote, "tok-secret") {
		t.Errorf("carrier form must redact auth_note, still contains secret: %s", carrierAuthNote)
	}
	if !strings.Contains(carrierAuthNote, "Bearer [REDACTED]") {
		t.Errorf("carrier form auth_note missing redaction marker, got: %s", carrierAuthNote)
	}
}

// ---------------------------------------------------------------------------
// TestOperationalError_NilReceiver
// Proves nil *OperationalError is safe: Error() returns "<nil>" and logging
// via slog.Any does not panic.
// ---------------------------------------------------------------------------

func TestOperationalError_NilReceiver(t *testing.T) {
	var nilErr *logging.OperationalError

	t.Run("Error returns <nil>", func(t *testing.T) {
		got := nilErr.Error()
		if got != "<nil>" {
			t.Errorf("Error() on nil receiver = %q, want %q", got, "<nil>")
		}
	})

	t.Run("logging via slog.Any does not panic", func(t *testing.T) {
		logger, capture := newJSONCaptureLogger("test-svc")

		// Must not panic.
		logger.Warn("test nil carrier", slog.Any("err", (*logging.OperationalError)(nil)))

		got := capture.LastJSON()
		if got == nil {
			t.Fatal("LastJSON returned nil — log output expected")
		}
		// The err group must be absent or empty (no panicking fields).
		if errGroup, ok := got["err"].(map[string]any); ok {
			if len(errGroup) != 0 {
				t.Errorf("expected empty err group for nil carrier, got: %v", errGroup)
			}
		}
		// If "err" is absent entirely that is also acceptable.
	})
}

// ---------------------------------------------------------------------------
// TestOperationalError_AllEmpty
// Proves &OperationalError{} returns the fallback sentinel, not "".
// ---------------------------------------------------------------------------

func TestOperationalError_AllEmpty(t *testing.T) {
	opErr := &logging.OperationalError{}
	got := opErr.Error()
	if got != "operational error" {
		t.Errorf("Error() on all-empty struct = %q, want %q", got, "operational error")
	}
}

// ---------------------------------------------------------------------------
// TestOperationalError_LogValue_ReservedKeyCollision
// Proves that Metadata keys colliding with the carrier's reserved group keys
// (op, message, root_cause) are silently dropped; the carrier's own values win
// and no duplicates appear. A non-reserved key is still emitted.
// ---------------------------------------------------------------------------

func TestOperationalError_LogValue_ReservedKeyCollision(t *testing.T) {
	logger, capture := newJSONCaptureLogger("test-svc")

	opErr := &logging.OperationalError{
		Op:      "carrier_op",
		Message: "carrier_message",
		Err:     errors.New("carrier_root_cause"),
		Metadata: map[string]any{
			"op":         "metadata_op_value",         // reserved — must be dropped
			"message":    "metadata_message_value",    // reserved — must be dropped
			"root_cause": "metadata_root_cause_value", // reserved — must be dropped
			"extra_key":  "extra_value",               // normal — must be present
		},
	}

	logger.Warn("collision test", slog.Any("err", opErr))

	got := capture.LastJSON()
	if got == nil {
		t.Fatal("LastJSON returned nil")
	}

	errGroup, ok := got["err"].(map[string]any)
	if !ok {
		t.Fatalf("expected got[\"err\"] to be map[string]any, got %T", got["err"])
	}

	// Carrier's own values must be present and unmodified.
	if op, _ := errGroup["op"].(string); op != "carrier_op" {
		t.Errorf("err.op = %q, want %q (carrier value must win)", op, "carrier_op")
	}
	if msg, _ := errGroup["message"].(string); msg != "carrier_message" {
		t.Errorf("err.message = %q, want %q (carrier value must win)", msg, "carrier_message")
	}
	if rc, _ := errGroup["root_cause"].(string); rc != "carrier_root_cause" {
		t.Errorf("err.root_cause = %q, want %q (carrier value must win)", rc, "carrier_root_cause")
	}

	// Normal metadata key must still be present.
	if extra, _ := errGroup["extra_key"].(string); extra != "extra_value" {
		t.Errorf("err.extra_key = %q, want %q", extra, "extra_value")
	}
}
