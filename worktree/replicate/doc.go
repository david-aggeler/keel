// Package replicate resolves the keel.worktree.replicate configuration subtree
// into neutral worktree replication items.
//
// Hosts own file loading and YAML decoding. This package intentionally exposes
// yaml-tagged structs but imports no YAML implementation; callers decode with
// their chosen yaml.v3-compatible decoder and pass the resulting Config to
// [Config.Resolve].
//
// DHF-REQ: keel/requirement-157
package replicate
