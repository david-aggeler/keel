# Keel Test Bridge Supported VS Code

DHF-REQ: keel/requirement-119

Minimum supported VS Code: ^1.125.0
VS Code runtime Node major: 22

Reason: the owner decision on 2026-08-14 raised the floor to `^1.125.0` so
the Test Bridge can keep its VSIX toolchain current without compiling against
APIs older declared engines cannot provide. Users on VS Code versions before
1.125 can no longer install this extension.

Dependency hold notes:

- `@types/vscode` remains at `1.102.0` until `keel/change_request-180` raises
  it to match this declared floor. It is valid only because it describes an API
  surface older than the declared minimum, not a newer one.
- `@types/node` remains on the 22 line until `keel/change_request-180` completes
  the toolchain update. It must not move above the Node major shipped by the VS
  Code release named above.
- `typescript` remains on the 5 line until those type packages move together in
  `keel/change_request-180`; TypeScript 7 cannot compile this workspace against
  the old Node type line.
