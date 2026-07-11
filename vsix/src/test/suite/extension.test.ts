import * as assert from 'node:assert/strict';
import * as fs from 'node:fs';
import * as os from 'node:os';
import * as path from 'node:path';
import * as vscode from 'vscode';
import {
  applyRunEvent,
  coverageFileSnapshotsForTest,
  currentAdapterConfig,
  ExternalRunMirror,
  parseGoCoverageProfile,
  setExternalRunStaleMsForTest,
  setCurrentTreeForTest,
  shouldInvalidateResultsForEvent
} from '../../extension';
import { configRelativePath, currentConfigVersion, defaultConfigTemplate, readAdapterConfig } from '../../bridgeAdapter';
import { publishDiscovery } from '../../tree';
import { RunEvent } from '../../protocol';

suite('Keel Test Bridge config contract', () => {
  // DHF-TEST: keel/requirement-40
  test('manifest exposes Keel identity, command ids, and config-file activation only', () => {
    const manifestPath = path.resolve(__dirname, '../../../package.json');
    const manifest = JSON.parse(fs.readFileSync(manifestPath, 'utf8')) as {
      publisher: string;
      name: string;
      displayName: string;
      license: string;
      activationEvents: string[];
      contributes?: { configuration?: unknown; commands?: Array<{ command: string }> };
    };

    assert.equal(manifest.publisher, 'aggeler');
    assert.equal(manifest.name, 'keel-test-bridge');
    assert.equal(manifest.displayName, 'Keel Test Bridge');
    assert.equal(manifest.license, 'Apache-2.0');
    assert.deepEqual(manifest.activationEvents, ['workspaceContains:.vscode/test-bridge.json']);
    assert.equal(manifest.contributes?.configuration, undefined);
    const commands = new Set(manifest.contributes?.commands?.map((command) => command.command));
    assert.ok(commands.has('keel.tests.refresh'));
    assert.ok(commands.has('keel.tests.initConfig'));
    assert.ok(!commands.has('vela.tests.refresh'));
  });

  // DHF-TEST: keel/requirement-40
  test('default template is the embedded current Keel config', () => {
    const parsed = JSON.parse(defaultConfigTemplate()) as { version: number; command: string; args: string[]; displayName: string };
    assert.equal(parsed.version, currentConfigVersion);
    assert.equal(parsed.command, 'bin/keel-dev');
    assert.deepEqual(parsed.args, ['vscode', 'tests']);
    assert.equal(parsed.displayName, 'Keel');
  });

  // DHF-TEST: keel/requirement-40
  test('file-backed config tolerantly reads newer versions and ignores unknown fields', () => {
    const root = fs.mkdtempSync(path.join(os.tmpdir(), 'keel-test-bridge-'));
    fs.mkdirSync(path.join(root, '.vscode'));
    fs.writeFileSync(
      path.join(root, configRelativePath),
      JSON.stringify({
        version: currentConfigVersion + 1,
        command: 'bin/future-dev',
        args: ['vscode', 'tests'],
        displayName: 'Future',
        extraFutureField: true
      })
    );

    const cfg = readAdapterConfig(root);
    assert.equal(cfg.version, currentConfigVersion + 1);
    assert.equal(cfg.command, 'bin/future-dev');
    assert.equal(cfg.displayName, 'Future');
  });

  // DHF-TEST: keel/requirement-40
  test('extension activates and registers Keel commands', async () => {
    const extension = vscode.extensions.getExtension('aggeler.keel-test-bridge');
    assert.ok(extension, 'extension should be discoverable');
    await extension.activate();

    const commands = await vscode.commands.getCommands(true);
    assert.ok(commands.includes('keel.tests.refresh'));
    assert.ok(commands.includes('keel.tests.initConfig'));
    assert.ok(commands.includes('keel.tests.clearTestResults'));
    assert.equal(currentAdapterConfig().displayName, 'Keel');
  });

  // DHF-TEST: keel/requirement-36
  test('discovery capabilities drive clear-result IDs without legacy OpenBrain fallback', async function () {
    this.timeout(10_000);
    const controller = vscode.tests.createTestController(`keelCapabilities-${Date.now()}`, 'Keel Capabilities');
    const tree = publishDiscovery(controller, process.cwd(), {
      version: 1,
      workspace: process.cwd(),
      generated_at: new Date().toISOString(),
      capabilities: {
        clear_results: true,
        clear_results_test_ids: ['keel::maintenance::clear-results']
      },
      items: [{ id: 'keel::maintenance::clear-results', label: 'clear Keel test results', kind: 'maintenance', runnable: true, profiles: ['run'] }]
    });
    setCurrentTreeForTest(tree);

    assert.equal(shouldInvalidateResultsForEvent({
      version: 1,
      event: 'passed',
      time: new Date().toISOString(),
      test_id: 'keel::maintenance::clear-results'
    }), true);
    assert.equal(shouldInvalidateResultsForEvent({
      version: 1,
      event: 'passed',
      time: new Date().toISOString(),
      test_id: 'openbrain::maintenance::clear-results'
    }), false);
    setCurrentTreeForTest(undefined);
    controller.dispose();
  });

  // DHF-TEST: keel/requirement-36
  test('external run mirror closes stale truncated streams as errored terminal runs', async function () {
    this.timeout(10_000);
    const controller = vscode.tests.createTestController(`keelStaleMirror-${Date.now()}`, 'Keel Stale Mirror');
    const tree = publishDiscovery(controller, process.cwd(), {
      version: 1,
      workspace: process.cwd(),
      generated_at: new Date().toISOString(),
      items: [{ id: 'keel::lane::test-fast', label: 'test-fast', kind: 'lane', runnable: true, profiles: ['run'] }]
    });
    setCurrentTreeForTest(tree);
    const mirror = new ExternalRunMirror(controller);
    setExternalRunStaleMsForTest(25);

    const root = process.env.KEEL_VSCODE_BRIDGE_DEV_WORKSPACE;
    assert.ok(root, 'test workspace should be configured');
    const runsDir = path.join(root, '.devtools', 'vscode-runs');
    fs.mkdirSync(runsDir, { recursive: true });
    const runFile = path.join(runsDir, `stale-${process.pid}-${Date.now()}.jsonl`);
    fs.writeFileSync(runFile, [
      JSON.stringify(runEvent({ event: 'run_started', run_id: 'stale-run', test_id: 'keel::lane::test-fast' })),
      JSON.stringify(runEvent({ event: 'test_started', run_id: 'stale-run', test_id: 'keel::lane::test-fast' }))
    ].join('\n') + '\n');
    try {
      await mirror.syncWorkspace();
      await waitFor(() => mirror.snapshots().some((snapshot) => snapshot.runId === 'stale-run' && snapshot.finished));
      const snapshot = mirror.snapshots().find((candidate) => candidate.runId === 'stale-run');
      assert.ok(snapshot?.resultIds.includes('keel::lane::test-fast'));
    } finally {
      fs.rmSync(runFile, { force: true });
      mirror.dispose();
      setCurrentTreeForTest(undefined);
      controller.dispose();
      setExternalRunStaleMsForTest(60_000);
    }
  });

  // DHF-TEST: keel/requirement-36
  test('hostile artifact demotion is rendered as output instead of a clickable artifact link', () => {
    const controller = vscode.tests.createTestController(`keelArtifactDemotion-${Date.now()}`, 'Keel Artifact Demotion');
    const tree = publishDiscovery(controller, process.cwd(), {
      version: 1,
      workspace: process.cwd(),
      generated_at: new Date().toISOString(),
      items: [{ id: 'keel::test::artifact', label: 'artifact', kind: 'test', runnable: true, profiles: ['run'] }]
    });
    const item = tree.itemsById.get('keel::test::artifact');
    assert.ok(item);
    const run = {
      appendOutput(data: string) {
        outputs.push(data);
      }
    };
    const outputs: string[] = [];

    applyRunEvent(run as vscode.TestRun, JSON.stringify(runEvent({
      event: 'output',
      test_id: 'keel::test::artifact',
      message: 'demoted artifact.uri must use file scheme: https://example.invalid/trace.zip'
    })), new Set([item.id]), new Set());

    assert.match(outputs.join(''), /demoted artifact\.uri must use file scheme/);
    assert.doesNotMatch(outputs.join(''), /command:keel\.tests\.openArtifact/);
    controller.dispose();
  });

  // DHF-TEST: keel/requirement-39
  test('Go coverage profile maps module paths to workspace FileCoverage URIs', () => {
    const root = process.cwd();
    const coverages = parseGoCoverageProfile([
      'mode: atomic',
      'github.com/david-aggeler/keel/log/logger.go:10.1,12.2 2 2',
      'github.com/david-aggeler/keel/log/logger.go:14.1,15.2 1 0',
      'github.com/david-aggeler/keel/exec/run.go:20.1,23.2 3 1'
    ].join('\n'), root, 'github.com/david-aggeler/keel');

    const snapshots = coverageFileSnapshotsForTest(coverages);
    assert.deepEqual(snapshots, [
      { uri: path.join(root, 'exec/run.go'), covered: 3, total: 3 },
      { uri: path.join(root, 'log/logger.go'), covered: 2, total: 3 }
    ]);
  });

  // DHF-TEST: keel/requirement-39
  test('coverage artifacts add FileCoverage only for coverage runs and missing artifacts are visible errors', () => {
    const root = process.cwd();
    const profile = path.join(os.tmpdir(), `keel-cover-${process.pid}-${Date.now()}.out`);
    fs.writeFileSync(profile, [
      'mode: atomic',
      'github.com/david-aggeler/keel/log/logger.go:10.1,12.2 2 2'
    ].join('\n'));
    const added: vscode.FileCoverage[] = [];
    const outputs: string[] = [];
    const run = {
      appendOutput(data: string) {
        outputs.push(data);
      },
      addCoverage(fileCoverage: vscode.FileCoverage) {
        added.push(fileCoverage);
      }
    };
    const event = runEvent({
      event: 'artifact',
      test_id: 'keel::lane::test-coverage',
      artifact: { name: 'coverage profile', uri: vscode.Uri.file(profile).toString(), kind: 'coverage' }
    });

    try {
      applyRunEvent(run as vscode.TestRun, JSON.stringify(event), new Set(), new Set(), { coverage: true, workspaceRoot: root, modulePath: 'github.com/david-aggeler/keel' });
      assert.equal(added.length, 1);
      applyRunEvent(run as vscode.TestRun, JSON.stringify(event), new Set(), new Set(), { coverage: false, workspaceRoot: root, modulePath: 'github.com/david-aggeler/keel' });
      assert.equal(added.length, 1, 'plain Run profile must not add coverage');

      fs.rmSync(profile, { force: true });
      applyRunEvent(run as vscode.TestRun, JSON.stringify(event), new Set(), new Set(), { coverage: true, workspaceRoot: root, modulePath: 'github.com/david-aggeler/keel' });
      assert.match(outputs.join(''), /coverage artifact is no longer available/);
    } finally {
      fs.rmSync(profile, { force: true });
    }
  });
});

function runEvent(partial: Partial<RunEvent>): RunEvent {
  return {
    version: 1,
    event: 'output',
    time: new Date().toISOString(),
    ...partial
  } as RunEvent;
}

async function waitFor(predicate: () => boolean, timeoutMs = 2_000): Promise<void> {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    if (predicate()) {
      return;
    }
    await new Promise((resolve) => setTimeout(resolve, 25));
  }
  assert.equal(predicate(), true);
}
