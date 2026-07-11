package cli_test

import (
	"context"
	"fmt"
	"strings"

	"github.com/david-aggeler/keel/cli"
)

// ExampleCommandSpec shows a small command tree using the same public model for
// dispatch and generated help.
//
// DHF-TEST: keel/requirement-30
func ExampleCommandSpec() {
	root := &cli.CommandSpec{
		Name: "keel-demo",
		Config: cli.Config{
			Program: "keel-demo",
			Usage:   "keel-demo <command> [args]",
			GlobalFlags: []cli.FlagSpec{
				{Name: "mode", Value: "human|ai|json", Default: "human", Short: "Console protocol."},
			},
		},
		Subcommands: []*cli.CommandSpec{
			{
				Name:  "echo",
				Use:   "echo <text>",
				Short: "Print text.",
				Handler: func(_ context.Context, args []string) error {
					fmt.Println(strings.Join(args, " "))
					return nil
				},
			},
		},
	}

	cfg, words, err := cli.ParseGlobalConfig([]string{"--mode", "json", "echo", "ready"})
	if err != nil {
		panic(err)
	}
	fmt.Println(cfg.Mode)
	if err := root.Dispatch(context.Background(), words); err != nil {
		panic(err)
	}

	// Output:
	// json
	// ready
}
