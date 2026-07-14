import * as assert from 'node:assert/strict';
import * as cp from 'node:child_process';
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
  runProfileHandlerForTest,
  setExternalRunStaleMsForTest,
  setCurrentTreeForTest,
  setupPlanOutputLines,
  shouldInvalidateResultsForEvent
} from '../../extension';
import { configRelativePath, currentConfigVersion, defaultConfigTemplate, discoverTests, planTests, readAdapterConfig, runTests, upgradeConfig } from '../../bridgeAdapter';
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
    assert.ok(commands.has('keel.tests.unlock'));
    assert.ok(commands.has('keel.tests.detectLanes'));
    assert.ok(!commands.has('keel.tests.toggleDemoBlock'));
    assert.ok(!commands.has('vela.tests.refresh'));
  });

  // DHF-TEST: keel/requirement-44
  test('manifest surfaces only the frequent commands in Testing-view menus', () => {
    const manifestPath = path.resolve(__dirname, '../../../package.json');
    const manifest = JSON.parse(fs.readFileSync(manifestPath, 'utf8')) as {
      contributes?: {
        menus?: Record<string, Array<{ command: string; when?: string; group?: string }>>;
      };
    };

    const surfacedCommands = Object.values(manifest.contributes?.menus ?? {})
      .flat()
      .map((item) => item.command)
      .sort();
    assert.deepEqual(surfacedCommands, [
      'keel.tests.clearTestResults',
      'keel.tests.refresh'
    ]);
    assert.deepEqual(Object.keys(manifest.contributes?.menus ?? {}).sort(), ['view/title']);
    for (const item of manifest.contributes?.menus?.['view/title'] ?? []) {
      assert.equal(item.when, 'view == workbench.view.testing');
    }
  });

  // DHF-TEST: keel/requirement-59
  test('default template is launcher-only config v3', () => {
    const parsed = JSON.parse(defaultConfigTemplate()) as { version: number; command: string; args: string[]; displayName: string };
    assert.equal(parsed.version, currentConfigVersion);
    assert.equal(parsed.command, 'bin/keel-dev');
    assert.deepEqual(parsed.args, []);
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
        args: ['wrapper'],
        displayName: 'Future',
        extraFutureField: true
      })
    );

    const cfg = readAdapterConfig(root);
    assert.equal(cfg.version, currentConfigVersion + 1);
    assert.equal(cfg.command, 'bin/future-dev');
    assert.deepEqual(cfg.args, ['wrapper']);
    assert.equal(cfg.displayName, 'Future');
  });

  // DHF-TEST: keel/requirement-64
  test('adapter fails loud on released VSIX and devtool version skew before discovery', async function () {
    const manifest = JSON.parse(fs.readFileSync(path.resolve(__dirname, '../../../package.json'), 'utf8')) as { version: string };
    for (const devtoolVersion of ['v0.4.2', 'v0.4.4']) {
      const root = fs.mkdtempSync(path.join(os.tmpdir(), 'keel-version-skew-'));
      const fake = path.join(root, 'fake-devtool.js');
      fs.mkdirSync(path.join(root, '.vscode'), { recursive: true });
      fs.writeFileSync(fake, [
        `if (process.argv.includes('--version')) { console.log('${devtoolVersion}'); process.exit(0); }`,
        "console.log(JSON.stringify({ version: 1, workspace: 'skew', generated_at: new Date().toISOString(), items: [] }));"
      ].join('\n'));
      fs.writeFileSync(path.join(root, configRelativePath), JSON.stringify({
        version: currentConfigVersion,
        command: process.execPath,
        args: [fake],
        displayName: 'Keel'
      }, null, 2) + '\n');
      try {
        await assert.rejects(
          discoverTests(root),
          (err: unknown) => {
            const message = err instanceof Error ? err.message : String(err);
            assert.match(message, /version skew/i);
            assert.match(message, new RegExp(`VSIX v?${manifest.version.replaceAll('.', '\\.')}`));
            assert.match(message, new RegExp(`devtool ${devtoolVersion.replaceAll('.', '\\.')}`));
            assert.doesNotMatch(message, /unknown flag|usage|parse/i);
            return true;
          }
        );
      } finally {
        fs.rmSync(root, { recursive: true, force: true });
      }
    }
  });

  // DHF-TEST: keel/requirement-59, keel/requirement-60
  test('adapter emits canonical test-bridge argv from launcher-only config and upgrades v2 args', async function () {
    this.timeout(10_000);
    const root = fs.mkdtempSync(path.join(os.tmpdir(), 'keel-canonical-wire-'));
    const fixture = path.resolve(__dirname, '../../../src/test/fixtures/fake-adapter.js');
    const configPath = path.join(root, configRelativePath);
    fs.mkdirSync(path.dirname(configPath), { recursive: true });
    fs.writeFileSync(configPath, JSON.stringify({
      version: 2,
      command: process.execPath,
      args: [fixture, 'vscode', 'tests'],
      displayName: 'Keel'
    }, null, 2) + '\n');

    await upgradeConfig(root);
    const migrated = readAdapterConfig(root);
    assert.equal(migrated.version, 3);
    assert.deepEqual(migrated.args, [fixture]);

    const discovery = await discoverTests(root);
    assert.ok(discovery.items.some((item) => item.id === 'keel::lane::ci'));

    const plan = await planTests(root, ['keel::lane::ci']);
    assert.ok(plan.items.some((item) => item.id === 'keel::lane::ci'));

    const run = await collectChild(runTests(root, ['keel::lane::ci']));
    assert.equal(run.code, 0);

    const calls = fs.readFileSync(path.join(root, '.devtools', 'fake-adapter-calls.log'), 'utf8')
      .trim()
      .split(/\r?\n/);
    const protocolCalls = calls.filter((call) => call !== '--version');
    assert.equal(calls.filter((call) => call === '--version').length, 4);
    assert.deepEqual(protocolCalls, [
      'test-bridge config upgrade',
      'test-bridge tests discover --format json',
      'test-bridge tests desired-state --format json --id keel::lane::ci',
      'test-bridge tests run --id keel::lane::ci'
    ]);
    assert.ok(protocolCalls.every((call) => !call.split(/\s+/).includes('vscode')));
    assert.ok(protocolCalls.every((call) => !/\bplan\b/.test(call)));
  });

  // DHF-TEST: keel/requirement-60
  test('desired-state output renders desired/current resources and teardown split', () => {
    const lines = setupPlanOutputLines({
      version: 1,
      workspace: 'keel',
      generated_at: new Date().toISOString(),
      items: [],
      required_resources: ['db'],
      desired_state: [{
        resource: 'db',
        kind: 'service',
        desired: 'seeded',
        current: 'empty',
        status: 'missing',
        action: 'reconcile_during_run',
        message: 'seed during run',
        reusable: false,
        owned: true
      }],
      checks: [],
      actions: [],
      teardown: {
        owned_temporary_resources: ['db'],
        shared_reusable_resources: ['go-toolchain'],
        policy: 'owned resources are torn down after run'
      }
    });

    assert.deepEqual(lines, [
      'desired state:',
      '- db missing: empty -> seeded; action=reconcile_during_run; owned, not reusable; seed during run',
      'teardown:',
      '- owned: db',
      '- reusable: go-toolchain',
      '- policy: owned resources are torn down after run'
    ]);
  });

  // DHF-TEST: keel/requirement-40
  test('extension activates and registers Keel commands', async () => {
    const extension = vscode.extensions.getExtension('aggeler.keel-test-bridge');
    assert.ok(extension, 'extension should be discoverable');
    await extension.activate();

    const commands = await vscode.commands.getCommands(true);
    assert.ok(commands.includes('keel.tests.refresh'));
    assert.ok(commands.includes('keel.tests.initConfig'));
    assert.ok(commands.includes('keel.tests.unlock'));
    assert.ok(commands.includes('keel.tests.clearTestResults'));
    assert.ok(!commands.includes('keel.tests.toggleDemoBlock'));
    assert.equal(currentAdapterConfig().displayName, 'Keel');
  });

  // DHF-TEST: keel/requirement-61
  test('extension manifest exposes no demo-toggle command', async function () {
    this.timeout(10_000);
    const root = process.env.KEEL_VSCODE_BRIDGE_DEV_WORKSPACE;
    assert.ok(root, 'test workspace should be configured');
    const configPath = path.join(root, configRelativePath);
    fs.mkdirSync(path.dirname(configPath), { recursive: true });
    fs.writeFileSync(configPath, JSON.stringify({
      version: currentConfigVersion,
      command: process.execPath,
      args: [path.resolve(__dirname, '../../../src/test/fixtures/fake-adapter.js')],
      displayName: 'Keel'
    }, null, 2) + '\n');
    const callsPath = path.join(root, '.devtools', 'fake-adapter-calls.log');
    fs.rmSync(path.join(root, '.devtools', 'vscode-demo-block.json'), { force: true });
    fs.rmSync(callsPath, { force: true });

    const commands = await vscode.commands.getCommands(true);
    assert.ok(!commands.includes('keel.tests.toggleDemoBlock'));

    await discoverTests(root);
    await planTests(root, ['keel::lane::ci']);
    const nextRun = await collectChild(runTests(root, ['keel::lane::ci']));
    assert.equal(nextRun.code, 0);
    const calls = fs.readFileSync(callsPath, 'utf8');
    assert.doesNotMatch(calls, /\bvscode demo\b/);
  });

  // DHF-TEST: keel/requirement-43
  test('discovery replay renders Go package and test children from the shared fixture', async function () {
    this.timeout(10_000);
    const root = fs.mkdtempSync(path.join(os.tmpdir(), 'keel-fake-discovery-'));
    fs.mkdirSync(path.join(root, '.vscode'), { recursive: true });
    fs.writeFileSync(path.join(root, configRelativePath), JSON.stringify({
      version: currentConfigVersion,
      command: process.execPath,
      args: [path.resolve(__dirname, '../../../src/test/fixtures/fake-adapter.js')],
      displayName: 'Keel'
    }, null, 2) + '\n');

    const discovery = await discoverTests(root);
    const controller = vscode.tests.createTestController(`keelGoDiscovery-${Date.now()}`, 'Keel Go Discovery');
    try {
      const tree = publishDiscovery(controller, root, discovery);
      assert.ok(tree.discoveryItemsById.has('go::root'));
      assert.ok(tree.discoveryItemsById.has('go::pkg::log'));
      assert.ok(tree.discoveryItemsById.has('go::test::log::TestLog'));
      assert.equal(tree.parentByItemId.get('go::pkg::log')?.id, 'go::root');
      assert.equal(tree.parentByItemId.get('go::test::log::TestLog')?.id, 'go::pkg::log');
    } finally {
      controller.dispose();
      fs.rmSync(root, { recursive: true, force: true });
    }
  });

  // DHF-TEST: keel/requirement-42, keel/requirement-62
  test('production bridge argv is accepted by the real keel-dev and keel-demo-dev binaries per verb', async function () {
    this.timeout(30_000);
    const root = fs.mkdtempSync(path.join(os.tmpdir(), 'keel-real-bridge-'));
    fs.mkdirSync(path.join(root, '.vscode'), { recursive: true });
    fs.writeFileSync(path.join(root, 'go.mod'), 'module github.com/david-aggeler/keel\n\ngo 1.25\n');
    fs.writeFileSync(path.join(root, 'go.sum'), '');
    fs.writeFileSync(path.join(root, configRelativePath), JSON.stringify({
      version: currentConfigVersion,
      command: realKeelDevBinary(),
      args: [],
      displayName: 'Keel'
    }, null, 2) + '\n');

    const discovery = await discoverTests(root);
    assert.ok(discovery.items.some((item) => item.id === 'keel::lane::lint'));

    const plan = await planTests(root, ['keel::lane::lint']);
    assert.ok(plan.items.some((item) => item.id === 'keel::lane::lint'));

    const run = await collectChild(runTests(root, ['keel::lane::lint']));
    assert.doesNotMatch(run.stderr + run.stdout, /unknown flag/);
    const terminalEvents = run.stdout.split(/\r?\n/)
      .filter((line) => line.trim().length > 0)
      .map((line) => JSON.parse(line) as RunEvent)
      .filter((event) => event.event === 'run_finished');
    assert.equal(terminalEvents.length, 1);

    fs.writeFileSync(path.join(root, configRelativePath), JSON.stringify({
      version: currentConfigVersion,
      command: realKeelDemoDevBinary(),
      args: [],
      displayName: 'Keel Demo Dev'
    }, null, 2) + '\n');

    const demoDiscovery = await discoverTests(root);
    assert.ok(demoDiscovery.items.some((item) => item.id === 'keel-demo-dev::lane::fake-smoke'));
    const demoController = vscode.tests.createTestController(`keelDemoDevDiscovery-${Date.now()}`, 'Keel Demo Dev Discovery');
    try {
      const demoTree = publishDiscovery(demoController, root, demoDiscovery);
      assert.ok(demoTree.discoveryItemsById.has('keel-demo-dev::maintenance'));
      assert.ok(demoTree.discoveryItemsById.has('keel-demo-dev::lanes'));
      assert.ok(demoTree.discoveryItemsById.has('keel-demo-dev::frameworks'));
      assert.ok(demoTree.discoveryItemsById.has('keel-demo-dev::lane::fake-smoke'));
    } finally {
      demoController.dispose();
    }

    const demoPlan = await planTests(root, ['keel-demo-dev::lane::fake-smoke']);
    assert.ok((demoPlan.desired_state ?? []).some((state) => state.resource === 'database' && state.desired !== state.current));

    const demoBlock = await collectChild(runTests(root, ['keel-demo-dev::maintenance::block-bad-lane']));
    assert.equal(demoBlock.code, 0);
    const demoBlockedRun = await collectChild(runTests(root, ['keel-demo-dev::lane::go-fail']));
    assert.notEqual(demoBlockedRun.code, 0);
    assert.match(demoBlockedRun.stdout, /lane blocked/);
    const demoUnblock = await collectChild(runTests(root, ['keel-demo-dev::maintenance::unblock-bad-lane']));
    assert.equal(demoUnblock.code, 0);

    const demoRun = await collectChild(runTests(root, ['keel-demo-dev::lane::go-pass']));
    assert.doesNotMatch(demoRun.stderr + demoRun.stdout, /unknown flag/);
    const demoTerminalEvents = demoRun.stdout.split(/\r?\n/)
      .filter((line) => line.trim().length > 0)
      .map((line) => JSON.parse(line) as RunEvent)
      .filter((event) => event.event === 'run_finished');
    assert.equal(demoTerminalEvents.length, 1);
    assert.equal(demoTerminalEvents[0].exit_code, 0);

    await upgradeConfig(root);

    const devRoot = keelModuleRootFromTestLocation();
    const previousDevWorkspace = process.env.KEEL_VSCODE_BRIDGE_DEV_WORKSPACE;
    process.env.KEEL_VSCODE_BRIDGE_DEV_WORKSPACE = devRoot;
    const devConfigPath = path.join(devRoot, configRelativePath);
    const previousConfig = fs.existsSync(devConfigPath) ? fs.readFileSync(devConfigPath, 'utf8') : undefined;
    fs.mkdirSync(path.dirname(devConfigPath), { recursive: true });
    fs.writeFileSync(devConfigPath, JSON.stringify({
      version: currentConfigVersion,
      command: realKeelDevBinary(),
      args: [],
      displayName: 'Keel'
    }, null, 2) + '\n');
    const runStreamRoot = devRoot;
    const runsDir = path.join(runStreamRoot, '.devtools', 'vscode-runs');
    fs.rmSync(path.join(runsDir, 'run.lock'), { force: true });
    const beforeRunStreams = new Set(listRunStreams(runsDir));
    try {
      const extension = vscode.extensions.getExtension('aggeler.keel-test-bridge');
      assert.ok(extension, 'extension should be discoverable');
      await extension.activate();
      await runProfileHandlerForTest('keel::lane::lint');

      const newStreams = listRunStreams(runsDir).filter((candidate) => !beforeRunStreams.has(candidate));
      assert.equal(newStreams.length, 1, `TestController run should create one external run stream under ${runStreamRoot}`);
      const runEvents = parseRunEvents(fs.readFileSync(newStreams[0], 'utf8'));
      assert.ok(runEvents.some((event) => event.event === 'run_started'));
      assert.equal(runEvents.filter((event) => event.event === 'run_finished').length, 1);
      assert.doesNotMatch(fs.readFileSync(newStreams[0], 'utf8'), /unknown flag/);
    } finally {
      if (previousDevWorkspace === undefined) {
        delete process.env.KEEL_VSCODE_BRIDGE_DEV_WORKSPACE;
      } else {
        process.env.KEEL_VSCODE_BRIDGE_DEV_WORKSPACE = previousDevWorkspace;
      }
      if (previousConfig === undefined) {
        fs.rmSync(devConfigPath, { force: true });
      } else {
        fs.writeFileSync(devConfigPath, previousConfig);
      }
    }
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

function realKeelDevBinary(): string {
  const exe = process.platform === 'win32' ? 'keel-dev.exe' : 'keel-dev';
  return path.resolve(__dirname, '../../../../bin', exe);
}

function realKeelDemoDevBinary(): string {
  const exe = process.platform === 'win32' ? 'keel-demo-dev.exe' : 'keel-demo-dev';
  return path.resolve(__dirname, '../../../../bin', exe);
}

function keelModuleRootFromTestLocation(): string {
  const root = path.resolve(__dirname, '../../../..');
  const goMod = path.join(root, 'go.mod');
  assert.ok(
    fs.existsSync(goMod) && /^module github\.com\/david-aggeler\/keel$/m.test(fs.readFileSync(goMod, 'utf8')),
    `compiled test location should resolve to the Keel module root: ${root}`
  );
  return root;
}

function listRunStreams(runsDir: string): string[] {
  if (!fs.existsSync(runsDir)) {
    return [];
  }
  return fs.readdirSync(runsDir)
    .filter((name) => name.endsWith('.jsonl'))
    .map((name) => path.join(runsDir, name))
    .sort();
}

function parseRunEvents(jsonl: string): RunEvent[] {
  return jsonl.split(/\r?\n/)
    .filter((line) => line.trim().length > 0)
    .map((line) => JSON.parse(line) as RunEvent);
}

async function collectChild(child: cp.ChildProcessWithoutNullStreams): Promise<{ code: number | null; stdout: string; stderr: string }> {
  let stdout = '';
  let stderr = '';
  child.stdout.on('data', (chunk: Buffer) => {
    stdout += chunk.toString('utf8');
  });
  child.stderr.on('data', (chunk: Buffer) => {
    stderr += chunk.toString('utf8');
  });
  return await new Promise((resolve, reject) => {
    child.on('error', reject);
    child.on('close', (code) => {
      resolve({ code, stdout, stderr });
    });
  });
}
