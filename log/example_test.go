package log_test

import (
	"bytes"
	"encoding/json"
	"fmt"

	logging "github.com/david-aggeler/keel/log"
)

// ExampleNew shows constructing a JSON logger over a caller-supplied writer and
// reading back the single record it emits. Production callers leave Writer nil
// to log to os.Stdout; here a bytes.Buffer captures the output so the example is
// deterministic.
//
// DHF-TEST: keel/user_need-1 (keel/ac-48)
func ExampleNew() {
	var buf bytes.Buffer
	logger := logging.New(logging.Config{Service: "demo", Writer: &buf})

	logger.Info("service starting", "port", 8080)

	var rec map[string]any
	_ = json.Unmarshal(buf.Bytes(), &rec)
	fmt.Println(rec["service"], rec["level"], rec["msg"], rec["port"])
	// Output: demo info service starting 8080
}

// ExampleRedactString shows the redaction contract every keel/log sink applies
// at the log boundary: a password embedded in a DSN is masked before it is
// logged, so callers never have to pre-scrub the values they log.
//
// DHF-TEST: keel/user_need-1 (keel/ac-48)
func ExampleRedactString() {
	fmt.Println(logging.RedactString("dialing postgres://user:sesame@db:5432/app"))
	// Output: dialing postgres://***:***@db:5432/app
}
