// Package cli provides keel's shared command tree, generated help, and
// UsageError contract for first-party developer CLIs.
//
// DHF-REQ: keel/requirement-21
package cli

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
)

// Mode names the shared console output mode selected by a CLI invocation.
type Mode string

const (
	// ModeHuman renders human-readable console output.
	ModeHuman Mode = "human"
	// ModeAI renders sparse AI-readable console records.
	ModeAI Mode = "ai"
	// ModeJSON renders full JSON console records.
	ModeJSON Mode = "json"
)

// RuntimeConfig is the shared global CLI configuration parsed before dispatch.
type RuntimeConfig struct {
	// Mode is the selected console output mode.
	Mode Mode
	// Verbose enables debug-level console detail.
	Verbose bool
	// NoHeader suppresses the consumer's run banner for machine protocol flows.
	NoHeader bool
	// Help requests generated help instead of command execution.
	Help bool
	// Version requests version output instead of command execution.
	Version bool
}

// Config describes the generated root-help shell around a consumer command tree.
type Config struct {
	// Program is the executable name shown in generated usage.
	Program string
	// RootSummary is the opening paragraph in root help.
	RootSummary string
	// Usage is the primary root usage line without the leading "usage:" prefix.
	Usage string
	// HelpUsage is the root help entry-point usage line.
	HelpUsage string
	// CommandUsage is the per-command help entry-point usage line.
	CommandUsage string
	// GlobalFlags are rendered in the root help global flag table.
	GlobalFlags []FlagSpec
	// ModeHelp contains optional output-mode prose rendered in root help.
	ModeHelp []string
	// Trailing is optional final root-help guidance.
	Trailing string
}

// Handler executes a command with its remaining arguments.
type Handler func(context.Context, []string) error

// CommandSpec is a node in the shared command tree.
type CommandSpec struct {
	// Name is the command token used for tree navigation.
	Name string
	// Use is the explicit usage suffix for this command.
	Use string
	// Short is the one-line summary shown in command lists.
	Short string
	// Long is optional longer command help prose.
	Long string
	// Args describes positional arguments when Use is not supplied.
	Args string
	// Flags are command-specific flags rendered in command help.
	Flags []FlagSpec
	// Subcommands are this command's child command specs.
	Subcommands []*CommandSpec
	// Handler executes this command when dispatched.
	Handler Handler
	// Config is the root help and usage configuration inherited by children.
	Config Config
}

// FlagSpec describes one flag row in generated help.
type FlagSpec struct {
	// Name is the flag name without leading dashes.
	Name string
	// Value is the optional value placeholder shown after the flag name.
	Value string
	// Default is the optional default value shown in help.
	Default string
	// Short is the flag description.
	Short string
}

// UsageError reports invalid CLI usage. Consumers map it to exit code 2.
type UsageError struct {
	// Err is the underlying diagnostic presented to the user.
	Err error
}

// NewUsageError returns a UsageError with a formatted message.
func NewUsageError(format string, args ...any) UsageError {
	return UsageError{Err: fmt.Errorf(format, args...)}
}

// Error returns the usage diagnostic text.
func (e UsageError) Error() string {
	if e.Err == nil {
		return ""
	}
	return e.Err.Error()
}

// Unwrap returns the underlying usage diagnostic.
func (e UsageError) Unwrap() error { return e.Err }

// ExitCode returns the process exit code for usage errors.
func (e UsageError) ExitCode() int { return 2 }

// ParseGlobalConfig parses shared position-independent global flags and returns
// the non-global command words.
func ParseGlobalConfig(argv []string) (RuntimeConfig, []string, error) {
	cfg := RuntimeConfig{Mode: ModeHuman}
	var words []string
	for i := 0; i < len(argv); i++ {
		arg := argv[i]
		switch arg {
		case "--mode":
			if i+1 >= len(argv) {
				return cfg, nil, NewUsageError("--mode requires one of: human, ai, json")
			}
			i++
			mode, err := ParseMode(argv[i])
			if err != nil {
				return cfg, nil, err
			}
			cfg.Mode = mode
		case "-v", "--verbose":
			cfg.Verbose = true
		case "--no-header":
			cfg.NoHeader = true
		case "-h", "--help":
			cfg.Help = true
		case "--version":
			cfg.Version = true
		default:
			words = append(words, arg)
		}
	}
	return cfg, words, nil
}

// ParseMode parses a shared console mode string.
func ParseMode(mode string) (Mode, error) {
	switch Mode(strings.ToLower(mode)) {
	case ModeHuman:
		return ModeHuman, nil
	case ModeAI:
		return ModeAI, nil
	case ModeJSON:
		return ModeJSON, nil
	default:
		return "", NewUsageError("unknown --mode %q: expected human, ai, or json", mode)
	}
}

// Usage returns the generated usage line for this command at path.
func (c *CommandSpec) Usage(path []string) string {
	program := c.program()
	if c.Use != "" {
		return "usage: " + program + " " + c.Use
	}
	if len(path) == 0 {
		if c.Config.Usage != "" {
			return "usage: " + c.Config.Usage
		}
		return "usage: " + program + " <command> [args]"
	}
	parts := append([]string{}, path...)
	if c.Args != "" {
		parts = append(parts, c.Args)
	} else if len(c.Subcommands) > 0 {
		parts = append(parts, SubcommandAlternates(c.Subcommands))
	}
	return "usage: " + program + " " + strings.Join(parts, " ")
}

// Find returns the deepest node matching path, any unmatched remainder, and
// whether the whole path matched.
func (c *CommandSpec) Find(path []string) (*CommandSpec, []string, bool) {
	if len(path) == 0 {
		return c, nil, true
	}
	for _, child := range c.Subcommands {
		if child.Name == path[0] {
			return child.Find(path[1:])
		}
	}
	return c, path, false
}

// Child returns the named direct subcommand.
func (c *CommandSpec) Child(name string) (*CommandSpec, bool) {
	for _, child := range c.Subcommands {
		if child.Name == name {
			return child, true
		}
	}
	return nil, false
}

// Dispatch invokes the deepest matching command handler.
func (c *CommandSpec) Dispatch(ctx context.Context, args []string) error {
	c.InheritConfig()
	if len(args) == 0 {
		return UsageError{Err: fmt.Errorf("%s", c.Usage(nil))}
	}
	node, matched, remaining := c.match(args)
	if len(matched) == 0 {
		return UsageError{Err: fmt.Errorf("unknown command %q\n%s", args[0], c.Usage(nil))}
	}
	if node.Handler == nil {
		return UsageError{Err: fmt.Errorf("%s", node.Usage(matched))}
	}
	return node.Handler(ctx, remaining)
}

func (c *CommandSpec) match(path []string) (*CommandSpec, []string, []string) {
	node := c
	var matched []string
	for len(path) > 0 {
		child, ok := node.Child(path[0])
		if !ok {
			break
		}
		node = child
		matched = append(matched, path[0])
		path = path[1:]
	}
	return node, matched, path
}

// RenderRootHelp writes generated root help.
func (c *CommandSpec) RenderRootHelp(w io.Writer) {
	c.InheritConfig()
	if c.Config.RootSummary != "" {
		fmt.Fprintln(w, c.Config.RootSummary)
		fmt.Fprintln(w)
	}
	fmt.Fprintln(w, "Usage:")
	for _, line := range []string{c.Config.Usage, c.Config.HelpUsage, c.Config.CommandUsage} {
		if line != "" {
			fmt.Fprintf(w, "  %s\n", line)
		}
	}
	if len(c.Config.GlobalFlags) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Global flags:")
		PrintFlagRows(w, c.Config.GlobalFlags)
	}
	if len(c.Subcommands) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Commands:")
		PrintCommandRows(w, c.Subcommands)
	}
	if len(c.Config.ModeHelp) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Output mode:")
		for _, line := range c.Config.ModeHelp {
			fmt.Fprintf(w, "  %s\n", line)
		}
	}
	if c.Config.Trailing != "" {
		fmt.Fprintln(w)
		fmt.Fprintln(w, c.Config.Trailing)
	}
}

// RenderTopicHelp writes help for the command path or root help when path is empty.
func (c *CommandSpec) RenderTopicHelp(w io.Writer, path []string) {
	c.InheritConfig()
	node, remaining, ok := c.Find(path)
	if !ok || len(remaining) > 0 {
		fmt.Fprintf(w, "unknown help topic %q\n", strings.Join(path, " "))
		c.RenderRootHelp(w)
		return
	}
	if node == c {
		c.RenderRootHelp(w)
		return
	}
	node.RenderCommandHelp(w, path)
}

// RenderCommandHelp writes command help for one command node.
func (c *CommandSpec) RenderCommandHelp(w io.Writer, path []string) {
	title := strings.Join(path, " ")
	fmt.Fprintf(w, "%s commands:\n", title)
	if c.Long != "" {
		fmt.Fprintf(w, "  %s\n", c.Long)
	} else if c.Short != "" {
		fmt.Fprintf(w, "  %s\n", c.Short)
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintf(w, "  %s\n", strings.TrimPrefix(c.Usage(path), "usage: "))
	if len(c.Flags) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Flags:")
		PrintFlagRows(w, c.Flags)
	}
	if len(c.Subcommands) == 0 {
		return
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Subcommands:")
	RenderSubcommandHelp(w, path, c.Subcommands, 0)
}

// RenderSubcommandHelp writes a nested subcommand listing.
func RenderSubcommandHelp(w io.Writer, parent []string, commands []*CommandSpec, depth int) {
	ordered := append([]*CommandSpec{}, commands...)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].Name < ordered[j].Name })
	for _, cmd := range ordered {
		path := append(append([]string{}, parent...), cmd.Name)
		indent := strings.Repeat("  ", depth+1)
		use := strings.TrimPrefix(cmd.Usage(path), "usage: "+cmd.program()+" ")
		fmt.Fprintf(w, "%s%s\n", indent, use)
		if cmd.Short != "" {
			fmt.Fprintf(w, "%s    %s\n", indent, cmd.Short)
		}
		if len(cmd.Subcommands) > 0 {
			RenderSubcommandHelp(w, path, cmd.Subcommands, depth+1)
		}
	}
}

// PrintCommandRows writes aligned command summary rows.
func PrintCommandRows(w io.Writer, commands []*CommandSpec) {
	width := 0
	for _, cmd := range commands {
		if len(cmd.Name) > width {
			width = len(cmd.Name)
		}
	}
	for _, cmd := range commands {
		fmt.Fprintf(w, "  %-*s  %s\n", width, cmd.Name, cmd.Short)
	}
}

// PrintFlagRows writes flag help rows.
func PrintFlagRows(w io.Writer, flags []FlagSpec) {
	for _, f := range flags {
		value := ""
		if f.Value != "" {
			value = " " + f.Value
		}
		def := ""
		if f.Default != "" {
			def = " (default " + f.Default + ")"
		}
		fmt.Fprintf(w, "  --%s%s\n      %s%s\n", f.Name, value, f.Short, def)
	}
}

// SubcommandAlternates returns a pipe-separated command-name list.
func SubcommandAlternates(commands []*CommandSpec) string {
	names := make([]string, 0, len(commands))
	for _, child := range commands {
		names = append(names, child.Name)
	}
	return strings.Join(names, "|")
}

// SimpleSpecs builds sorted leaf command specs from descriptions.
func SimpleSpecs(prefix string, descriptions map[string]string) []*CommandSpec {
	keys := make([]string, 0, len(descriptions))
	for name := range descriptions {
		keys = append(keys, name)
	}
	sort.Strings(keys)
	specs := make([]*CommandSpec, 0, len(keys))
	for _, name := range keys {
		specs = append(specs, &CommandSpec{Name: name, Use: prefix + " " + name, Short: descriptions[name]})
	}
	return specs
}

// InheritConfig fills missing child Config values from the root configuration.
func (c *CommandSpec) InheritConfig() {
	c.inheritConfig(c.Config)
}

func (c *CommandSpec) inheritConfig(cfg Config) {
	if c.Config.Program == "" {
		c.Config.Program = cfg.Program
	}
	for _, child := range c.Subcommands {
		child.inheritConfig(c.Config)
	}
}

func (c *CommandSpec) program() string {
	if c.Config.Program != "" {
		return c.Config.Program
	}
	return rootProgram(c)
}

func rootProgram(c *CommandSpec) string {
	if c.Name != "" {
		return c.Name
	}
	return "command"
}
