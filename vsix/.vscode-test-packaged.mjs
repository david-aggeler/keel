import { defineConfig } from '@vscode/test-cli';
import { mkdirSync, readFileSync, rmSync } from 'node:fs';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const extensionRoot = dirname(fileURLToPath(import.meta.url));
const repoRoot = resolve(extensionRoot, '..');
const pkg = JSON.parse(readFileSync(resolve(extensionRoot, 'package.json'), 'utf8'));
const hostWorkspace = '/tmp/keel-vscode-packaged-e2e-workspace';
rmSync(hostWorkspace, { recursive: true, force: true });
mkdirSync(hostWorkspace, { recursive: true });

export default defineConfig({
  files: 'out/test/packaged-e2e/**/*.e2e.js',
  version: 'stable',
  extensionDevelopmentPath: resolve(extensionRoot, 'packaged-e2e-driver'),
  workspaceFolder: hostWorkspace,
  launchArgs: [],
  env: {
    KEEL_REPO_ROOT: repoRoot
  }
});
