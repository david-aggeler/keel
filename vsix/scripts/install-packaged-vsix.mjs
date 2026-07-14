import { runVSCodeCommand } from '@vscode/test-electron';
import { readFileSync } from 'node:fs';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const extensionRoot = dirname(dirname(fileURLToPath(import.meta.url)));
const repoRoot = resolve(extensionRoot, '..');
const pkg = JSON.parse(readFileSync(resolve(extensionRoot, 'package.json'), 'utf8'));
const vsix = resolve(repoRoot, 'bin', `keel-test-bridge-${pkg.version}.vsix`);

await runVSCodeCommand(['--install-extension', vsix, '--force'], { version: 'stable' });
