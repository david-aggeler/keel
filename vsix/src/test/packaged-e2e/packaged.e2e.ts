import * as assert from 'node:assert/strict';
import { createRequire } from 'node:module';
import * as fs from 'node:fs';
import * as path from 'node:path';
import * as vscode from 'vscode';

interface PublishedTreeSnapshot {
  discoveryItemsById: Map<string, { id: string; parent_id?: string; limitations?: string[] }>;
}

interface ExtensionAPI {
  currentTree(): PublishedTreeSnapshot | undefined;
  runProfileHandlerForTest(id: string): Promise<void>;
}

interface RunEvent {
  event: string;
  test_id?: string;
  message?: string;
  exit_code?: number;
}

const requireExtension = createRequire(__filename);

suite('packaged VSIX e2e lane', () => {
  // DHF-TEST: keel/requirement-76
  test('installed package drives the keel-demo-dev lifecycle through a real TestController', async function () {
    this.timeout(120_000);

    const workspaceRoot = vscode.workspace.workspaceFolders?.[0]?.uri.fsPath;
    assert.ok(workspaceRoot, 'packaged e2e workspace should be open');
    const repoRoot = process.env.KEEL_REPO_ROOT;
    assert.ok(repoRoot, 'KEEL_REPO_ROOT must point at the repository root');
    prepareWorkspace(workspaceRoot, repoRoot);

    const extension = vscode.extensions.getExtension('aggeler.keel-test-bridge');
    assert.ok(extension, 'installed VSIX should be discoverable');
    assert.equal(extension.packageJSON.name, 'keel-test-bridge');
    assert.equal(extension.packageJSON.version, JSON.parse(fs.readFileSync(path.join(repoRoot, 'vsix', 'package.json'), 'utf8')).version);
    const extensionPath = path.resolve(extension.extensionPath);
    assert.notEqual(extensionPath, path.resolve(repoRoot, 'vsix'), `extension loaded from source path: ${extension.extensionPath}`);
    assert.ok(extensionPath.includes(`${path.sep}.vscode-test${path.sep}extensions${path.sep}`), `extension was not loaded from the installed test profile: ${extension.extensionPath}`);

    await extension.activate();
    const api = requireExtension(path.join(extension.extensionPath, 'out', 'extension.js')) as ExtensionAPI;

    await vscode.commands.executeCommand('keel.tests.refresh');
    let tree = requireTree(api);
    assertMissing(tree, 'keel::desired-state::group::test-preconditions');
    assertMissing(tree, 'keel::desired-state::group::app-db-data-set');
    assertMissing(tree, 'keel-demo-dev::lane::fake-smoke');

    await runAndRead(workspaceRoot, api, 'keel::maintenance::detect-lanes');
    await vscode.commands.executeCommand('keel.tests.refresh');
    tree = requireTree(api);
    assertPresent(tree, 'keel-demo-dev::lane::go-pass');
    assertPresent(tree, 'keel-demo-dev::lane::go-fail');
    assertPresent(tree, 'keel-demo-dev::lane::fake-smoke');
    assert.equal(childrenOf(tree, 'keel::desired-state').length, 2);
    // cr-101 / requirement-88: a mutually-exclusive group with nothing selected
    // has the bridge-synthesized Unknown State member as its sole active row.
    assertOneActiveDataSet(tree, 'keel::desired-state::group::app-db-data-set::unknown');

    for (const id of [
      'keel-demo-dev::desired-state::docker-env',
      'keel-demo-dev::desired-state::postgres',
      'keel-demo-dev::desired-state::service-a',
      'keel-demo-dev::desired-state::service-b',
      'keel-demo-dev::desired-state::service-c',
      'keel-demo-dev::desired-state::sdk',
      'keel-demo-dev::desired-state::dns',
      'keel-demo-dev::desired-state::ping'
    ]) {
      assertRunEvent(await runAndRead(workspaceRoot, api, id), 'passed', id);
    }

    fs.rmSync(path.join(workspaceRoot, '.devtools', 'keel-demo-dev', 'ready', 'docker-env'), { force: true });
    assertRunEvent(await runAndRead(workspaceRoot, api, 'keel-demo-dev::desired-state::docker-env'), 'failed', 'keel-demo-dev::desired-state::docker-env');
    await runAndRead(workspaceRoot, api, 'keel::maintenance::detect-lanes');
    assertRunEvent(await runAndRead(workspaceRoot, api, 'keel-demo-dev::desired-state::docker-env'), 'passed', 'keel-demo-dev::desired-state::docker-env');

    await runAndRead(workspaceRoot, api, 'keel-demo-dev::desired-state::dataset::full');
    await vscode.commands.executeCommand('keel.tests.refresh');
    assertOneActiveDataSet(requireTree(api), 'keel-demo-dev::desired-state::dataset::full');

    await runAndRead(workspaceRoot, api, 'keel-demo-dev::desired-state::dataset::empty');
    await vscode.commands.executeCommand('keel.tests.refresh');
    assertOneActiveDataSet(requireTree(api), 'keel-demo-dev::desired-state::dataset::empty');
  });
});

function prepareWorkspace(workspaceRoot: string, repoRoot: string): void {
  fs.rmSync(path.join(workspaceRoot, '.vscode'), { recursive: true, force: true });
  fs.rmSync(path.join(workspaceRoot, '.devtools'), { recursive: true, force: true });
  fs.mkdirSync(path.join(workspaceRoot, '.vscode'), { recursive: true });
  fs.writeFileSync(path.join(workspaceRoot, '.vscode', 'test-bridge.json'), JSON.stringify({
    version: 3,
    command: path.join(repoRoot, 'bin', process.platform === 'win32' ? 'keel-demo-dev.exe' : 'keel-demo-dev'),
    args: [],
    displayName: 'Keel Demo Dev'
  }, null, 2) + '\n');
}

function requireTree(api: ExtensionAPI): PublishedTreeSnapshot {
  const tree = api.currentTree();
  assert.ok(tree, 'extension should publish a TestController tree');
  return tree;
}

function assertPresent(tree: PublishedTreeSnapshot, id: string): void {
  assert.ok(tree.discoveryItemsById.has(id), `expected ${id} to be present`);
}

function assertMissing(tree: PublishedTreeSnapshot, id: string): void {
  assert.ok(!tree.discoveryItemsById.has(id), `expected ${id} to be absent`);
}

function childrenOf(tree: PublishedTreeSnapshot, parentID: string): string[] {
  return [...tree.discoveryItemsById.values()]
    .filter((item) => item.parent_id === parentID)
    .map((item) => item.id);
}

function assertOneActiveDataSet(tree: PublishedTreeSnapshot, activeID: string): void {
  const rows = [
    'keel-demo-dev::desired-state::dataset::empty',
    'keel-demo-dev::desired-state::dataset::small',
    'keel-demo-dev::desired-state::dataset::full',
    // cr-101 / requirement-88: the bridge-synthesized Unknown State member is
    // the sole active row until a concrete dataset is selected, then it
    // deactivates. Including it here proves both halves of that toggle.
    'keel::desired-state::group::app-db-data-set::unknown'
  ];
  const active = rows.filter((id) => (tree.discoveryItemsById.get(id)?.limitations ?? []).includes('active=true'));
  assert.deepEqual(active, [activeID]);
}

async function runAndRead(workspaceRoot: string, api: ExtensionAPI, id: string): Promise<RunEvent[]> {
  const before = new Set(runStreams(workspaceRoot));
  await api.runProfileHandlerForTest(id);
  const after = runStreams(workspaceRoot).filter((candidate) => !before.has(candidate));
  assert.equal(after.length, 1, `expected exactly one run stream for ${id}, got ${after.length}`);
  return fs.readFileSync(after[0], 'utf8')
    .split(/\r?\n/)
    .filter((line) => line.trim().length > 0)
    .map((line) => JSON.parse(line) as RunEvent);
}

function runStreams(workspaceRoot: string): string[] {
  const runsDir = path.join(workspaceRoot, '.devtools', 'vscode-runs');
  if (!fs.existsSync(runsDir)) {
    return [];
  }
  return fs.readdirSync(runsDir)
    .filter((name) => name.endsWith('.jsonl'))
    .map((name) => path.join(runsDir, name))
    .sort();
}

function assertRunEvent(events: RunEvent[], event: string, testID: string): void {
  assert.ok(events.some((candidate) => candidate.event === event && candidate.test_id === testID), `missing ${event} for ${testID}: ${JSON.stringify(events)}`);
}
