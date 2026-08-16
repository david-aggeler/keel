package exec_test

import (
	"context"
	"fmt"

	procexec "github.com/david-aggeler/keel/exec"
	logging "github.com/david-aggeler/keel/log"
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
		Logger:  logging.Discard(),
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
