package log

import (
	"log/slog"
	"os"
)

// CLIProfile returns the console shape for a command-line tool: human-readable
// diagnostics on stderr, leaving stdout to the payload the tool's verbs
// declare. The result is an ordinary [Config]; amend any field before passing
// it to [New].
//
// DHF-REQ: keel/requirement-167, keel/requirement-164
func CLIProfile(service string) Config {
	return Config{
		Service: service,
		Console: ConsolePlain,
		Writer:  os.Stderr,
	}
}

// ServiceProfile returns the console shape for a long-running service: JSON
// records at Info and below on stdout, Warn and above on stderr, so a log
// aggregator that tags stderr as high severity needs no per-deployment
// configuration. Console verbosity starts at Debug because the aggregator,
// not the process, filters; raise ConsoleVerbosity to drop Debug at the source.
// The result is an ordinary [Config]; amend any field before passing it to
// [New].
//
// DHF-REQ: keel/requirement-167
func ServiceProfile(service string) Config {
	return Config{
		Service:          service,
		Console:          ConsoleJSON,
		ConsoleVerbosity: slog.LevelDebug,
		Writer:           os.Stdout,
		WarnWriter:       os.Stderr,
	}
}
