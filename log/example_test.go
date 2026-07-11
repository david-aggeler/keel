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
	logger, err := logging.New(logging.Config{Service: "demo", Console: logging.ConsoleJSON, Writer: &buf})
	if err != nil {
		fmt.Println(err)
		return
	}

	logger.Info("service starting", "port", 8080)

	var rec map[string]any
	_ = json.Unmarshal(buf.Bytes(), &rec)
	fmt.Println(rec["service"], rec["level"], rec["msg"], rec["port"])
	// Output: demo INFO service starting 8080
}
