# Keel Test Bridge Supported VS Code

DHF-REQ: keel/requirement-119

Minimum supported VS Code: ^1.125.0
VS Code runtime Node major: 26

Reason: the owner decision on 2026-08-14 raised the floor to `^1.125.0` so
the Test Bridge can keep its VSIX toolchain current without compiling against
APIs older declared engines cannot provide. Users on VS Code versions before
1.125 can no longer install this extension.

Dependency hold notes:

No `vsix/` dependency is intentionally held below its current release as of
`keel/change_request-180`. If a future dependency must be held back, state the
reason and release condition at its declaration site or in this note.
