package exec_test

import (
	"context"
	"fmt"
	"io"
	"log/slog"

	procexec "github.com/david-aggeler/keel/exec"
)

// ExampleProcessStart runs a subprocess through keel/exec and reads its captured
// stdout. Every launch emits START/END lifecycle records through the supplied
// logger; here they are discarded so the example output is just the process
// result. ProcessStart returns immediately — Wait blocks for the exit.
//
// DHF-TEST: keel/user_need-1 (keel/ac-48)
func ExampleProcessStart() {
	proc, err := procexec.ProcessStart(context.Background(), procexec.Request{
		Program: "echo",
		Args:    []string{"hello from keel/exec"},
		Logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		panic(err)
	}
	res, err := proc.Wait()
	if err != nil {
		panic(err)
	}
	fmt.Print(res.Stdout)
	// Output: hello from keel/exec
}
