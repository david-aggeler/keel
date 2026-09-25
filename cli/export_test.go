package cli

import "io"

// Test-only access to the help renderer. None of these identifiers is part of
// keel/cli's API: they compile only into this package's tests, so the tests
// can pin rendered help into a buffer while consumers cannot choose a help
// writer (keel/requirement-172).

// RenderRootHelp writes root help to w.
func (c *CommandSpec) RenderRootHelp(w io.Writer) { c.writeRootHelp(w) }

// RenderTopicHelp writes help for path to w.
func (c *CommandSpec) RenderTopicHelp(w io.Writer, path []string) error {
	return c.writeTopicHelp(w, path)
}

// RenderAllHelp writes the --help-all dump to w.
func (c *CommandSpec) RenderAllHelp(w io.Writer) { c.writeAllHelp(w) }

// RenderHelpJSON writes the --help-json inventory to w.
func (c *CommandSpec) RenderHelpJSON(w io.Writer) error { return c.writeHelpJSON(w) }

// RenderCommandHelp writes command help for the node at path to w.
func (c *CommandSpec) RenderCommandHelp(w io.Writer, path []string) {
	c.writeCommandHelp(w, path)
}

// SetHelpWidth wraps every help page in the tree to width columns.
func (c *CommandSpec) SetHelpWidth(width int) { c.setHelpWidth(width) }

// SetHelpOutput points the help renderer's stdout and stderr at the given
// writers in place of the process streams.
func (c *CommandSpec) SetHelpOutput(stdout, stderr io.Writer) {
	c.Config.helpStdout, c.Config.helpStderr = stdout, stderr
}

// ParseGlobalConfig parses the shared global flags without a command tree.
func ParseGlobalConfig(argv []string) (RuntimeConfig, []string, error) {
	return parseGlobalConfig(argv, nil, consumerGlobalLookup{})
}

// ParseGlobalConfig parses the shared global flags against the tree, as
// Start does before it serves help.
func (c *CommandSpec) ParseGlobalConfig(argv []string) (RuntimeConfig, []string, error) {
	return c.parseGlobals(argv)
}

// HelpRequested reports the help requests a parse recorded.
func (c RuntimeConfig) HelpRequested() (help, helpAll, helpJSON, version bool) {
	return c.help, c.helpAll, c.helpJSON, c.version
}
