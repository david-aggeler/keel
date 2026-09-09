// This file exists ONLY to keep .claude/ out of keel's published Go module zip.
// It declares no packages and is never built, fetched, or imported.
//
// Why: the module zip contains every git-tracked file at a tag, dot-directories
// included. keel is a public module, so anything tracked under .claude/ is
// published to proxy.golang.org and cached there permanently -- module@version
// is immutable, and content cannot be withdrawn once fetched. Between v0.1.x and
// v0.9.2 that shipped OpenBrain's catalog-materialized skills and agents in 31
// tags (keel/issue-235, keel/issue-236).
//
// Gitignoring those trees stopped the bleeding, but it relies on the ignore list
// staying correct: the `decide` skill was adopted into gold on 2026-09-09 and
// stayed tracked until someone noticed. golang.org/x/mod/zip excludes any
// directory holding its own go.mod, so this file makes the exclusion structural
// instead of a matter of vigilance -- nothing under .claude/ can reach the proxy
// again, whatever the ignore rules say.
//
// The module path is deliberately unresolvable. Do not add packages here, do not
// add it to go.work, and do not delete it without reading keel/issue-235 and
// keel/issue-236.
module keel.local/claude-config

go 1.26.6
