# Keel Test Bridge Supported VS Code

DHF-REQ: keel/requirement-119

Minimum supported VS Code: ^1.125.0
VS Code runtime Node major: 22

Reason: the owner decision on 2026-08-14 raised the floor to `^1.125.0` so
the Test Bridge can keep its VSIX toolchain current without compiling against
APIs older declared engines cannot provide. Users on VS Code versions before
1.125 can no longer install this extension.

Dependency hold notes:

- `@types/vscode` is held at `1.102.0` (current: `1.125.0`).
  Reason: it must not describe APIs above the declared VS Code engine floor.
  Release condition: `keel/change_request-180` raises it to the declared floor.
- `@types/node` is held at `22.20.1` (current: `26.2.0`).
  Reason: it must not describe a Node runtime above the VS Code release named by
  the declared floor.
  Release condition: `keel/change_request-180` completes the coupled VSIX
  toolchain update.
- `typescript` is held at `5.9.3` (current: `7.0.2`).
  Reason: TypeScript 7 cannot compile this workspace against the old Node type
  line.
  Release condition: `keel/change_request-180` moves the type packages and
  TypeScript together.
