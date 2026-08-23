// Package cli provides keel's shared command tree, generated help, and
// usage-error contract for first-party developer CLIs.
//
// Consumers describe their CLI as a tree of CommandSpec values. Dispatch walks
// that tree from the root to the deepest matching command, validates
// command-declared flags, and invokes only the matched command's Handler with
// the remaining positional arguments. Commands without handlers, unknown
// commands, and unknown flag-shaped arguments return UsageError so callers can
// render diagnostics consistently and exit with exit code 2.
//
// The same tree is also the source for generated help. Config defines the root
// usage shell, global flag rows, output-mode prose, and trailing guidance;
// command nodes provide their own usage suffixes, summaries, long descriptions,
// flags, and nested subcommands. RenderRootHelp and RenderTopicHelp format that
// model for human help output without requiring each CLI to maintain a second
// hand-written usage text. RenderAllHelp emits that same generated model in one
// full-tree dump for operators and agents that need the whole surface at once.
//
// ParseGlobalConfig handles keel's shared position-independent global flags
// before command dispatch. It returns RuntimeConfig, whose Mode selects the
// console protocol and whose Help, HelpAll, Version, Verbose, and NoHeader
// fields let binaries route shared behavior before invoking the command tree.
//
// DHF-REQ: keel/requirement-21, keel/requirement-30, keel/requirement-57
package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
)

// Mode names the shared console protocol selected by a CLI invocation.
// Human mode is the default terminal-facing stream; AI and JSON modes are
// machine-facing protocols consumed by automation.
type Mode string

const (
	// ModeHuman renders human-readable console output.
	ModeHuman Mode = "human"
	// ModeAI renders sparse AI-readable console records.
	ModeAI Mode = "ai"
	// ModeJSON renders full JSON console records.
	ModeJSON Mode = "json"
)

// RuntimeConfig is the shared global CLI configuration parsed before command
// dispatch. Binaries use it to handle cross-cutting concerns such as output
// protocol, verbosity, generated help, version output, and banner suppression
// before handing the remaining words to a CommandSpec tree.
type RuntimeConfig struct {
	// Mode is the selected console output mode.
	Mode Mode
	// Verbose enables debug-level console detail.
	Verbose bool
	// NoHeader suppresses the consumer's run banner for machine protocol flows.
	NoHeader bool
	// Help requests generated help instead of command execution.
	Help bool
	// HelpAll requests the full generated help tree instead of command execution.
	HelpAll bool
	// HelpJSON requests the structured JSON command inventory instead of command
	// execution. It is mode- and path-independent: the whole command tree is
	// always emitted regardless of any trailing command path or output mode.
	HelpJSON bool
	// Version requests version output instead of command execution.
	Version bool
}

// Config describes the generated root-help shell around a consumer command
// tree. It keeps program-level usage, global flags, mode descriptions, and final
// guidance in one data structure so root help and command-topic help stay in
// sync with dispatch.
type Config struct {
	// Program is the executable name shown in generated usage.
	Program string
	// Version is the optional program version rendered as the first root-help
	// line. Empty preserves the pre-version help shape.
	Version string
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

// Handler executes a matched command with the arguments left after command-tree
// navigation. It receives only the command-specific remainder; global flags are
// parsed by ParseGlobalConfig before Dispatch.
type Handler func(context.Context, []string) error

// CommandSpec is a node in the shared command tree. A root node holds Config
// plus first-level Subcommands, while leaf nodes usually provide a Handler and
// optional Flags. Dispatch, generated usage, and generated help all read this
// same model so CLIs do not maintain separate command registries.
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
	// Group is an optional generated-help grouping label.
	Group string
	// Flags are command-specific flags rendered in command help.
	Flags []FlagSpec
	// ExitCodes are optional process exit-code rows rendered in generated help
	// and structured help JSON for commands with a declared taxonomy.
	ExitCodes []ExitCodeSpec
	// Positionals describes the accepted positional operand arity.
	Positionals []PositionalSpec
	// Subcommands are this command's child command specs.
	Subcommands []*CommandSpec
	// Handler executes this command when dispatched.
	Handler Handler
	// Config is the root help and usage configuration inherited by children.
	Config Config
}

// FlagSpec describes one command or global flag row in generated help. Name is
// stored without leading dashes, Value is an optional placeholder, Default is
// appended to the description when present, and Short is the operator-facing
// explanation.
type FlagSpec struct {
	// Name is the flag name without leading dashes.
	Name string
	// Value is the optional value placeholder shown after the flag name.
	Value string
	// Default is the optional default value shown in help.
	Default string
	// Short is the flag description.
	Short string
	// Alias is an optional single-letter short flag without a leading dash.
	Alias string
	// Enum limits value flags to the listed values.
	Enum []string
	// Required marks a value flag as mandatory.
	Required bool
	// Repeatable allows a string-list flag to be supplied more than once.
	Repeatable bool
	// StringTarget receives a parsed string flag value.
	StringTarget *string
	// BoolTarget receives a parsed bool flag value.
	BoolTarget *bool
	// StringSliceTarget receives parsed repeatable string flag values.
	StringSliceTarget *[]string
}

// ExitCodeSpec describes one generated-help row in a command's public exit-code
// taxonomy.
type ExitCodeSpec struct {
	// Code is the process exit status.
	Code int `json:"code"`
	// Meaning is the operator-facing description of the status.
	Meaning string `json:"meaning"`
}

// PositionalSpec describes a named positional operand group. Min and Max define
// the accepted arity; Max < 0 means unbounded.
type PositionalSpec struct {
	// Name identifies the operand in usage diagnostics.
	Name string
	// Min is the minimum accepted count for this operand group.
	Min int
	// Max is the maximum accepted count for this operand group; Max < 0 is
	// unbounded.
	Max int
}

// UsageError reports invalid CLI usage. Consumers should present its diagnostic
// to the operator and map it to exit code 2, keeping usage failures distinct
// from runtime command failures.
type UsageError struct {
	// Err is the underlying diagnostic presented to the user.
	Err error
}

// NewUsageError returns a UsageError with a formatted message suitable for
// direct display in CLI diagnostics.
func NewUsageError(format string, args ...any) UsageError {
	return UsageError{Err: fmt.Errorf(format, args...)}
}

// Error returns the usage diagnostic text, or an empty string for a zero-value
// UsageError.
func (e UsageError) Error() string {
	if e.Err == nil {
		return ""
	}
	return e.Err.Error()
}

// Unwrap returns the underlying usage diagnostic so callers can use errors.Is
// and errors.As with wrapped usage failures.
func (e UsageError) Unwrap() error { return e.Err }

// ExitCode returns the process exit code for usage errors.
func (e UsageError) ExitCode() int { return 2 }

// ParseGlobalConfig parses shared position-independent global flags and returns
// the non-global command words. It accepts --mode, -v/--verbose, --no-header,
// -h/--help, --help-all, --help-json, and --version wherever they appear in
// argv, leaving command and positional words in their original relative order.
//
// DHF-REQ: keel/requirement-57
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
		case "--help-all":
			cfg.HelpAll = true
		case "--help-json":
			// DHF-REQ: keel/requirement-100
			cfg.HelpJSON = true
		case "--version":
			cfg.Version = true
		default:
			words = append(words, arg)
		}
	}
	return cfg, words, nil
}

// ParseMode parses a shared console mode string case-insensitively and rejects
// unknown values with UsageError.
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

// Usage returns the generated usage line for this command at path. Explicit Use
// text wins; otherwise the root Config or the command's Args/Subcommands fill in
// the command-specific suffix.
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
// whether the whole path matched. It is useful for help-topic routing where a
// caller needs both the resolved command and the unresolved suffix.
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

// Child returns the named direct subcommand without walking deeper into the
// tree.
func (c *CommandSpec) Child(name string) (*CommandSpec, bool) {
	for _, child := range c.Subcommands {
		if child.Name == name {
			return child, true
		}
	}
	return nil, false
}

// Dispatch invokes the deepest matching command handler. It inherits root
// Config into child nodes, rejects empty or unknown command paths with
// UsageError, parses command-declared typed flags, validates positional arity,
// and passes the remaining positional arguments to the resolved Handler.
//
// DHF-REQ: keel/requirement-104
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
	handlerArgs, err := node.parseCommandArgs(matched, remaining)
	if err != nil {
		return err
	}
	return node.Handler(ctx, handlerArgs)
}

func (c *CommandSpec) parseCommandArgs(matched, remaining []string) ([]string, error) {
	c.applyFlagDefaults()
	seen := map[string]bool{}
	var handlerArgs []string
	stopOptions := false
	for i := 0; i < len(remaining); i++ {
		arg := remaining[i]
		if stopOptions || len(arg) < 2 || arg[0] != '-' || arg == "-" {
			handlerArgs = append(handlerArgs, arg)
			continue
		}
		if arg == "--" {
			stopOptions = true
			continue
		}
		if strings.HasPrefix(arg, "--") {
			consumed, passthrough, err := c.parseLongFlag(arg, remaining, &i, seen)
			if err != nil {
				return nil, err
			}
			if !consumed {
				handlerArgs = append(handlerArgs, passthrough...)
			}
			continue
		}
		consumed, passthrough, err := c.parseShortFlags(arg, remaining, &i, seen)
		if err != nil {
			return nil, err
		}
		if !consumed {
			handlerArgs = append(handlerArgs, passthrough...)
		}
	}
	if err := c.checkRequiredFlags(seen, matched); err != nil {
		return nil, err
	}
	if err := c.validatePositionals(matched, handlerArgs); err != nil {
		return nil, err
	}
	return handlerArgs, nil
}

func (c *CommandSpec) applyFlagDefaults() {
	for _, f := range c.Flags {
		switch {
		case f.StringTarget != nil:
			*f.StringTarget = f.Default
		case f.BoolTarget != nil:
			*f.BoolTarget = f.Default == "true"
		case f.StringSliceTarget != nil:
			*f.StringSliceTarget = nil
			if f.Default != "" {
				*f.StringSliceTarget = append(*f.StringSliceTarget, f.Default)
			}
		}
	}
}

func (c *CommandSpec) parseLongFlag(arg string, remaining []string, index *int, seen map[string]bool) (bool, []string, error) {
	body := strings.TrimPrefix(arg, "--")
	name, value, hasValue := body, "", false
	if eq := strings.IndexByte(body, '='); eq >= 0 {
		name, value, hasValue = body[:eq], body[eq+1:], true
	}
	flag, ok := c.flagByName(name)
	if !ok {
		return false, nil, UsageError{Err: fmt.Errorf("unknown flag %q", arg)}
	}
	if !flag.hasTarget() {
		return false, []string{arg}, nil
	}
	if flag.BoolTarget != nil {
		if hasValue {
			parsed, err := parseBoolFlagValue(value)
			if err != nil {
				return false, nil, err
			}
			*flag.BoolTarget = parsed
		} else {
			*flag.BoolTarget = true
		}
		seen[flag.Name] = true
		return true, nil, nil
	}
	if !hasValue {
		if *index+1 >= len(remaining) {
			return false, nil, UsageError{Err: fmt.Errorf("--%s requires a value", name)}
		}
		*index++
		value = remaining[*index]
	}
	if err := flag.setValue(value); err != nil {
		return false, nil, err
	}
	seen[flag.Name] = true
	return true, nil, nil
}

func (c *CommandSpec) parseShortFlags(arg string, remaining []string, index *int, seen map[string]bool) (bool, []string, error) {
	body := strings.TrimPrefix(arg, "-")
	if body == "" {
		return false, []string{arg}, nil
	}
	for pos := 0; pos < len(body); pos++ {
		alias := body[pos : pos+1]
		flag, ok := c.flagByAlias(alias)
		if !ok {
			return false, nil, UsageError{Err: fmt.Errorf("unknown flag %q", "-"+alias)}
		}
		if !flag.hasTarget() {
			return false, []string{arg}, nil
		}
		if flag.BoolTarget != nil {
			*flag.BoolTarget = true
			seen[flag.Name] = true
			continue
		}
		if len(body) > 1 {
			return false, nil, UsageError{Err: fmt.Errorf("flag -%s requires a separate value", alias)}
		}
		if *index+1 >= len(remaining) {
			return false, nil, UsageError{Err: fmt.Errorf("-%s requires a value", alias)}
		}
		*index++
		if err := flag.setValue(remaining[*index]); err != nil {
			return false, nil, err
		}
		seen[flag.Name] = true
	}
	return true, nil, nil
}

func (c *CommandSpec) checkRequiredFlags(seen map[string]bool, matched []string) error {
	for _, f := range c.Flags {
		if f.Required && !seen[f.Name] {
			return UsageError{Err: fmt.Errorf("required flag --%s missing\n%s", f.Name, c.Usage(matched))}
		}
	}
	return nil
}

func (c *CommandSpec) validatePositionals(matched, args []string) error {
	if len(c.Positionals) == 0 {
		return nil
	}
	min, max := 0, 0
	unbounded := false
	for _, spec := range c.Positionals {
		min += spec.Min
		if spec.Max < 0 {
			unbounded = true
			continue
		}
		max += spec.Max
	}
	if len(args) < min || (!unbounded && len(args) > max) {
		return UsageError{Err: fmt.Errorf("invalid positional arity: got %d, want %s\n%s", len(args), c.positionalArityText(), c.Usage(matched))}
	}
	return nil
}

func (c *CommandSpec) positionalArityText() string {
	parts := make([]string, 0, len(c.Positionals))
	for _, spec := range c.Positionals {
		name := spec.Name
		if name == "" {
			name = "arg"
		}
		switch {
		case spec.Min == spec.Max:
			parts = append(parts, fmt.Sprintf("%d %s", spec.Min, name))
		case spec.Max < 0:
			parts = append(parts, fmt.Sprintf("at least %d %s", spec.Min, name))
		default:
			parts = append(parts, fmt.Sprintf("%d-%d %s", spec.Min, spec.Max, name))
		}
	}
	return strings.Join(parts, ", ")
}

func (c *CommandSpec) flagByName(name string) (FlagSpec, bool) {
	for _, f := range c.Flags {
		if f.Name == name {
			return f, true
		}
	}
	return FlagSpec{}, false
}

func (c *CommandSpec) flagByAlias(alias string) (FlagSpec, bool) {
	for _, f := range c.Flags {
		if f.Alias == alias {
			return f, true
		}
	}
	return FlagSpec{}, false
}

func (f FlagSpec) hasTarget() bool {
	return f.StringTarget != nil || f.BoolTarget != nil || f.StringSliceTarget != nil
}

func (f FlagSpec) setValue(value string) error {
	if len(f.Enum) > 0 {
		ok := false
		for _, allowed := range f.Enum {
			if value == allowed {
				ok = true
				break
			}
		}
		if !ok {
			return UsageError{Err: fmt.Errorf("invalid --%s %q: expected one of: %s", f.Name, value, strings.Join(f.Enum, ", "))}
		}
	}
	switch {
	case f.StringTarget != nil:
		*f.StringTarget = value
	case f.StringSliceTarget != nil:
		if !f.Repeatable && len(*f.StringSliceTarget) > 0 {
			*f.StringSliceTarget = nil
		}
		*f.StringSliceTarget = append(*f.StringSliceTarget, value)
	default:
		return UsageError{Err: fmt.Errorf("--%s does not accept a value", f.Name)}
	}
	return nil
}

func parseBoolFlagValue(value string) (bool, error) {
	switch value {
	case "true", "1", "yes", "on":
		return true, nil
	case "false", "0", "no", "off":
		return false, nil
	default:
		return false, UsageError{Err: fmt.Errorf("invalid bool flag value %q", value)}
	}
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

// ValidateTree checks keel's first-party command-tree invariants: command paths
// are at most two tokens below the program, non-root namespace nodes have at
// least two children, nodes do not mix a handler with children, and command
// flags do not collide with keel-owned global flag names or aliases.
//
// DHF-REQ: keel/requirement-106, keel/requirement-104
func (c *CommandSpec) ValidateTree() error {
	return c.validateTree(nil, true)
}

func (c *CommandSpec) validateTree(path []string, root bool) error {
	if !root && len(path) > 2 {
		return fmt.Errorf("command path %q exceeds maximum depth 2", strings.Join(path, " "))
	}
	if c.Handler != nil && len(c.Subcommands) > 0 {
		return fmt.Errorf("command %q mixes a handler with child commands", commandPath(path, c.Name))
	}
	if !root && len(c.Subcommands) == 1 {
		return fmt.Errorf("namespace %q has fewer than two children", strings.Join(path, " "))
	}
	if !root && c.Handler == nil && len(c.Subcommands) == 0 {
		return fmt.Errorf("command %q is neither a namespace nor a leaf with a handler", strings.Join(path, " "))
	}
	if err := c.validateFlagCollisions(path); err != nil {
		return err
	}
	for _, child := range c.Subcommands {
		if child == nil {
			return fmt.Errorf("command %q declares a nil child command", commandPath(path, c.Name))
		}
		childPath := append(append([]string{}, path...), child.Name)
		if err := child.validateTree(childPath, false); err != nil {
			return err
		}
	}
	return nil
}

func (c *CommandSpec) validateFlagCollisions(path []string) error {
	long, short := globalFlagNames()
	for _, flag := range c.Flags {
		if long[flag.Name] || short[flag.Name] {
			return fmt.Errorf("command %q declares global flag collision %q", strings.Join(path, " "), flag.Name)
		}
		if flag.Alias != "" && (long[flag.Alias] || short[flag.Alias]) {
			return fmt.Errorf("command %q declares global flag alias collision %q", strings.Join(path, " "), flag.Alias)
		}
	}
	return nil
}

func globalFlagNames() (map[string]bool, map[string]bool) {
	long := map[string]bool{}
	for _, flag := range GlobalFlagSpecs() {
		long[flag.Name] = true
	}
	return long, map[string]bool{"v": true, "h": true}
}

func commandPath(path []string, fallback string) string {
	if len(path) > 0 {
		return strings.Join(path, " ")
	}
	return fallback
}

// GlobalFlagSpecs returns the canonical help rows for the shared global flags
// ParseGlobalConfig parses. keel owns these names and descriptions so root help
// cannot drift from the parser; a consumer's Config.GlobalFlags contributes only
// its own additional binary-specific globals. The order mirrors ParseGlobalConfig.
//
// DHF-REQ: keel/requirement-101
func GlobalFlagSpecs() []FlagSpec {
	return []FlagSpec{
		{Name: "mode", Value: "human|ai|json", Default: "human", Short: "Select the console output protocol."},
		{Name: "verbose", Short: "Include debug-level console detail."},
		{Name: "no-header", Short: "Suppress the run header for machine protocol consumers."},
		{Name: "help", Short: "Print generated help and exit."},
		{Name: "help-all", Short: "Print root help plus every command topic and exit."},
		{Name: "help-json", Short: "Print the full command tree as a JSON inventory and exit."},
		{Name: "version", Short: "Print version and exit."},
	}
}

// ModeHelpLines returns the canonical --mode ai|json output-mode description keel
// owns. RenderRootHelp emits these lines so the mode protocol is documented once
// from keel; a consumer's Config.ModeHelp contributes only additional
// binary-specific lines (for example, where it writes structured logs).
//
// DHF-REQ: keel/requirement-101
func ModeHelpLines() []string {
	return []string{
		"human renders plain console output.",
		"ai emits sparse AI-readable records.",
		"json emits full JSON log records.",
	}
}

// mergeGlobalFlags returns keel's canonical global flag rows followed by any
// consumer-supplied globals that are not keel-owned. A consumer entry re-listing
// a keel-owned flag name is de-duped (rendered once, from keel) for back-compat.
//
// DHF-REQ: keel/requirement-101
func mergeGlobalFlags(extra []FlagSpec) []FlagSpec {
	canonical := GlobalFlagSpecs()
	owned := make(map[string]bool, len(canonical))
	for _, f := range canonical {
		owned[f.Name] = true
	}
	merged := append([]FlagSpec{}, canonical...)
	for _, f := range extra {
		if owned[f.Name] {
			continue
		}
		merged = append(merged, f)
	}
	return merged
}

// mergeModeHelp returns keel's canonical output-mode lines followed by any
// consumer-supplied lines not already present, so the mode protocol is described
// once from keel and legacy consumer duplicates do not double-render.
//
// DHF-REQ: keel/requirement-101
func mergeModeHelp(extra []string) []string {
	canonical := ModeHelpLines()
	seen := make(map[string]bool, len(canonical))
	for _, line := range canonical {
		seen[line] = true
	}
	merged := append([]string{}, canonical...)
	for _, line := range extra {
		if seen[line] {
			continue
		}
		seen[line] = true
		merged = append(merged, line)
	}
	return merged
}

// renderHelpHeader writes the header every help page shares: the
// "<program> v<version>" identity line from Config.Version, then what was asked
// for — a title line with its indented summary on a command topic, or the root
// summary paragraph at root — then the Usage: block. Both renderers call it, so
// the identity line and the header ordering have one source and root help reads
// the same way as every topic below it. The identity line is omitted whenever
// Config.Version is empty.
//
// DHF-REQ: keel/requirement-111, keel/requirement-149
func (c *CommandSpec) renderHelpHeader(w io.Writer, title, summary string, usage []string) {
	wrote := false
	if c.Config.Version != "" {
		fmt.Fprintf(w, "%s v%s\n", c.program(), c.Config.Version)
		wrote = true
	}
	if title != "" {
		if wrote {
			fmt.Fprintln(w)
		}
		fmt.Fprintln(w, title)
		wrote = true
	}
	if summary != "" {
		if title != "" {
			fmt.Fprintf(w, "  %s\n", summary)
		} else {
			if wrote {
				fmt.Fprintln(w)
			}
			fmt.Fprintln(w, summary)
		}
		wrote = true
	}
	if wrote {
		fmt.Fprintln(w)
	}
	fmt.Fprintln(w, "Usage:")
	for _, line := range usage {
		if line != "" {
			fmt.Fprintf(w, "  %s\n", line)
		}
	}
}

// helpTitle names the command topic a help page was opened for.
func (c *CommandSpec) helpTitle(path []string) string {
	return strings.Join(path, " ") + " commands:"
}

// RenderRootHelp writes generated root help from Config, global flags,
// first-level command summaries, output-mode prose, and trailing guidance. The
// global flag rows and the --mode ai|json output-mode description are keel-owned
// (GlobalFlagSpecs, ModeHelpLines); Config.GlobalFlags and Config.ModeHelp are
// additive — consumer-only extras rendered after keel's canonical entries, with
// any keel-owned re-declarations de-duped.
//
// DHF-REQ: keel/requirement-101, keel/requirement-111
func (c *CommandSpec) RenderRootHelp(w io.Writer) {
	c.InheritConfig()
	c.renderHelpHeader(w, "", c.Config.RootSummary, []string{c.Config.Usage, c.Config.HelpUsage, c.Config.CommandUsage})
	if globals := mergeGlobalFlags(c.Config.GlobalFlags); len(globals) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Global flags:")
		PrintFlagRows(w, globals)
	}
	if len(c.Subcommands) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Commands:")
		PrintGroupedCommandRows(w, c.Subcommands)
	}
	if modeHelp := mergeModeHelp(c.Config.ModeHelp); len(modeHelp) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Output mode:")
		for _, line := range modeHelp {
			fmt.Fprintf(w, "  %s\n", line)
		}
	}
	if c.Config.Trailing != "" {
		fmt.Fprintln(w)
		fmt.Fprintln(w, c.Config.Trailing)
	}
}

// RenderTopicHelp writes help for the command path or root help when path is
// empty. Unknown topics render a diagnostic followed by root help.
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

// RenderAllHelp writes generated root help followed by command-topic help for
// every command in the tree exactly once, depth-first in declaration order.
//
// DHF-REQ: keel/requirement-57
func (c *CommandSpec) RenderAllHelp(w io.Writer) {
	c.InheritConfig()
	c.RenderRootHelp(w)
	for i, child := range c.Subcommands {
		fmt.Fprintln(w)
		if i > 0 {
			fmt.Fprintln(w)
		}
		child.renderAllCommandHelp(w, []string{child.Name})
	}
}

func (c *CommandSpec) renderAllCommandHelp(w io.Writer, path []string) {
	c.RenderCommandHelp(w, path)
	for _, child := range c.Subcommands {
		fmt.Fprintln(w)
		fmt.Fprintln(w)
		childPath := append(append([]string{}, path...), child.Name)
		child.renderAllCommandHelp(w, childPath)
	}
}

// helpJSONFlag is the JSON element shape for one flag row in the structured
// help inventory.
type helpJSONFlag struct {
	Name        string `json:"name"`
	Value       string `json:"value"`
	Default     string `json:"default"`
	Description string `json:"description"`
}

// helpJSONCommand is the JSON element shape for one command in the structured
// help inventory. Flags is always a (possibly empty) array, never null.
type helpJSONCommand struct {
	Path      string         `json:"path"`
	Group     string         `json:"group"`
	Summary   string         `json:"summary"`
	Usage     string         `json:"usage"`
	Flags     []helpJSONFlag `json:"flags"`
	ExitCodes []ExitCodeSpec `json:"exit_codes,omitempty"`
}

// RenderHelpJSON writes a single JSON array describing every command in the
// tree exactly once, depth-first in declaration order. The root node is the
// program shell, not a command, so it is not emitted; each element carries a
// non-empty space-joined command path plus that command's summary, usage, and
// flags. The CommandSpec tree is the single source — this is a pure traversal
// and marshal with no second help model. Output is deterministic across calls.
//
// DHF-REQ: keel/requirement-100
func (c *CommandSpec) RenderHelpJSON(w io.Writer) error {
	c.InheritConfig()
	commands := []helpJSONCommand{}
	for _, child := range c.Subcommands {
		child.appendHelpJSON(&commands, []string{child.Name})
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(commands)
}

func (c *CommandSpec) appendHelpJSON(out *[]helpJSONCommand, path []string) {
	flags := make([]helpJSONFlag, 0, len(c.Flags))
	for _, f := range c.Flags {
		flags = append(flags, helpJSONFlag{
			Name:        f.Name,
			Value:       f.Value,
			Default:     f.Default,
			Description: f.Short,
		})
	}
	*out = append(*out, helpJSONCommand{
		Path:      strings.Join(path, " "),
		Group:     commandGroup(c),
		Summary:   c.Short,
		Usage:     strings.TrimPrefix(c.Usage(path), "usage: "),
		Flags:     flags,
		ExitCodes: append([]ExitCodeSpec{}, c.ExitCodes...),
	})
	for _, child := range c.Subcommands {
		childPath := append(append([]string{}, path...), child.Name)
		child.appendHelpJSON(out, childPath)
	}
}

// RenderCommandHelp writes command help for one command node, including its
// summary, usage, declared flags, and nested subcommands.
func (c *CommandSpec) RenderCommandHelp(w io.Writer, path []string) {
	summary := c.Long
	if summary == "" {
		summary = c.Short
	}
	c.renderHelpHeader(w, c.helpTitle(path), summary, []string{strings.TrimPrefix(c.Usage(path), "usage: ")})
	if len(c.Flags) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Flags:")
		PrintFlagRows(w, c.Flags)
	}
	if len(c.ExitCodes) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Exit codes:")
		PrintExitCodeRows(w, c.ExitCodes)
	}
	if len(c.Subcommands) == 0 {
		return
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Subcommands:")
	PrintGroupedCommandRows(w, c.Subcommands)
}

// RenderSubcommandHelp writes a nested subcommand listing below parent. Rows
// are sorted by command name at each level for stable generated help.
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

// PrintCommandRows writes aligned command summary rows in the order supplied by
// the caller.
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

// PrintGroupedCommandRows writes command summary rows under group headings.
// Group order and command order within each group follow declaration order.
//
// DHF-REQ: keel/requirement-105
func PrintGroupedCommandRows(w io.Writer, commands []*CommandSpec) {
	groups := groupCommands(commands)
	width := commandNameWidth(commands)
	for _, group := range groups {
		fmt.Fprintf(w, "%s:\n", group.name)
		printIndentedCommandRows(w, group.commands, 2, width)
	}
}

func printIndentedCommandRows(w io.Writer, commands []*CommandSpec, indent, width int) {
	if width == 0 {
		width = commandNameWidth(commands)
	}
	prefix := strings.Repeat(" ", indent)
	for _, cmd := range commands {
		fmt.Fprintf(w, "%s%-*s  %s\n", prefix, width, cmd.Name, cmd.Short)
	}
}

func commandNameWidth(commands []*CommandSpec) int {
	width := 0
	for _, cmd := range commands {
		if len(cmd.Name) > width {
			width = len(cmd.Name)
		}
	}
	return width
}

type commandGroupRows struct {
	name     string
	commands []*CommandSpec
}

func groupCommands(commands []*CommandSpec) []commandGroupRows {
	var groups []commandGroupRows
	index := map[string]int{}
	for _, cmd := range commands {
		name := commandGroup(cmd)
		at, ok := index[name]
		if !ok {
			index[name] = len(groups)
			groups = append(groups, commandGroupRows{name: name})
			at = len(groups) - 1
		}
		groups[at].commands = append(groups[at].commands, cmd)
	}
	return groups
}

func commandGroup(cmd *CommandSpec) string {
	if cmd.Group != "" {
		return cmd.Group
	}
	return "Other"
}

// PrintFlagRows writes flag help rows using the package's shared two-line flag
// layout.
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

// PrintExitCodeRows writes aligned exit-code taxonomy rows in declaration order.
func PrintExitCodeRows(w io.Writer, codes []ExitCodeSpec) {
	width := 0
	for _, row := range codes {
		if n := len(fmt.Sprint(row.Code)); n > width {
			width = n
		}
	}
	for _, row := range codes {
		fmt.Fprintf(w, "  %-*d  %s\n", width, row.Code, row.Meaning)
	}
}

// SubcommandAlternates returns a pipe-separated command-name list in command
// tree order for generated usage strings.
func SubcommandAlternates(commands []*CommandSpec) string {
	names := make([]string, 0, len(commands))
	for _, child := range commands {
		names = append(names, child.Name)
	}
	return strings.Join(names, "|")
}

// SimpleSpecs builds sorted leaf command specs from descriptions, using prefix
// plus each map key as the Use text. It is a convenience for static topic lists
// whose commands only need generated help.
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

// InheritConfig fills missing child Config values from the root configuration so
// child usage and help render with the same program name and root shell.
func (c *CommandSpec) InheritConfig() {
	c.inheritConfig(c.Config)
}

func (c *CommandSpec) inheritConfig(cfg Config) {
	if c.Config.Program == "" {
		c.Config.Program = cfg.Program
	}
	// Version travels the same path as Program so every command topic can
	// render the identity line requirement-111 puts on every help page.
	if c.Config.Version == "" {
		c.Config.Version = cfg.Version
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
