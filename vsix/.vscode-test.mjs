import { defineConfig } from '@vscode/test-cli';
import { mkdirSync } from 'node:fs';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const extensionRoot = dirname(fileURLToPath(import.meta.url));
const repoRoot = resolve(extensionRoot, '../..');
const hostWorkspace = '/tmp/keel-vscode-extension-host';
mkdirSync(hostWorkspace, { recursive: true });

export default defineConfig({
  files: 'out/test/**/*.test.js',
  version: 'stable',
  extensionDevelopmentPath: extensionRoot,
  workspaceFolder: hostWorkspace,
  launchArgs: ['--disable-extensions'],
  env: {
    KEEL_VSCODE_BRIDGE_DEV_WORKSPACE: repoRoot
  }
});
