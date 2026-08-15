# Keel Test Bridge Supported VS Code

DHF-REQ: keel/requirement-119

Minimum supported VS Code: ^1.125.0
VS Code runtime Node major: 24
VS Code runtime Node major source: VS Code 1.125.0 ships Electron 42.3.0 with Node.js 24.15.0 — https://github.com/ewanharris/vscode-versions, corroborated against https://code.visualstudio.com/updates/v1_125

Reason: the owner decision on 2026-08-14 raised the floor to `^1.125.0` so
the Test Bridge can keep its VSIX toolchain current without compiling against
APIs older declared engines cannot provide. Users on VS Code versions before
1.125 can no longer install this extension.

The runtime Node major is derived from the floor above, never from what a
package wants: the floor names a VS Code release, that release ships an
Electron build, and that Electron build ships a Node runtime. The source line
carries that derivation so it can be re-checked without reading
`vsix/package.json`. `keel/issue-147` records what happens without it — the
value was moved 22 → 26 to clear the `@types/node` ceiling it exists to impose.

Dependency hold notes:

- `@types/node` is held at `24.13.3` (current: `26.2.0`).
  Reason: it must not describe a Node runtime above the one the declared VS Code
  floor ships, which is Node 24 per the source line above; `24.13.3` is the
  highest published release of that line.
  Release condition: the declared VS Code floor moves to a release whose bundled
  Electron ships Node 26 or later, evidenced by an updated source line.
