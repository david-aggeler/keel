import * as assert from 'node:assert/strict';
import * as cp from 'node:child_process';
import * as fs from 'node:fs';
import * as os from 'node:os';
import * as path from 'node:path';
import * as vscode from 'vscode';
import {
  applyRunEvent,
  activeRunStatusSnapshot,
  artifactCommandUri,
  artifactOutputLine,
  beginActiveRun,
  cancelActiveRun,
  coverageFileSnapshotsForTest,
  currentTree,
  currentAdapterConfig,
  deferredWatcherEventCountForTest,
  desiredStateRowProtocolID,
  ExternalRunMirror,
  finishActiveRun,
  isRunActive,
  isWatcherRefreshPending,
  parseGoCoverageProfile,
  publishedTestItemIds,
  rejectConcurrentRun,
  resultItemsForRunEvent,
  runEventApplicationSnapshot,
  runProfileHandlerForTest,
  setWatcherDebounceMs,
  setExternalRunStaleMsForTest,
  setCurrentTreeForTest,
  desiredStateDocumentOutputLines,
  shouldInvalidateResultsForEvent,
  shouldApplyResultToItem,
  signalProcessGroup,
  invalidateClearedResults,
  testControllerForTest,
  testMessageFromEvent,
  timestampedRunOutputLines,
  triggerWatcherEventForTest
} from '../../extension';
import { adapterConfig, configRelativePath, currentConfigVersion, defaultConfigTemplate, discoverTests, readDesiredState, readAdapterConfig, runTests, upgradeConfig } from '../../bridgeAdapter';
import { publishDiscovery, replacePublishedTestItem } from '../../tree';
import { DesiredStateGroup, DiscoveryItem, RunEvent } from '../../protocol';

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

  // DHF-TEST: keel/requirement-40
  test('adapter config resolves relative commands and preserves env overrides', () => {
    const root = fs.mkdtempSync(path.join(os.tmpdir(), 'keel-adapter-config-'));
    fs.mkdirSync(path.join(root, '.vscode'), { recursive: true });
    fs.writeFileSync(path.join(root, configRelativePath), JSON.stringify({
      version: currentConfigVersion,
      command: 'tools/custom-devtool',
      args: ['launcher'],
      displayName: 'Custom',
      env: { KEEL_ADAPTER_ENV_TEST: 'configured' }
    }, null, 2) + '\n');

    try {
      const cfg = adapterConfig(root);
      assert.equal(cfg.command, path.join(root, 'tools', 'custom-devtool'));
      assert.deepEqual(cfg.args, ['launcher']);
      assert.equal(cfg.displayName, 'Custom');
      assert.equal(cfg.outputChannel, 'Custom Test Bridge');
      assert.deepEqual(cfg.env, { KEEL_ADAPTER_ENV_TEST: 'configured' });
    } finally {
      fs.rmSync(root, { recursive: true, force: true });
    }
  });

  // DHF-TEST: keel/requirement-40, keel/requirement-66
  test('adapter env is applied to version checks, discovery, and run spawns', async function () {
    this.timeout(10_000);
    const root = fs.mkdtempSync(path.join(os.tmpdir(), 'keel-adapter-env-'));
    const fake = path.join(root, 'env-adapter.cjs');
    fs.mkdirSync(path.join(root, '.vscode'), { recursive: true });
    fs.writeFileSync(fake, [
      "const fs = require('node:fs');",
      "const path = require('node:path');",
      "const args = process.argv.slice(2);",
      "const now = () => new Date().toISOString();",
      "const seen = process.env.KEEL_ADAPTER_ENV_TEST || 'missing';",
      "fs.mkdirSync(path.join(process.cwd(), '.devtools'), { recursive: true });",
      "fs.appendFileSync(path.join(process.cwd(), '.devtools', 'env-adapter.log'), `${args.join(' ')} env=${seen}\\n`);",
      "if (args.includes('--version')) { console.log('dev'); process.exit(0); }",
      "if (args.join(' ') === 'test-bridge tests discover --format json') {",
      "  console.log(JSON.stringify({ version: 1, workspace: seen, generated_at: now(), items: [] }));",
      "  process.exit(0);",
      "}",
      "if (args.slice(0, 3).join(' ') === 'test-bridge tests run') {",
      "  process.stdout.write(`${JSON.stringify({ version: 1, event: 'run_started', time: now(), message: seen })}\\n`);",
      "  process.stdout.write(`${JSON.stringify({ version: 1, event: 'run_finished', time: now(), exit_code: 0 })}\\n`);",
      "  process.exit(0);",
      "}",
      "process.exit(2);"
    ].join('\n'));
    fs.writeFileSync(path.join(root, configRelativePath), JSON.stringify({
      version: currentConfigVersion,
      command: process.execPath,
      args: [fake],
      displayName: 'Env Adapter',
      env: { KEEL_ADAPTER_ENV_TEST: 'configured' }
    }, null, 2) + '\n');

    try {
      const discovery = await discoverTests(root);
      assert.equal(discovery.workspace, 'configured');

      const run = await collectChild(runTests(root, ['keel::lane::env']));
      assert.equal(run.code, 0);
      assert.match(run.stdout, /"message":"configured"/);
      assert.match(fs.readFileSync(path.join(root, '.devtools', 'env-adapter.log'), 'utf8'), /--version env=configured/);
    } finally {
      fs.rmSync(root, { recursive: true, force: true });
    }
  });

  // DHF-TEST: keel/requirement-40, keel/requirement-66
  test('adapter rejects malformed config, unsupported documents, and unreadable versions', async function () {
    this.timeout(10_000);
    const root = fs.mkdtempSync(path.join(os.tmpdir(), 'keel-adapter-rejects-'));
    const fake = path.join(root, 'rejecting-adapter.cjs');
    fs.mkdirSync(path.join(root, '.vscode'), { recursive: true });

    const writeConfig = (body: unknown) => {
      fs.writeFileSync(path.join(root, configRelativePath), JSON.stringify(body, null, 2) + '\n');
    };

    try {
      writeConfig({ command: 'bin/keel-dev', args: [], displayName: 'Keel' });
      assert.throws(() => readAdapterConfig(root), /missing numeric version/);

      writeConfig({ version: currentConfigVersion, command: '', args: [], displayName: 'Keel' });
      assert.throws(() => readAdapterConfig(root), /missing command/);

      writeConfig({ version: currentConfigVersion, command: 'bin/keel-dev', args: ['test-bridge'], displayName: 'Keel' });
      assert.throws(() => readAdapterConfig(root), /launcher-only/);

      writeConfig({ version: currentConfigVersion, command: 'bin/keel-dev', args: [], displayName: '' });
      assert.throws(() => readAdapterConfig(root), /missing displayName/);

      fs.writeFileSync(fake, [
        "const args = process.argv.slice(2);",
        "if (args.includes('--version')) { console.log(process.env.BAD_VERSION ? 'not-a-version' : 'dev'); process.exit(process.env.BAD_VERSION ? 2 : 0); }",
        "if (args.join(' ') === 'test-bridge tests discover --format json') { console.log(JSON.stringify({ version: 2, items: null })); process.exit(0); }",
        "if (args.slice(0, 4).join(' ') === 'test-bridge tests desired-state --format') { console.log(JSON.stringify({ version: 1, groups: null })); process.exit(0); }",
        "process.exit(2);"
      ].join('\n'));

      writeConfig({
        version: currentConfigVersion,
        command: process.execPath,
        args: [fake],
        displayName: 'Rejecting'
      });
      await assert.rejects(discoverTests(root), /unsupported VS Code discovery document/);
      await assert.rejects(readDesiredState(root, ['case::id']), /unsupported VS Code desired-state document/);

      writeConfig({
        version: currentConfigVersion,
        command: process.execPath,
        args: [fake],
        displayName: 'Rejecting',
        env: { BAD_VERSION: '1' }
      });
      assert.throws(() => runTests(root, ['case::id']), /could not read devtool version/);
    } finally {
      fs.rmSync(root, { recursive: true, force: true });
    }
  });

  // DHF-TEST: keel/requirement-64, keel/requirement-66
  test('adapter fails loud on released VSIX and devtool version skew before discovery', async function () {
    const manifest = JSON.parse(fs.readFileSync(path.resolve(__dirname, '../../../package.json'), 'utf8')) as { version: string };
    for (const devtoolVersion of ['v0.0.0']) {
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

  // DHF-TEST: keel/requirement-66
  test('adapter permits discovery when released VSIX and devtool versions match', async function () {
    const manifest = JSON.parse(fs.readFileSync(path.resolve(__dirname, '../../../package.json'), 'utf8')) as { version: string };
    const root = fs.mkdtempSync(path.join(os.tmpdir(), 'keel-version-match-'));
    const fake = path.join(root, 'fake-devtool.js');
    fs.mkdirSync(path.join(root, '.vscode'), { recursive: true });
    fs.writeFileSync(fake, [
      `if (process.argv.includes('--version')) { console.log('v${manifest.version}'); process.exit(0); }`,
      "console.log(JSON.stringify({ version: 1, workspace: 'matched', generated_at: new Date().toISOString(), items: [] }));"
    ].join('\n'));
    fs.writeFileSync(path.join(root, configRelativePath), JSON.stringify({
      version: currentConfigVersion,
      command: process.execPath,
      args: [fake],
      displayName: 'Keel'
    }, null, 2) + '\n');
    try {
      const discovery = await discoverTests(root);
      assert.equal(discovery.workspace, 'matched');
      assert.deepEqual(discovery.items, []);
    } finally {
      fs.rmSync(root, { recursive: true, force: true });
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

    const desiredState = await readDesiredState(root, ['keel::lane::ci']);
    assert.equal(desiredState.version, 3);
    assert.ok(desiredState.groups.some((group) => group.rows.some((row) => row.resource === 'python')));

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
    const retiredVerb = ['p', 'l', 'a', 'n'].join('');
    assert.ok(protocolCalls.every((call) => !new RegExp(`\\b${retiredVerb}\\b`).test(call)));
  });

  // DHF-TEST: keel/requirement-60
  test('desired-state output renders groups, active rows, resources, and teardown split', () => {
    const lines = desiredStateDocumentOutputLines({
      version: 3,
      workspace: 'keel',
      generated_at: new Date().toISOString(),
      groups: [{
        label: 'Data Set',
        order: 10,
        mutually_exclusive: true,
        rows: [{
          resource: 'db',
          kind: 'service',
          desired: 'seeded',
          current: 'empty',
          status: 'reconcilable',
          action: 'reconcile_during_run',
          message: 'seed during run',
          reusable: false,
          owned: true,
          active: true
        }, {
          resource: 'go-toolchain',
          kind: 'tool',
          desired: 'available',
          current: 'available',
          status: 'satisfied',
          action: 'reuse',
          message: 'go available',
          reusable: true,
          owned: false
        }]
      }],
      teardown_policy: 'owned resources are torn down after run'
    });

    assert.deepEqual(lines, [
      'desired state:',
      'Data Set (mutually exclusive)',
      '- [active] db reconcilable: empty -> seeded; action=reconcile_during_run; owned, not reusable; seed during run',
      '- go-toolchain satisfied: available -> available; action=reuse; shared, reusable; go available',
      'teardown:',
      '- owned: db',
      '- reusable: go-toolchain',
      '- policy: owned resources are torn down after run'
    ]);
  });

  // DHF-TEST: keel/requirement-42
  test('run output helpers normalize nested log prefixes and artifact command URIs', () => {
    const lines = timestampedRunOutputLines([
      '2025-01-01 00:00:00 stdout 2025-01-01 00:00:00 warn queued',
      '2025-01-01 00:00:01 stderr 2025-01-01 00:00:01 error failed',
      'plain output'
    ].join('\n'), new Date(2026, 0, 2, 3, 4, 5), 'DEBUG');

    assert.deepEqual(lines, [
      '2026-01-02 03:04:05 WARN queued',
      '2026-01-02 03:04:05 ERROR failed',
      '2026-01-02 03:04:05 DEBUG plain output'
    ]);

    const artifact = runEvent({
      event: 'artifact',
      test_id: 'keel::lane::ci',
      artifact: { name: 'log', uri: '/tmp/keel.log', kind: 'log' }
    });
    assert.match(artifactCommandUri('/tmp/keel.log'), /^command:keel\.tests\.openArtifact\?/);
    assert.match(artifactOutputLine(artifact), /artifact keel::lane::ci: log log \/tmp\/keel\.log/);
    assert.equal(artifactOutputLine(runEvent({ event: 'artifact', message: 'artifact omitted' })), 'artifact omitted\r\n');
  });

  // DHF-TEST: keel/requirement-60
  test('desired-state rows activate through the ordinary run argv path', async function () {
    this.timeout(10_000);
    const root = fs.mkdtempSync(path.join(os.tmpdir(), 'keel-desired-state-row-run-'));
    const previousDevWorkspace = process.env.KEEL_VSCODE_BRIDGE_DEV_WORKSPACE;
    process.env.KEEL_VSCODE_BRIDGE_DEV_WORKSPACE = root;
    fs.mkdirSync(path.join(root, '.vscode'), { recursive: true });
    fs.writeFileSync(path.join(root, configRelativePath), JSON.stringify({
      version: currentConfigVersion,
      command: process.execPath,
      args: [path.resolve(__dirname, '../../../src/test/fixtures/fake-adapter.js')],
      displayName: 'Keel'
    }, null, 2) + '\n');

    // The fake adapter is strict like the real bridge: it rejects ids it did
    // not serve. The runnable row is keyed by its served run_id; the
    // informational row keeps the VSIX-private display id and never reaches
    // the wire (formal_review-80).
    const servedRunID = 'keel::action::provision-python-venv';
    const rowGroup: DesiredStateGroup = {
      label: 'Test Preconditions',
      order: 10,
      mutually_exclusive: false,
      rows: []
    };
    const informationalRowID = desiredStateRowProtocolID(rowGroup, {
      resource: 'python',
      kind: 'tool',
      desired: 'available',
      current: 'available',
      status: 'satisfied',
      action: 'reuse',
      message: 'python available',
      reusable: true,
      owned: false
    });

    try {
      const extension = vscode.extensions.getExtension('aggeler.keel-test-bridge');
      assert.ok(extension, 'extension should be discoverable');
      await extension.activate();

      await runProfileHandlerForTest(servedRunID);
      const callsAfterRunnable = fs.readFileSync(path.join(root, '.devtools', 'fake-adapter-calls.log'), 'utf8')
        .trim()
        .split(/\r?\n/);
      assert.equal(
        callsAfterRunnable.filter((call) => call === 'test-bridge tests desired-state --format json').length,
        0,
        'refresh must render Desired State from discovery without a live empty-selection probe'
      );
      assert.ok(callsAfterRunnable.includes(`test-bridge tests desired-state --format json --id ${servedRunID}`));
      assert.ok(callsAfterRunnable.includes(`test-bridge tests run --id ${servedRunID}`));

      await runProfileHandlerForTest(informationalRowID);
      const callsAfterInformational = fs.readFileSync(path.join(root, '.devtools', 'fake-adapter-calls.log'), 'utf8')
        .trim()
        .split(/\r?\n/);
      const informationalRuns = callsAfterInformational.filter((call) => call.includes('tests run') && call.includes(informationalRowID));
      assert.equal(informationalRuns.length, 0, 'informational rows must never be submitted on the wire');
      const informationalDesiredStateCalls = callsAfterInformational.filter((call) => call.includes('desired-state') && call.includes(informationalRowID));
      assert.equal(informationalDesiredStateCalls.length, 0, 'informational display ids must never be sent on the desired-state wire path');
    } finally {
      if (previousDevWorkspace === undefined) {
        delete process.env.KEEL_VSCODE_BRIDGE_DEV_WORKSPACE;
      } else {
        process.env.KEEL_VSCODE_BRIDGE_DEV_WORKSPACE = previousDevWorkspace;
      }
      fs.rmSync(root, { recursive: true, force: true });
    }
  });

  // DHF-TEST: keel/requirement-88
  test('run-finished re-queries desired state and refreshes rendered rows', async function () {
    this.timeout(10_000);
    const root = fs.mkdtempSync(path.join(os.tmpdir(), 'keel-post-run-desired-state-'));
    const previousDevWorkspace = process.env.KEEL_VSCODE_BRIDGE_DEV_WORKSPACE;
    process.env.KEEL_VSCODE_BRIDGE_DEV_WORKSPACE = root;
    fs.mkdirSync(path.join(root, '.vscode'), { recursive: true });
    fs.writeFileSync(path.join(root, configRelativePath), JSON.stringify({
      version: currentConfigVersion,
      command: process.execPath,
      args: [path.resolve(__dirname, '../../../src/test/fixtures/fake-adapter.js')],
      displayName: 'Keel'
    }, null, 2) + '\n');

    try {
      const extension = vscode.extensions.getExtension('aggeler.keel-test-bridge');
      assert.ok(extension, 'extension should be discoverable');
      await extension.activate();

      const runID = 'keel::action::provision-python-venv';
      await runProfileHandlerForTest(runID);

      const calls = fs.readFileSync(path.join(root, '.devtools', 'fake-adapter-calls.log'), 'utf8')
        .trim()
        .split(/\r?\n/);
      assert.equal(
        calls.filter((call) => call === `test-bridge tests desired-state --format json --id ${runID}`).length,
        2,
        'run-finished must re-query desired-state after the devtool changes the selected row'
      );
      const refreshed = currentTree()?.discoveryItemsById.get(runID);
      assert.ok(refreshed, 'post-run refresh should keep the selected desired-state row published');
      assert.ok(refreshed.limitations?.includes('active=true'), `post-run row limitations = ${refreshed.limitations}`);
      assert.ok(refreshed.limitations?.includes('action=reuse'), `post-run row limitations = ${refreshed.limitations}`);
    } finally {
      if (previousDevWorkspace === undefined) {
        delete process.env.KEEL_VSCODE_BRIDGE_DEV_WORKSPACE;
      } else {
        process.env.KEEL_VSCODE_BRIDGE_DEV_WORKSPACE = previousDevWorkspace;
      }
      fs.rmSync(root, { recursive: true, force: true });
    }
  });

  // DHF-TEST: keel/requirement-93
  test('run-finished reconciles exclusive-group results at rest after discovery refresh', async function () {
    this.timeout(10_000);
    const root = fs.mkdtempSync(path.join(os.tmpdir(), 'keel-post-run-mutex-results-'));
    const previousDevWorkspace = process.env.KEEL_VSCODE_BRIDGE_DEV_WORKSPACE;
    process.env.KEEL_VSCODE_BRIDGE_DEV_WORKSPACE = root;
    fs.mkdirSync(path.join(root, '.vscode'), { recursive: true });
    const adapter = path.join(root, 'exclusive-adapter.js');
    fs.writeFileSync(adapter, `
const fs = require('node:fs');
const path = require('node:path');
const args = process.argv.slice(2);
const now = () => new Date().toISOString();
const activePath = path.join(process.cwd(), '.devtools', 'active-member');
if (args.includes('--version')) {
  process.stdout.write('dev\\n');
  process.exit(0);
}
function activeMember() {
  try {
    return fs.readFileSync(activePath, 'utf8').trim();
  } catch {
    return 'demo::desired-state::dataset::small';
  }
}
function discovery() {
  const active = activeMember();
  return {
    version: 1,
    workspace: process.cwd(),
    generated_at: now(),
    capabilities: { reconcile_results: [
      'demo::desired-state::dataset::small',
      'demo::desired-state::dataset::full',
      'demo::desired-state::dataset::unknown'
    ].map((id) => ({ test_id: id, state: id === active ? 'passed' : 'skipped', message: id === active ? 'active' : 'not active' })) },
    items: [
      { id: 'demo::desired-state::dataset', label: 'Data Set', kind: 'group', runnable: false, profiles: [], limitations: ['mutually_exclusive=true'] },
      { id: 'demo::desired-state::dataset::small', parent_id: 'demo::desired-state::dataset', label: 'small', kind: 'test', runnable: true, profiles: ['run'], limitations: ['active=' + (active === 'demo::desired-state::dataset::small')] },
      { id: 'demo::desired-state::dataset::full', parent_id: 'demo::desired-state::dataset', label: 'full', kind: 'test', runnable: true, profiles: ['run'], limitations: ['active=' + (active === 'demo::desired-state::dataset::full')] },
      { id: 'demo::desired-state::dataset::unknown', parent_id: 'demo::desired-state::dataset', label: 'Unknown State', kind: 'test', runnable: true, profiles: ['run'], limitations: ['active=' + (active === 'demo::desired-state::dataset::unknown')] }
    ]
  };
}
function desiredState() {
  const active = activeMember();
  const rows = [
    ['demo::desired-state::dataset::small', 'small'],
    ['demo::desired-state::dataset::full', 'full'],
    ['demo::desired-state::dataset::unknown', 'Unknown State']
  ].map(([run_id, resource]) => ({
    run_id,
    resource,
    kind: 'dataset',
    desired: resource,
    current: resource,
    status: run_id === active ? 'satisfied' : 'available',
    action: run_id === active ? 'reuse' : 'none',
    message: resource,
    reusable: true,
    owned: false,
    active: run_id === active
  }));
  return {
    version: 3,
    workspace: process.cwd(),
    generated_at: now(),
    groups: [{ label: 'Data Set', order: 1, mutually_exclusive: true, rows }]
  };
}
if (args.slice(0, 4).join(' ') === 'test-bridge tests discover --format') {
  process.stdout.write(JSON.stringify(discovery()) + '\\n');
  process.exit(0);
}
if (args.slice(0, 4).join(' ') === 'test-bridge tests desired-state --format') {
  process.stdout.write(JSON.stringify(desiredState()) + '\\n');
  process.exit(0);
}
if (args.slice(0, 3).join(' ') === 'test-bridge tests run') {
  const selected = args[args.indexOf('--id') + 1];
  fs.mkdirSync(path.dirname(activePath), { recursive: true });
  fs.writeFileSync(activePath, selected + '\\n');
  const emit = (event) => process.stdout.write(JSON.stringify({ version: 1, time: now(), run_id: 'mutex-run', ...event }) + '\\n');
  emit({ event: 'run_started', test_id: selected });
  emit({ event: 'test_started', test_id: selected });
  emit({ event: 'passed', test_id: selected, duration_ms: 1 });
  emit({ event: 'run_finished', exit_code: 0 });
  process.exit(0);
}
process.stderr.write('unsupported command ' + args.join(' ') + '\\n');
process.exit(2);
`);
    fs.writeFileSync(path.join(root, configRelativePath), JSON.stringify({
      version: currentConfigVersion,
      command: process.execPath,
      args: [adapter],
      displayName: 'Keel'
    }, null, 2) + '\n');

    try {
      const extension = vscode.extensions.getExtension('aggeler.keel-test-bridge');
      assert.ok(extension, 'extension should be discoverable');
      await extension.activate();
      await vscode.commands.executeCommand('keel.tests.refresh');
      const controller = testControllerForTest();
      const initialTree = currentTree();
      assert.ok(controller, 'extension should expose its active TestController for tests');
      assert.ok(initialTree, 'discovery should publish the initial tree');
      const originalSmall = initialTree.itemsById.get('demo::desired-state::dataset::small');
      const originalFull = initialTree.itemsById.get('demo::desired-state::dataset::full');
      const originalUnknown = initialTree.itemsById.get('demo::desired-state::dataset::unknown');
      assert.ok(originalSmall && originalFull && originalUnknown, 'exclusive group members should be published');

      interface ReconcileRunRecord { persisted: boolean; stamps: Array<[string, string]> }
      const reconcileRuns: ReconcileRunRecord[] = [];
      const originalCreateTestRun = controller.createTestRun.bind(controller);
      controller.createTestRun = ((request: vscode.TestRunRequest, name?: string, persist?: boolean) => {
        const run = originalCreateTestRun(request, name, persist);
        if (name !== 'desired-state reconcile') {
          return run;
        }
        const record: ReconcileRunRecord = { persisted: run.isPersisted, stamps: [] };
        reconcileRuns.push(record);
        const originalPassed = run.passed.bind(run);
        const originalSkipped = run.skipped.bind(run);
        run.passed = (item: vscode.TestItem, duration?: number) => {
          record.stamps.push([item.id, 'passed']);
          originalPassed(item, duration);
        };
        run.skipped = (item: vscode.TestItem) => {
          record.stamps.push([item.id, 'skipped']);
          originalSkipped(item);
        };
        return run;
      }) as typeof controller.createTestRun;

      try {
        await runProfileHandlerForTest('demo::desired-state::dataset::full');
      } finally {
        controller.createTestRun = originalCreateTestRun;
      }

      const refreshedTree = currentTree();
      assert.ok(refreshedTree, 'post-run discovery refresh should leave a published tree');
      // Pre-run, small was active; the run activates full, and the post-run
      // discovery refresh replays the flipped bridge-served states through a
      // non-persisted reconcile run — proving the stamp reconcile fired.
      const lastReconcile = reconcileRuns[reconcileRuns.length - 1];
      assert.ok(lastReconcile, 'a desired-state reconcile run fires on the post-run refresh');
      assert.equal(lastReconcile.persisted, false, 'the reconcile run is non-persisted');
      assert.deepEqual([...lastReconcile.stamps].sort(), [
        ['demo::desired-state::dataset::full', 'passed'],
        ['demo::desired-state::dataset::small', 'skipped'],
        ['demo::desired-state::dataset::unknown', 'skipped']
      ], 'the post-run reconcile stamps the newly active member passed and all peers skipped');
      // The stamp mechanism overwrites results instead of replacing items, so
      // every member keeps its TestItem identity (requirement-70 default).
      assert.equal(refreshedTree.itemsById.get('demo::desired-state::dataset::small'), originalSmall, 'members keep their TestItem identity — reconcile stamps, it does not rebuild');
      assert.equal(refreshedTree.itemsById.get('demo::desired-state::dataset::unknown'), originalUnknown, 'the Unknown peer keeps its TestItem identity');
      const fullAfterRun = refreshedTree.itemsById.get('demo::desired-state::dataset::full');
      assert.equal(fullAfterRun, originalFull, 'the active member keeps its TestItem identity');

      await vscode.commands.executeCommand('keel.tests.refresh');
      const afterExplicitRefresh = currentTree();
      assert.ok(afterExplicitRefresh, 'explicit discovery refresh should leave a published tree');
      assert.equal(afterExplicitRefresh.itemsById.get('demo::desired-state::dataset::full'), fullAfterRun, 'active member keeps its TestItem identity across an at-rest refresh');
    } finally {
      if (previousDevWorkspace === undefined) {
        delete process.env.KEEL_VSCODE_BRIDGE_DEV_WORKSPACE;
      } else {
        process.env.KEEL_VSCODE_BRIDGE_DEV_WORKSPACE = previousDevWorkspace;
      }
      fs.rmSync(root, { recursive: true, force: true });
    }
  });

  // ac-311 (requirement-97 / design_decision-5): the VSIX applies bridge
  // decisions verbatim and must not branch on the mutually_exclusive wire
  // flag. Allowed occurrences in production sources: the protocol.ts type
  // declaration and the display passthrough in formatDesiredStateGroup.
  //
  // DHF-TEST: keel/requirement-97
  test('production sources do not branch on mutually_exclusive', () => {
    const srcDir = path.resolve(__dirname, '../../../src');
    const allowed = new Map([['protocol.ts', 1], ['extension.ts', 1]]);
    const offenders: string[] = [];
    for (const entry of fs.readdirSync(srcDir, { withFileTypes: true })) {
      if (!entry.isFile() || !entry.name.endsWith('.ts')) {
        continue;
      }
      const occurrences = fs.readFileSync(path.join(srcDir, entry.name), 'utf8').split('mutually_exclusive').length - 1;
      if (occurrences !== (allowed.get(entry.name) ?? 0)) {
        offenders.push(`${entry.name}: ${occurrences} occurrence(s), allowed ${allowed.get(entry.name) ?? 0}`);
      }
    }
    assert.deepEqual(offenders, [], 'mutually_exclusive may appear only as the protocol type declaration and the verbatim display passthrough');
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

  // DHF-TEST: keel/requirement-40
  test('current adapter config falls back to the workspace default when config cannot be read', () => {
    const root = fs.mkdtempSync(path.join(os.tmpdir(), 'keel-config-fallback-'));
    const previousDevWorkspace = process.env.KEEL_VSCODE_BRIDGE_DEV_WORKSPACE;
    process.env.KEEL_VSCODE_BRIDGE_DEV_WORKSPACE = root;
    fs.mkdirSync(path.join(root, '.vscode'), { recursive: true });
    fs.writeFileSync(path.join(root, configRelativePath), '{not-json');

    try {
      const cfg = currentAdapterConfig();
      assert.equal(cfg.version, currentConfigVersion);
      assert.equal(cfg.command, path.join(root, 'bin', process.platform === 'win32' ? 'keel-dev.exe' : 'keel-dev'));
      assert.deepEqual(cfg.args, []);
      assert.equal(cfg.displayName, 'Keel');
      assert.equal(cfg.outputChannel, 'Keel Test Bridge');
    } finally {
      if (previousDevWorkspace === undefined) {
        delete process.env.KEEL_VSCODE_BRIDGE_DEV_WORKSPACE;
      } else {
        process.env.KEEL_VSCODE_BRIDGE_DEV_WORKSPACE = previousDevWorkspace;
      }
      fs.rmSync(root, { recursive: true, force: true });
    }
  });

  // DHF-TEST: keel/requirement-36, keel/requirement-44
  test('registered commands run maintenance paths and report missing artifacts', async function () {
    this.timeout(10_000);
    const root = fs.mkdtempSync(path.join(os.tmpdir(), 'keel-command-maintenance-'));
    const previousDevWorkspace = process.env.KEEL_VSCODE_BRIDGE_DEV_WORKSPACE;
    process.env.KEEL_VSCODE_BRIDGE_DEV_WORKSPACE = root;
    fs.mkdirSync(path.join(root, '.vscode'), { recursive: true });
    fs.writeFileSync(path.join(root, configRelativePath), JSON.stringify({
      version: currentConfigVersion,
      command: process.execPath,
      args: [path.resolve(__dirname, '../../../src/test/fixtures/fake-adapter.js')],
      displayName: 'Keel'
    }, null, 2) + '\n');

    try {
      const extension = vscode.extensions.getExtension('aggeler.keel-test-bridge');
      assert.ok(extension, 'extension should be discoverable');
      await extension.activate();

      await vscode.commands.executeCommand('keel.tests.clearLocalState');
      await vscode.commands.executeCommand('keel.tests.unlock');
      await vscode.commands.executeCommand('keel.tests.detectLanes');
      await vscode.commands.executeCommand('keel.tests.openArtifact', path.join(root, 'missing-artifact.txt'));

      const calls = fs.readFileSync(path.join(root, '.devtools', 'fake-adapter-calls.log'), 'utf8');
      assert.match(calls, /test-bridge tests run --id keel::maintenance::clear-state/);
      assert.match(calls, /test-bridge tests run --id keel::maintenance::unlock/);
      assert.match(calls, /test-bridge tests run --id keel::maintenance::detect-lanes/);
    } finally {
      if (previousDevWorkspace === undefined) {
        delete process.env.KEEL_VSCODE_BRIDGE_DEV_WORKSPACE;
      } else {
        process.env.KEEL_VSCODE_BRIDGE_DEV_WORKSPACE = previousDevWorkspace;
      }
      fs.rmSync(root, { recursive: true, force: true });
    }
  });

  // DHF-TEST: keel/requirement-51, keel/requirement-70
  test('watcher refreshes are deferred while a run is active and flushed afterward', async function () {
    this.timeout(10_000);
    const root = fs.mkdtempSync(path.join(os.tmpdir(), 'keel-watcher-deferral-'));
    const previousDevWorkspace = process.env.KEEL_VSCODE_BRIDGE_DEV_WORKSPACE;
    process.env.KEEL_VSCODE_BRIDGE_DEV_WORKSPACE = root;
    fs.mkdirSync(path.join(root, '.vscode'), { recursive: true });
    fs.writeFileSync(path.join(root, configRelativePath), JSON.stringify({
      version: currentConfigVersion,
      command: process.execPath,
      args: [path.resolve(__dirname, '../../../src/test/fixtures/fake-adapter.js')],
      displayName: 'Keel'
    }, null, 2) + '\n');

    try {
      const extension = vscode.extensions.getExtension('aggeler.keel-test-bridge');
      assert.ok(extension, 'extension should be discoverable');
      await extension.activate();
      const controller = testControllerForTest();
      assert.ok(controller, 'extension should expose its active TestController for tests');
      const item = controller.createTestItem(`keelWatcher-${Date.now()}`, 'watcher lane');

      setWatcherDebounceMs(25);
      beginActiveRun([item]);
      assert.equal(isRunActive(), true);
      assert.match(activeRunStatusSnapshot().text, /watcher lane/);

      triggerWatcherEventForTest(controller);
      assert.equal(deferredWatcherEventCountForTest(), true);
      assert.equal(isWatcherRefreshPending(), false);

      finishActiveRun();
      assert.equal(isRunActive(), false);
      assert.equal(isWatcherRefreshPending(), true);
      await waitFor(() => !isWatcherRefreshPending(), 2_000);
    } finally {
      setWatcherDebounceMs(300);
      finishActiveRun();
      if (previousDevWorkspace === undefined) {
        delete process.env.KEEL_VSCODE_BRIDGE_DEV_WORKSPACE;
      } else {
        process.env.KEEL_VSCODE_BRIDGE_DEV_WORKSPACE = previousDevWorkspace;
      }
      fs.rmSync(root, { recursive: true, force: true });
    }
  });

  // DHF-TEST: keel/requirement-42, keel/requirement-70
  test('run profile handles desired-state, start, stderr, and reset-result branches', async function () {
    this.timeout(15_000);
    const previousDevWorkspace = process.env.KEEL_VSCODE_BRIDGE_DEV_WORKSPACE;
    const root = fs.mkdtempSync(path.join(os.tmpdir(), 'keel-run-profile-branches-'));
    const fake = path.join(root, 'profile-adapter.cjs');
    fs.mkdirSync(path.join(root, '.vscode'), { recursive: true });
    fs.writeFileSync(fake, [
      "const fs = require('node:fs');",
      "const path = require('node:path');",
      "const args = process.argv.slice(2);",
      "const now = () => new Date().toISOString();",
      "const mode = process.env.KEEL_PROFILE_MODE || 'reset';",
      "const selected = process.env.KEEL_PROFILE_ID || 'case::maintenance::clear';",
      "const sentinel = path.join(process.cwd(), '.devtools', `${mode}.sentinel`);",
      "fs.mkdirSync(path.dirname(sentinel), { recursive: true });",
      "if (args.includes('--version')) {",
      "  if (mode === 'start-fail' && fs.existsSync(sentinel)) { console.log('v0.0.0'); } else { console.log('dev'); }",
      "  process.exit(0);",
      "}",
      "if (args.join(' ') === 'test-bridge tests discover --format json') {",
      "  console.log(JSON.stringify({ version: 1, workspace: process.cwd(), generated_at: now(), capabilities: { clear_results_test_ids: ['case::maintenance::clear'] }, items: [{ id: selected, label: selected, kind: 'maintenance', runnable: true, profiles: ['run'] }] }));",
      "  process.exit(0);",
      "}",
      "if (args.slice(0, 4).join(' ') === 'test-bridge tests desired-state --format') {",
      "  if (mode === 'desired-fail') { console.error('desired-state failed intentionally'); process.exit(3); }",
      "  if (mode === 'start-fail') { fs.writeFileSync(sentinel, 'ready'); }",
      "  console.log(JSON.stringify({ version: 3, workspace: process.cwd(), generated_at: now(), groups: [{ label: 'Empty', order: 1, mutually_exclusive: false, rows: [] }] }));",
      "  process.exit(0);",
      "}",
      "if (args.slice(0, 3).join(' ') === 'test-bridge tests run') {",
      "  process.stderr.write('profile warning on stderr\\n');",
      "  process.stdout.write(`${JSON.stringify({ version: 1, event: 'run_started', time: now(), test_id: selected })}\\n`);",
      "  process.stdout.write(`${JSON.stringify({ version: 1, event: 'passed', time: now(), test_id: selected })}\\n`);",
      "  process.stdout.write(`${JSON.stringify({ version: 1, event: 'run_finished', time: now(), exit_code: 0 })}`);",
      "  process.exit(0);",
      "}",
      "process.exit(2);"
    ].join('\n'));

    const writeConfig = (mode: string, id: string) => {
      fs.writeFileSync(path.join(root, configRelativePath), JSON.stringify({
        version: currentConfigVersion,
        command: process.execPath,
        args: [fake],
        displayName: 'Profile Branches',
        env: { KEEL_PROFILE_MODE: mode, KEEL_PROFILE_ID: id }
      }, null, 2) + '\n');
    };

    try {
      process.env.KEEL_VSCODE_BRIDGE_DEV_WORKSPACE = root;
      const extension = vscode.extensions.getExtension('aggeler.keel-test-bridge');
      assert.ok(extension, 'extension should be discoverable');
      await extension.activate();

      writeConfig('desired-fail', 'case::lane::desired-fail');
      await runProfileHandlerForTest('case::lane::desired-fail');
      assert.equal(isRunActive(), false);

      writeConfig('start-fail', 'case::lane::start-fail');
      fs.rmSync(path.join(root, '.devtools', 'start-fail.sentinel'), { force: true });
      await runProfileHandlerForTest('case::lane::start-fail');
      assert.equal(isRunActive(), false);

      writeConfig('reset', 'case::maintenance::clear');
      await runProfileHandlerForTest('case::maintenance::clear');
      assert.equal(isRunActive(), false);
      assert.ok(publishedTestItemIds().includes('case::maintenance::clear'));
    } finally {
      if (previousDevWorkspace === undefined) {
        delete process.env.KEEL_VSCODE_BRIDGE_DEV_WORKSPACE;
      } else {
        process.env.KEEL_VSCODE_BRIDGE_DEV_WORKSPACE = previousDevWorkspace;
      }
      fs.rmSync(root, { recursive: true, force: true });
    }
  });

  // DHF-TEST: keel/requirement-70
  test('open-workspace refresh does not invalidate terminal results on surviving items', async function () {
    this.timeout(10_000);
    const root = fs.mkdtempSync(path.join(os.tmpdir(), 'keel-refresh-preserve-results-'));
    const previousDevWorkspace = process.env.KEEL_VSCODE_BRIDGE_DEV_WORKSPACE;
    process.env.KEEL_VSCODE_BRIDGE_DEV_WORKSPACE = root;
    fs.mkdirSync(path.join(root, '.vscode'), { recursive: true });
    fs.writeFileSync(path.join(root, configRelativePath), JSON.stringify({
      version: currentConfigVersion,
      command: process.execPath,
      args: [path.resolve(__dirname, '../../../src/test/fixtures/fake-adapter.js')],
      displayName: 'Keel'
    }, null, 2) + '\n');

    const extension = vscode.extensions.getExtension('aggeler.keel-test-bridge');
    assert.ok(extension, 'extension should be discoverable');
    await extension.activate();
    const controller = testControllerForTest();
    assert.ok(controller, 'extension should expose its active TestController for tests');
    const spyTarget = controller as vscode.TestController & { invalidateTestResults: () => void };
    const originalInvalidate = spyTarget.invalidateTestResults.bind(controller);
    let invalidations = 0;
    spyTarget.invalidateTestResults = () => {
      invalidations += 1;
      originalInvalidate();
    };

    try {
      await vscode.commands.executeCommand('keel.tests.refresh');
      assert.equal(invalidations, 0, 'ordinary open-workspace refresh must not reset terminal result icons');
    } finally {
      spyTarget.invalidateTestResults = originalInvalidate;
      if (previousDevWorkspace === undefined) {
        delete process.env.KEEL_VSCODE_BRIDGE_DEV_WORKSPACE;
      } else {
        process.env.KEEL_VSCODE_BRIDGE_DEV_WORKSPACE = previousDevWorkspace;
      }
      fs.rmSync(root, { recursive: true, force: true });
    }
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
    await readDesiredState(root, ['keel::lane::ci']);
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

  // DHF-TEST: keel/requirement-70, keel/requirement-88
  test('tree replacement preserves root, child, alias, and metadata relationships', () => {
    const controller = vscode.tests.createTestController(`keelTreeReplace-${Date.now()}`, 'Keel Tree Replace');
    const root = fs.mkdtempSync(path.join(os.tmpdir(), 'keel-tree-replace-'));
    const tree = publishDiscovery(controller, root, {
      version: 1,
      workspace: root,
      generated_at: new Date().toISOString(),
      items: [
        { id: 'tree::root', label: 'root', kind: 'root', runnable: true, profiles: ['run'], required_resources: ['go'] },
        {
          id: 'tree::child',
          parent_id: 'tree::root',
          label: 'child',
          sort_text: 'b',
          kind: 'test',
          uri: 'child.test.ts',
          range: { start_line: 1, start_column: 2, end_line: 3, end_column: 4 },
          runnable: true,
          profiles: ['run'],
          limitations: ['slow']
        },
        { id: 'tree::alias', parent_id: 'tree::root', label: 'alias', kind: 'test', canonical_id: 'tree::child', runnable: true, profiles: ['run'] }
      ]
    });

    try {
      const replacedChild = replacePublishedTestItem(controller, tree, 'tree::child');
      assert.ok(replacedChild);
      assert.equal(replacedChild.label, 'child');
      assert.equal(replacedChild.sortText, 'b');
      assert.equal(replacedChild.description, 'slow');
      assert.equal(replacedChild.range?.start.line, 1);
      assert.equal(tree.parentByItemId.get('tree::child')?.id, 'tree::root');

      const replacedAlias = replacePublishedTestItem(controller, tree, 'tree::alias');
      assert.ok(replacedAlias);
      assert.equal(tree.aliasesByCanonicalId.get('tree::child')?.[0].id, 'tree::alias');

      const replacedRoot = replacePublishedTestItem(controller, tree, 'tree::root');
      assert.ok(replacedRoot);
      assert.equal(tree.itemsById.get('tree::root')?.id, 'tree::root');
      assert.equal(replacePublishedTestItem(controller, tree, 'tree::missing'), undefined);
    } finally {
      controller.dispose();
      fs.rmSync(root, { recursive: true, force: true });
    }
  });

  // DHF-TEST: keel/requirement-71
  // Verifies: keel/ac-303
  test('lane-run Go package import-path events settle framework package rows', () => {
    const controller = vscode.tests.createTestController(`keelGoPackageSettle-${Date.now()}`, 'Keel Go Package Settle');
    const root = fs.mkdtempSync(path.join(os.tmpdir(), 'keel-go-package-settle-'));
    const items: DiscoveryItem[] = [
      { id: 'keel::frameworks', label: 'D - Frameworks', kind: 'group', runnable: false, profiles: [] },
      { id: 'go::root', parent_id: 'keel::frameworks', label: 'd.1 Go', kind: 'root', framework: 'go', runner: 'go-test', runnable: true, profiles: ['run'] },
      { id: 'go::pkg::log', parent_id: 'go::root', label: 'log', kind: 'package', framework: 'go', runner: 'go-test', runnable: true, profiles: ['run'] },
      { id: 'go::pkg::vscode', parent_id: 'go::root', label: 'vscode', kind: 'package', framework: 'go', runner: 'go-test', runnable: true, profiles: ['run'] }
    ];
    for (let i = 0; i < 9; i += 1) {
      items.push({ id: `go::file::log/file${i}_test.go`, parent_id: 'go::pkg::log', label: `file${i}_test.go`, kind: 'file', framework: 'go', runner: 'go-test', runnable: true, profiles: ['run'] });
    }
    for (let i = 0; i < 14; i += 1) {
      items.push({ id: `go::file::vscode/file${i}_test.go`, parent_id: 'go::pkg::vscode', label: `file${i}_test.go`, kind: 'file', framework: 'go', runner: 'go-test', runnable: true, profiles: ['run'] });
    }
    const tree = publishDiscovery(controller, root, {
      version: 1,
      workspace: 'keel',
      module_path: 'github.com/david-aggeler/keel',
      generated_at: new Date().toISOString(),
      items
    });
    setCurrentTreeForTest(tree);
    try {
      const passed: string[] = [];
      const skipped: string[] = [];
      const outputs: string[] = [];
      const run = {
        started() { /* no-op */ },
        passed(item: vscode.TestItem) { passed.push(item.id); },
        failed() { /* no-op */ },
        errored() { /* no-op */ },
        skipped(item: vscode.TestItem) { skipped.push(item.id); },
        appendOutput(data: string) { outputs.push(data); }
      };
      const selectedItemIds = new Set(['keel::lane::test-coverage']);
      const resultItemIds = new Set<string>();

      applyRunEvent(run as unknown as vscode.TestRun, JSON.stringify(runEvent({
        event: 'passed',
        test_id: 'go::package::github.com/david-aggeler/keel/log'
      })), selectedItemIds, resultItemIds);
      applyRunEvent(run as unknown as vscode.TestRun, JSON.stringify(runEvent({
        event: 'passed',
        test_id: 'go::package::github.com/david-aggeler/keel/vscode'
      })), selectedItemIds, resultItemIds);

      assert.deepEqual(passed.sort(), ['go::pkg::log', 'go::pkg::vscode']);
      assert.deepEqual(skipped, [], 'package terminal events must not demote package rows or their children');
      assert.ok(resultItemIds.has('go::pkg::log'), 'log package row must hold a terminal result');
      assert.ok(resultItemIds.has('go::pkg::vscode'), 'vscode package row must hold a terminal result');
      assert.match(outputs.join(''), /passed go::package::github\.com\/david-aggeler\/keel\/log/);
    } finally {
      setCurrentTreeForTest(undefined);
      controller.dispose();
      fs.rmSync(root, { recursive: true, force: true });
    }
  });

  // DHF-TEST: keel/requirement-71, keel/requirement-88
  test('run-event application covers aliases, siblings, locations, and run control fallbacks', () => {
    const controller = vscode.tests.createTestController(`keelRunEventBranches-${Date.now()}`, 'Keel Run Event Branches');
    const root = fs.mkdtempSync(path.join(os.tmpdir(), 'keel-run-event-branches-'));
    const tree = publishDiscovery(controller, root, {
      version: 1,
      workspace: root,
      module_path: 'github.com/david-aggeler/keel',
      generated_at: new Date().toISOString(),
      capabilities: { clear_results_test_ids: ['case::maintenance::clear'] },
      items: [
        { id: 'case::suite', label: 'suite', kind: 'suite', runnable: true, profiles: ['run'] },
        { id: 'case::test::a', parent_id: 'case::suite', label: 'a', kind: 'test', runnable: true, profiles: ['run'] },
        { id: 'case::test::b', parent_id: 'case::suite', label: 'b', kind: 'test', runnable: true, profiles: ['run'] },
        { id: 'case::alias::a', parent_id: 'case::suite', label: 'alias a', kind: 'test', canonical_id: 'case::test::a', runnable: true, profiles: ['run'] },
        { id: 'go::root', label: 'Go', kind: 'root', framework: 'go', runnable: true, profiles: ['run'] },
        { id: 'go::pkg::log', parent_id: 'go::root', label: 'log', kind: 'package', framework: 'go', runnable: true, profiles: ['run'] },
        { id: 'case::maintenance::clear', label: 'clear results', kind: 'maintenance', runnable: true, profiles: ['run'] }
      ]
    });
    setCurrentTreeForTest(tree);

    const started: string[] = [];
    const passed: string[] = [];
    const failed: Array<{ id: string; message: string; line?: number }> = [];
    const errored: string[] = [];
    const skipped: string[] = [];
    const outputs: string[] = [];
    const coverages: vscode.FileCoverage[] = [];
    const run = {
      started(item: vscode.TestItem) { started.push(item.id); },
      passed(item: vscode.TestItem) { passed.push(item.id); },
      failed(item: vscode.TestItem, message: vscode.TestMessage) {
        failed.push({ id: item.id, message: typeof message.message === 'string' ? message.message : message.message.value, line: message.location?.range.start.line });
      },
      errored(item: vscode.TestItem) { errored.push(item.id); },
      skipped(item: vscode.TestItem) { skipped.push(item.id); },
      appendOutput(data: string) { outputs.push(data); },
      addCoverage(fileCoverage: vscode.FileCoverage) { coverages.push(fileCoverage); },
      end() { outputs.push('ended'); }
    };

    try {
      const selected = new Set(['case::suite']);
      const resultIds = new Set<string>();
      const snapshot = runEventApplicationSnapshot('case::test::a', selected, resultIds);
      assert.deepEqual(snapshot.resultIds.sort(), ['case::alias::a', 'case::test::a']);
      assert.deepEqual(snapshot.skippedSiblingIds.sort(), ['case::alias::a', 'case::test::a', 'case::test::b']);
      assert.deepEqual(runEventApplicationSnapshot('case::test::a', new Set(['case::test::a']), resultIds).neutralAncestorIds, ['case::suite']);
      assert.equal(shouldApplyResultToItem(tree.itemsById.get('case::suite') as vscode.TestItem, new Set(), new Set(['case::test::a', 'case::test::b', 'case::alias::a'])), true);
      assert.deepEqual(resultItemsForRunEvent([tree.itemsById.get('case::test::a') as vscode.TestItem, tree.itemsById.get('case::test::a') as vscode.TestItem]).map((item) => item.id), ['case::test::a']);

      applyRunEvent(run as unknown as vscode.TestRun, 'not-json', selected, resultIds);
      applyRunEvent(run as unknown as vscode.TestRun, JSON.stringify(runEvent({ event: 'test_started', test_id: 'case::test::a' })), selected, resultIds);
      applyRunEvent(run as unknown as vscode.TestRun, JSON.stringify(runEvent({ event: 'passed', test_id: 'case::test::a', duration_ms: 7 })), selected, resultIds);
      applyRunEvent(run as unknown as vscode.TestRun, JSON.stringify(runEvent({
        event: 'failed',
        test_id: 'case::test::b',
        message: 'broken',
        location: { uri: path.join(root, 'case.test.ts'), line: 12, column: 3 }
      })), selected, resultIds);
      applyRunEvent(run as unknown as vscode.TestRun, JSON.stringify(runEvent({ event: 'errored', test_id: 'case::alias::a', message: 'boom' })), selected, resultIds);
      applyRunEvent(run as unknown as vscode.TestRun, JSON.stringify(runEvent({ event: 'cancelled', test_id: 'case::test::b', message: 'stop' })), selected, resultIds);
      applyRunEvent(run as unknown as vscode.TestRun, JSON.stringify(runEvent({ event: 'skipped', test_id: 'case::alias::a', message: 'skip reason' })), selected, resultIds);
      applyRunEvent(run as unknown as vscode.TestRun, JSON.stringify(runEvent({ event: 'output' })), selected, resultIds);
      applyRunEvent(run as unknown as vscode.TestRun, JSON.stringify(runEvent({ event: 'artifact', test_id: 'case::test::a', artifact: { name: 'log', uri: '/tmp/case.log', kind: 'log' } })), selected, resultIds);
      const finished = applyRunEvent(run as unknown as vscode.TestRun, JSON.stringify(runEvent({ event: 'run_finished' })), selected, resultIds);
      const reset = applyRunEvent(run as unknown as vscode.TestRun, JSON.stringify(runEvent({ event: 'passed', test_id: 'case::maintenance::clear' })), new Set(['case::maintenance::clear']), new Set());
      const packageItems = runEventApplicationSnapshot('go::package::github.com/david-aggeler/keel/log', new Set(['go::root']), new Set());

      assert.deepEqual(started.sort(), ['case::alias::a', 'case::test::a']);
      assert.ok(passed.includes('case::test::a'));
      assert.ok(passed.includes('case::alias::a'));
      assert.deepEqual(failed, [{ id: 'case::test::b', message: 'broken', line: 12 }]);
      assert.ok(errored.includes('case::alias::a'));
      assert.ok(skipped.includes('case::test::b'));
      assert.match(outputs.join(''), /not-json/);
      assert.match(outputs.join(''), /skip reason/);
      assert.match(outputs.join(''), /artifact case::test::a: log log/);
      assert.equal(finished.finished, true);
      assert.equal(reset.resetResults, true);
      assert.deepEqual(packageItems.resultIds, ['go::pkg::log']);

      const located = testMessageFromEvent(runEvent({
        event: 'failed',
        message: 'located',
        location: { uri: path.join(root, 'located.test.ts'), line: 5, column: 6 }
      }), 'fallback');
      assert.equal(located.message, 'located');
      assert.equal(located.location?.range.start.line, 5);

      const killed: Array<NodeJS.Signals | number | undefined> = [];
      const child = {
        pid: 99_999_999,
        kill(signal?: NodeJS.Signals | number) {
          killed.push(signal);
          return true;
        }
      };
      assert.equal(signalProcessGroup(child, 'SIGTERM'), true);
      cancelActiveRun(run as unknown as vscode.TestRun, [tree.itemsById.get('case::test::a') as vscode.TestItem], child);
      assert.deepEqual(killed.slice(-2), ['SIGTERM', 'SIGTERM']);

      const rejected: string[] = [];
      const fakeController = {
        createTestRun() {
          return {
            appendOutput(data: string) { rejected.push(data); },
            skipped(item: vscode.TestItem) { rejected.push(`skipped:${item.id}`); },
            end() { rejected.push('end'); }
          };
        }
      };
      rejectConcurrentRun(fakeController as unknown as vscode.TestController, new vscode.TestRunRequest([tree.itemsById.get('case::test::a') as vscode.TestItem]), [tree.itemsById.get('case::test::a') as vscode.TestItem]);
      assert.ok(rejected.some((line) => /already active/.test(line)));
      assert.ok(rejected.includes('skipped:case::test::a'));
      assert.ok(rejected.includes('end'));
      assert.deepEqual(coverages, []);
    } finally {
      setCurrentTreeForTest(undefined);
      controller.dispose();
      fs.rmSync(root, { recursive: true, force: true });
    }
  });

  // DHF-TEST: keel/requirement-42, keel/requirement-62, keel/requirement-65
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

    // requirement-65 (amended): a bare root serves no lanes until detect-lanes
    // runs; the maintenance item seeds .vscode/test-lanes.json with the gate lanes.
    const bareDiscovery = await discoverTests(root);
    assert.ok(bareDiscovery.items.some((item) => item.id === 'keel::maintenance::detect-lanes'));
    assert.ok(!bareDiscovery.items.some((item) => item.id === 'keel::lane::lint'));
    const detect = await collectChild(runTests(root, ['keel::maintenance::detect-lanes']));
    assert.equal(detect.code, 0);

    const discovery = await discoverTests(root);
    assert.ok(discovery.items.some((item) => item.id === 'keel::lane::lint'));

    const desiredState = await readDesiredState(root, ['keel::lane::lint']);
    assert.equal(desiredState.version, 3);
    assert.ok(desiredState.groups.some((group) => group.rows.some((row) => row.run_id === 'keel::desired-state::go-toolchain')));

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
      assert.ok(demoTree.discoveryItemsById.has('keel::maintenance'));
      assert.ok(demoTree.discoveryItemsById.has('keel-demo-dev::lanes'));
      assert.ok(demoTree.discoveryItemsById.has('keel-demo-dev::frameworks'));
      assert.ok(demoTree.discoveryItemsById.has('keel-demo-dev::lane::fake-smoke'));
    } finally {
      demoController.dispose();
    }

    const demoDesiredState = await readDesiredState(root, ['keel-demo-dev::lane::fake-smoke']);
    // cr-79 aligned demo model: the seeded-database row is 'postgres' and, as
    // a reconcilable row, carries a devtool-served run_id (cr-75 contract).
    assert.ok(demoDesiredState.groups.some((group) => group.rows.some((state) => state.resource === 'postgres' && state.desired !== state.current && !!state.run_id)));

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
    // requirement-65 (amended): lanes are detect-produced; seed the dev root's
    // lanes file through the real binary before driving the lint lane.
    const devLanesPath = path.join(devRoot, '.vscode', 'test-lanes.json');
    const previousLanes = fs.existsSync(devLanesPath) ? fs.readFileSync(devLanesPath, 'utf8') : undefined;
    const devDetect = await collectChild(runTests(devRoot, ['keel::maintenance::detect-lanes']));
    assert.equal(devDetect.code, 0);
    const runStreamRoot = devRoot;
    const runsDir = path.join(runStreamRoot, '.devtools', 'vscode-runs');
    fs.rmSync(path.join(runsDir, 'run.lock'), { force: true });
    const beforeRunStreams = new Set(listRunStreams(runsDir));
    try {
      const extension = vscode.extensions.getExtension('aggeler.keel-test-bridge');
      assert.ok(extension, 'extension should be discoverable');
      await extension.activate();
      // DHF-TEST: keel/requirement-60
      await runProfileHandlerForTest('keel::desired-state::keel-module-root');
      const desiredStateTree = currentTree();
      assert.ok(desiredStateTree, 'TestController refresh should publish a tree');
      for (const id of [
        'keel::desired-state::go-toolchain',
        'keel::desired-state::keel-module-root',
        'keel::desired-state::stub-binaries'
      ]) {
        const item = desiredStateTree.discoveryItemsById.get(id);
        assert.ok(item, `real keel-dev discovery should serve desired-state row ${id}`);
        assert.equal(item.parent_id, 'keel::desired-state::group::test-preconditions');
        assert.equal(item.runnable, true, `${id} must be runnable, not informational`);
        assert.deepEqual(item.profiles, ['run']);
      }

      const afterDesiredStateStreams = listRunStreams(runsDir).filter((candidate) => !beforeRunStreams.has(candidate));
      assert.equal(afterDesiredStateStreams.length, 1, `desired-state TestController run should create one external run stream under ${runStreamRoot}`);
      const desiredStateEvents = parseRunEvents(fs.readFileSync(afterDesiredStateStreams[0], 'utf8'));
      assert.ok(desiredStateEvents.some((event) => event.event === 'passed' && event.test_id === 'keel::desired-state::keel-module-root'));
      assert.equal(desiredStateEvents.filter((event) => event.event === 'run_finished').length, 1);
      assert.doesNotMatch(fs.readFileSync(afterDesiredStateStreams[0], 'utf8'), /Selection contains only informational desired-state rows/);

      const beforeLintRunStreams = new Set(listRunStreams(runsDir));
      await runProfileHandlerForTest('keel::lane::lint');

      const newStreams = listRunStreams(runsDir).filter((candidate) => !beforeLintRunStreams.has(candidate));
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
      if (previousLanes === undefined) {
        fs.rmSync(devLanesPath, { force: true });
      } else {
        fs.writeFileSync(devLanesPath, previousLanes);
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
  test('external run mirror does not re-red an item from historical completed failed streams', async function () {
    this.timeout(10_000);
    const root = fs.mkdtempSync(path.join(os.tmpdir(), 'keel-external-stale-import-'));
    const previousDevWorkspace = process.env.KEEL_VSCODE_BRIDGE_DEV_WORKSPACE;
    process.env.KEEL_VSCODE_BRIDGE_DEV_WORKSPACE = root;
    const controller = vscode.tests.createTestController(`keelStaleImport-${Date.now()}`, 'Keel Stale Import');
    const tree = publishDiscovery(controller, root, {
      version: 1,
      workspace: root,
      generated_at: new Date().toISOString(),
      items: [{ id: 'keel::lane::test-fast', label: 'test-fast', kind: 'lane', runnable: true, profiles: ['run'] }]
    });
    setCurrentTreeForTest(tree);
    const passed: string[] = [];
    const failed: string[] = [];
    const errored: string[] = [];
    const spyTarget = controller as vscode.TestController & {
      createTestRun: (request: vscode.TestRunRequest, name?: string, persist?: boolean) => vscode.TestRun;
    };
    const originalCreateTestRun = spyTarget.createTestRun.bind(controller);
    spyTarget.createTestRun = (request: vscode.TestRunRequest, name?: string, persist?: boolean): vscode.TestRun => {
      const run = originalCreateTestRun(request, name, persist);
      const originalPassed = run.passed.bind(run);
      const originalFailed = run.failed.bind(run);
      const originalErrored = run.errored.bind(run);
      run.passed = (item: vscode.TestItem, duration?: number) => {
        passed.push(item.id);
        originalPassed(item, duration);
      };
      run.failed = (item: vscode.TestItem, message: vscode.TestMessage | readonly vscode.TestMessage[], duration?: number) => {
        failed.push(item.id);
        originalFailed(item, message, duration);
      };
      run.errored = (item: vscode.TestItem, message: vscode.TestMessage | readonly vscode.TestMessage[], duration?: number) => {
        errored.push(item.id);
        originalErrored(item, message, duration);
      };
      return run;
    };
    const mirror = new ExternalRunMirror(controller);
    const runsDir = path.join(root, '.devtools', 'vscode-runs');
    fs.mkdirSync(runsDir, { recursive: true });
    const currentRunFile = path.join(runsDir, '001-current-passed.jsonl');
    fs.writeFileSync(currentRunFile, [
      JSON.stringify(runEvent({ event: 'run_started', run_id: 'current-pass', test_id: 'keel::lane::test-fast' })),
      JSON.stringify(runEvent({ event: 'test_started', run_id: 'current-pass', test_id: 'keel::lane::test-fast' })),
      JSON.stringify(runEvent({ event: 'passed', run_id: 'current-pass', test_id: 'keel::lane::test-fast' })),
      JSON.stringify(runEvent({ event: 'run_finished', run_id: 'current-pass', exit_code: 0 }))
    ].join('\n') + '\n');
    const historicalRunFile = path.join(runsDir, '999-historical-failed.jsonl');
    fs.writeFileSync(historicalRunFile, [
      JSON.stringify(runEvent({ event: 'run_started', run_id: 'historical-fail', test_id: 'keel::lane::test-fast' })),
      JSON.stringify(runEvent({ event: 'test_started', run_id: 'historical-fail', test_id: 'keel::lane::test-fast' })),
      JSON.stringify(runEvent({ event: 'failed', run_id: 'historical-fail', test_id: 'keel::lane::test-fast', message: 'old failure' })),
      JSON.stringify(runEvent({ event: 'run_finished', run_id: 'historical-fail', exit_code: 1 }))
    ].join('\n') + '\n');
    const oldTime = new Date(Date.now() - 2 * 60 * 60 * 1000);
    fs.utimesSync(historicalRunFile, oldTime, oldTime);

    try {
      await mirror.syncWorkspace();
      assert.ok(passed.includes('keel::lane::test-fast'), 'current completed pass is still imported');
      assert.ok(!failed.includes('keel::lane::test-fast'), 'historical completed failure must not re-red the item');
      assert.ok(!errored.includes('keel::lane::test-fast'), 'historical completed error must not re-red the item');
    } finally {
      spyTarget.createTestRun = originalCreateTestRun;
      fs.rmSync(root, { recursive: true, force: true });
      mirror.dispose();
      setCurrentTreeForTest(undefined);
      controller.dispose();
      if (previousDevWorkspace === undefined) {
        delete process.env.KEEL_VSCODE_BRIDGE_DEV_WORKSPACE;
      } else {
        process.env.KEEL_VSCODE_BRIDGE_DEV_WORKSPACE = previousDevWorkspace;
      }
    }
  });

  // DHF-TEST: keel/requirement-88
  test('external run mirror invalidates exclusive-group siblings cleared by imported streams', async function () {
    this.timeout(10_000);
    const root = fs.mkdtempSync(path.join(os.tmpdir(), 'keel-external-exclusive-clear-'));
    const previousDevWorkspace = process.env.KEEL_VSCODE_BRIDGE_DEV_WORKSPACE;
    process.env.KEEL_VSCODE_BRIDGE_DEV_WORKSPACE = root;
    const controller = vscode.tests.createTestController(`keelExternalExclusiveClear-${Date.now()}`, 'Keel External Exclusive Clear');
    const tree = publishDiscovery(controller, root, {
      version: 1,
      workspace: root,
      generated_at: new Date().toISOString(),
      items: [
        { id: 'demo::desired-state::dataset', label: 'Data Set', kind: 'group', runnable: false, profiles: [] },
        { id: 'demo::desired-state::dataset::small', parent_id: 'demo::desired-state::dataset', label: 'small', kind: 'test', runnable: true, profiles: ['run'] },
        { id: 'demo::desired-state::dataset::full', parent_id: 'demo::desired-state::dataset', label: 'full', kind: 'test', runnable: true, profiles: ['run'] }
      ]
    });
    setCurrentTreeForTest(tree);
    const mirror = new ExternalRunMirror(controller);
    const runsDir = path.join(root, '.devtools', 'vscode-runs');
    fs.mkdirSync(runsDir, { recursive: true });
    const runFile = path.join(runsDir, `exclusive-clear-${process.pid}-${Date.now()}.jsonl`);
    fs.writeFileSync(runFile, [
      JSON.stringify(runEvent({ event: 'run_started', run_id: 'external-exclusive', test_id: 'demo::desired-state::dataset::full' })),
      JSON.stringify(runEvent({ event: 'test_started', run_id: 'external-exclusive', test_id: 'demo::desired-state::dataset::full' })),
      JSON.stringify(runEvent({ event: 'passed', run_id: 'external-exclusive', test_id: 'demo::desired-state::dataset::full' })),
      JSON.stringify(runEvent({
        event: 'cleared',
        run_id: 'external-exclusive',
        test_id: 'demo::desired-state::dataset::small',
        message: 'small deactivated by exclusive desired-state selection'
      })),
      JSON.stringify(runEvent({ event: 'run_finished', run_id: 'external-exclusive', exit_code: 0 }))
    ].join('\n') + '\n');

    const spyTarget = controller as vscode.TestController & { invalidateTestResults: (items?: vscode.TestItem | readonly vscode.TestItem[]) => void };
    const originalInvalidate = spyTarget.invalidateTestResults.bind(controller);
    const invalidated: string[] = [];
    spyTarget.invalidateTestResults = (items?: vscode.TestItem | readonly vscode.TestItem[]) => {
      if (Array.isArray(items)) {
        for (const item of items as readonly vscode.TestItem[]) {
          invalidated.push(item.id);
        }
      } else if (items) {
        invalidated.push((items as vscode.TestItem).id);
      }
      originalInvalidate(items as never);
    };

    try {
      await mirror.syncWorkspace();
      assert.deepEqual(invalidated, ['demo::desired-state::dataset::small'], 'imported cleared sibling result is invalidated on the controller');
      const snapshot = mirror.snapshots().find((candidate) => candidate.runId === 'external-exclusive');
      assert.ok(snapshot?.finished, 'completed external stream is imported as finished');
      assert.deepEqual(snapshot.resultIds, ['demo::desired-state::dataset::full'], 'only the selected member keeps a displayed result');
    } finally {
      spyTarget.invalidateTestResults = originalInvalidate;
      fs.rmSync(root, { recursive: true, force: true });
      mirror.dispose();
      setCurrentTreeForTest(undefined);
      controller.dispose();
      if (previousDevWorkspace === undefined) {
        delete process.env.KEEL_VSCODE_BRIDGE_DEV_WORKSPACE;
      } else {
        process.env.KEEL_VSCODE_BRIDGE_DEV_WORKSPACE = previousDevWorkspace;
      }
    }
  });

  // DHF-TEST: keel/requirement-93
  test('external run mirror does not replay a completed stream while desired-state refresh is awaiting', async function () {
    this.timeout(10_000);
    const root = fs.mkdtempSync(path.join(os.tmpdir(), 'keel-external-refresh-cursor-'));
    const previousDevWorkspace = process.env.KEEL_VSCODE_BRIDGE_DEV_WORKSPACE;
    process.env.KEEL_VSCODE_BRIDGE_DEV_WORKSPACE = root;
    fs.mkdirSync(path.join(root, '.vscode'), { recursive: true });
    const adapter = path.join(root, 'blocking-adapter.js');
    const marker = path.join(root, '.devtools', 'desired-state-called');
    const release = path.join(root, '.devtools', 'release-desired-state');
    fs.writeFileSync(adapter, `
const fs = require('node:fs');
const path = require('node:path');
const args = process.argv.slice(2);
const now = () => new Date().toISOString();
const marker = ${JSON.stringify(marker)};
const release = ${JSON.stringify(release)};
const item = { id: 'demo::desired-state::dataset::full', label: 'full', kind: 'test', runnable: true, profiles: ['run'] };
function writeDiscovery() {
  process.stdout.write(JSON.stringify({ version: 1, workspace: process.cwd(), generated_at: now(), items: [item] }) + '\\n');
}
function writeDesiredState() {
  process.stdout.write(JSON.stringify({
    version: 3,
    workspace: process.cwd(),
    generated_at: now(),
    groups: [{ label: 'Data Set', order: 1, mutually_exclusive: true, rows: [{
      run_id: item.id,
      resource: 'full',
      kind: 'dataset',
      desired: 'full',
      current: 'full',
      status: 'satisfied',
      action: 'reuse',
      message: 'full',
      reusable: true,
      owned: false,
      active: true
    }] }]
  }) + '\\n');
}
if (args.includes('--version')) {
  process.stdout.write('dev\\n');
  process.exit(0);
}
if (args.slice(0, 4).join(' ') === 'test-bridge tests discover --format') {
  writeDiscovery();
  process.exit(0);
}
if (args.slice(0, 4).join(' ') === 'test-bridge tests desired-state --format') {
  fs.mkdirSync(path.dirname(marker), { recursive: true });
  fs.writeFileSync(marker, args.join(' ') + '\\n');
  const wait = () => {
    if (fs.existsSync(release)) {
      writeDesiredState();
      process.exit(0);
    }
    setTimeout(wait, 10);
  };
  wait();
} else {
  process.stderr.write('unsupported command ' + args.join(' ') + '\\n');
  process.exit(2);
}
`);
    fs.writeFileSync(path.join(root, configRelativePath), JSON.stringify({
      version: currentConfigVersion,
      command: process.execPath,
      args: [adapter],
      displayName: 'Keel'
    }, null, 2) + '\n');

    const controller = vscode.tests.createTestController(`keelExternalRefreshCursor-${Date.now()}`, 'Keel External Refresh Cursor');
    const tree = publishDiscovery(controller, root, {
      version: 1,
      workspace: root,
      generated_at: new Date().toISOString(),
      items: [{ id: 'demo::desired-state::dataset::full', label: 'full', kind: 'test', runnable: true, profiles: ['run'] }]
    });
    setCurrentTreeForTest(tree);
    const passed: string[] = [];
    let endCount = 0;
    const spyTarget = controller as vscode.TestController & {
      createTestRun: (request: vscode.TestRunRequest, name?: string, persist?: boolean) => vscode.TestRun;
    };
    const originalCreateTestRun = spyTarget.createTestRun.bind(controller);
    spyTarget.createTestRun = (request: vscode.TestRunRequest, name?: string, persist?: boolean): vscode.TestRun => {
      const run = originalCreateTestRun(request, name, persist);
      const originalPassed = run.passed.bind(run);
      const originalEnd = run.end.bind(run);
      run.passed = (item: vscode.TestItem, duration?: number) => {
        passed.push(item.id);
        originalPassed(item, duration);
      };
      run.end = () => {
        endCount += 1;
        originalEnd();
      };
      return run;
    };
    const mirror = new ExternalRunMirror(controller);
    const runsDir = path.join(root, '.devtools', 'vscode-runs');
    fs.mkdirSync(runsDir, { recursive: true });
    fs.writeFileSync(path.join(runsDir, 'cursor-race.jsonl'), [
      JSON.stringify(runEvent({ event: 'run_started', run_id: 'cursor-race', test_id: 'demo::desired-state::dataset::full' })),
      JSON.stringify(runEvent({ event: 'test_started', run_id: 'cursor-race', test_id: 'demo::desired-state::dataset::full' })),
      JSON.stringify(runEvent({ event: 'passed', run_id: 'cursor-race', test_id: 'demo::desired-state::dataset::full' })),
      JSON.stringify(runEvent({ event: 'run_finished', run_id: 'cursor-race', exit_code: 0 }))
    ].join('\n') + '\n');

    try {
      const firstSync = mirror.syncWorkspace();
      await waitFor(() => fs.existsSync(marker));
      const secondSync = mirror.syncWorkspace();
      fs.writeFileSync(release, 'ok\n');
      await Promise.all([firstSync, secondSync]);

      assert.deepEqual(passed, ['demo::desired-state::dataset::full'], 'completed stream is applied once while post-run refresh is awaiting');
      assert.equal(endCount, 1, 'completed stream ends exactly one TestRun');
    } finally {
      spyTarget.createTestRun = originalCreateTestRun;
      fs.rmSync(root, { recursive: true, force: true });
      mirror.dispose();
      setCurrentTreeForTest(undefined);
      controller.dispose();
      if (previousDevWorkspace === undefined) {
        delete process.env.KEEL_VSCODE_BRIDGE_DEV_WORKSPACE;
      } else {
        process.env.KEEL_VSCODE_BRIDGE_DEV_WORKSPACE = previousDevWorkspace;
      }
    }
  });

  // DHF-TEST: keel/requirement-93
  test('external run mirror refreshes desired state for observed terminal result ids', async function () {
    this.timeout(10_000);
    const root = fs.mkdtempSync(path.join(os.tmpdir(), 'keel-external-refresh-result-ids-'));
    const previousDevWorkspace = process.env.KEEL_VSCODE_BRIDGE_DEV_WORKSPACE;
    process.env.KEEL_VSCODE_BRIDGE_DEV_WORKSPACE = root;
    fs.mkdirSync(path.join(root, '.vscode'), { recursive: true });
    const callsPath = path.join(root, '.devtools', 'adapter-calls.log');
    const adapter = path.join(root, 'result-id-adapter.js');
    fs.writeFileSync(adapter, `
const fs = require('node:fs');
const path = require('node:path');
const args = process.argv.slice(2);
const now = () => new Date().toISOString();
const callsPath = ${JSON.stringify(callsPath)};
const fullId = 'demo::desired-state::dataset::full';
fs.mkdirSync(path.dirname(callsPath), { recursive: true });
fs.appendFileSync(callsPath, args.join(' ') + '\\n');
if (args.includes('--version')) {
  process.stdout.write('dev\\n');
  process.exit(0);
}
if (args.slice(0, 4).join(' ') === 'test-bridge tests discover --format') {
  process.stdout.write(JSON.stringify({
    version: 1,
    workspace: process.cwd(),
    generated_at: now(),
    items: [{ id: fullId, label: 'full', kind: 'test', runnable: true, profiles: ['run'] }]
  }) + '\\n');
  process.exit(0);
}
if (args.slice(0, 4).join(' ') === 'test-bridge tests desired-state --format') {
  process.stdout.write(JSON.stringify({
    version: 3,
    workspace: process.cwd(),
    generated_at: now(),
    groups: [{ label: 'Data Set', order: 1, mutually_exclusive: true, rows: [{
      run_id: fullId,
      resource: 'full',
      kind: 'dataset',
      desired: 'full',
      current: 'full',
      status: 'satisfied',
      action: 'reuse',
      message: 'full',
      reusable: true,
      owned: false,
      active: true
    }] }]
  }) + '\\n');
  process.exit(0);
}
process.stderr.write('unsupported command ' + args.join(' ') + '\\n');
process.exit(2);
`);
    fs.writeFileSync(path.join(root, configRelativePath), JSON.stringify({
      version: currentConfigVersion,
      command: process.execPath,
      args: [adapter],
      displayName: 'Keel'
    }, null, 2) + '\n');

    const controller = vscode.tests.createTestController(`keelExternalRefreshResultIds-${Date.now()}`, 'Keel External Refresh Result IDs');
    const tree = publishDiscovery(controller, root, {
      version: 1,
      workspace: root,
      generated_at: new Date().toISOString(),
      items: [{ id: 'demo::desired-state::dataset::full', label: 'full', kind: 'test', runnable: true, profiles: ['run'] }]
    });
    setCurrentTreeForTest(tree);
    const mirror = new ExternalRunMirror(controller);
    const runsDir = path.join(root, '.devtools', 'vscode-runs');
    fs.mkdirSync(runsDir, { recursive: true });
    fs.writeFileSync(path.join(runsDir, 'terminal-result-id.jsonl'), [
      JSON.stringify(runEvent({ event: 'run_started', run_id: 'terminal-result-id' })),
      JSON.stringify(runEvent({ event: 'test_started', run_id: 'terminal-result-id', test_id: 'demo::desired-state::dataset::full' })),
      JSON.stringify(runEvent({ event: 'passed', run_id: 'terminal-result-id', test_id: 'demo::desired-state::dataset::full' })),
      JSON.stringify(runEvent({ event: 'run_finished', run_id: 'terminal-result-id', exit_code: 0 }))
    ].join('\n') + '\n');

    try {
      await mirror.syncWorkspace();
      const calls = fs.readFileSync(callsPath, 'utf8').trim().split(/\r?\n/);
      assert.ok(
        calls.includes('test-bridge tests desired-state --format json --id demo::desired-state::dataset::full'),
        `desired-state refresh calls should include terminal result id; calls=${calls.join(' | ')}`
      );
    } finally {
      fs.rmSync(root, { recursive: true, force: true });
      mirror.dispose();
      setCurrentTreeForTest(undefined);
      controller.dispose();
      if (previousDevWorkspace === undefined) {
        delete process.env.KEEL_VSCODE_BRIDGE_DEV_WORKSPACE;
      } else {
        process.env.KEEL_VSCODE_BRIDGE_DEV_WORKSPACE = previousDevWorkspace;
      }
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

      applyRunEvent(run as vscode.TestRun, JSON.stringify(runEvent({
        event: 'artifact',
        test_id: 'keel::lane::test-coverage',
        artifact: { name: 'coverage profile', uri: 'not a uri', kind: 'coverage' }
      })), new Set(), new Set(), { coverage: true, workspaceRoot: root, modulePath: 'github.com/david-aggeler/keel' });
      assert.match(outputs.join(''), /coverage artifact URI is not a file URI/);

      applyRunEvent(run as vscode.TestRun, JSON.stringify(event), new Set(), new Set(), { coverage: true, workspaceRoot: root });
      assert.match(outputs.join(''), /coverage artifact cannot be applied because discovery did not provide module_path/);

      applyRunEvent(run as vscode.TestRun, JSON.stringify(runEvent({
        event: 'artifact',
        message: 'artifact metadata omitted'
      })), new Set(), new Set(), { coverage: true, workspaceRoot: root, modulePath: 'github.com/david-aggeler/keel' });
      assert.match(outputs.join(''), /artifact metadata omitted/);

      fs.rmSync(profile, { force: true });
      applyRunEvent(run as vscode.TestRun, JSON.stringify(event), new Set(), new Set(), { coverage: true, workspaceRoot: root, modulePath: 'github.com/david-aggeler/keel' });
      assert.match(outputs.join(''), /coverage artifact is no longer available/);
    } finally {
      fs.rmSync(profile, { force: true });
    }
  });

  // DHF-TEST: keel/requirement-88
  test('exclusive-group cleared events leave siblings with no result (not skipped) and invalidate them', () => {
    const controller = vscode.tests.createTestController(`keelExclusiveClear-${Date.now()}`, 'Keel Exclusive Clear');
    const tree = publishDiscovery(controller, process.cwd(), {
      version: 1,
      workspace: process.cwd(),
      generated_at: new Date().toISOString(),
      items: [
        { id: 'demo::desired-state::dataset', label: 'Data Set', kind: 'group', runnable: false, profiles: [] },
        { id: 'demo::desired-state::dataset::small', parent_id: 'demo::desired-state::dataset', label: 'small', kind: 'test', runnable: true, profiles: ['run'] },
        { id: 'demo::desired-state::dataset::full', parent_id: 'demo::desired-state::dataset', label: 'full', kind: 'test', runnable: true, profiles: ['run'] }
      ]
    });
    setCurrentTreeForTest(tree);
    try {
      const passed: string[] = [];
      const skipped: string[] = [];
      const failed: string[] = [];
      const errored: string[] = [];
      const outputs: string[] = [];
      const run = {
        started() { /* no-op */ },
        passed(item: vscode.TestItem) { passed.push(item.id); },
        skipped(item: vscode.TestItem) { skipped.push(item.id); },
        failed(item: vscode.TestItem) { failed.push(item.id); },
        errored(item: vscode.TestItem) { errored.push(item.id); },
        appendOutput(data: string) { outputs.push(data); }
      };

      const selectedItemIds = new Set(['demo::desired-state::dataset::full']);
      const resultItemIds = new Set<string>();

      // Activate the concrete member 'full'.
      applyRunEvent(run as unknown as vscode.TestRun, JSON.stringify(runEvent({
        event: 'passed', test_id: 'demo::desired-state::dataset::full'
      })), selectedItemIds, resultItemIds);

      // The bridge deactivates sibling 'small' with a 'cleared' event.
      const applied = applyRunEvent(run as unknown as vscode.TestRun, JSON.stringify(runEvent({
        event: 'cleared', test_id: 'demo::desired-state::dataset::small', message: 'deactivated by exclusive desired-state selection'
      })), selectedItemIds, resultItemIds);

      assert.deepEqual(passed, ['demo::desired-state::dataset::full'], 'selected member shows a result');
      assert.ok(!skipped.includes('demo::desired-state::dataset::small'), 'deactivated sibling must NOT get a terminal skipped result');
      assert.ok(!failed.includes('demo::desired-state::dataset::small') && !errored.includes('demo::desired-state::dataset::small'), 'deactivated sibling gets no terminal result at all');
      assert.deepEqual(applied.clearedResultIds, ['demo::desired-state::dataset::small'], 'cleared event surfaces the sibling id for invalidation');
      assert.ok(!resultItemIds.has('demo::desired-state::dataset::small'), 'cleared sibling holds no result');
      assert.ok(resultItemIds.has('demo::desired-state::dataset::full'), 'selected member retains its result');

      // The run loop invalidates cleared items on the controller so any stale
      // result from a prior run drops to no-result — scoped to the siblings,
      // never the member left active.
      const spyTarget = controller as vscode.TestController & { invalidateTestResults: (items?: vscode.TestItem | readonly vscode.TestItem[]) => void };
      const original = spyTarget.invalidateTestResults.bind(controller);
      const invalidated: string[] = [];
      spyTarget.invalidateTestResults = (items?: vscode.TestItem | readonly vscode.TestItem[]) => {
        if (Array.isArray(items)) {
          for (const it of items) {
            invalidated.push(it.id);
          }
        }
        original(items as never);
      };
      try {
        invalidateClearedResults(controller, new Set(['demo::desired-state::dataset::small']));
      } finally {
        spyTarget.invalidateTestResults = original;
      }
      assert.deepEqual(invalidated, ['demo::desired-state::dataset::small'], 'cleared sibling result is invalidated on the controller');
    } finally {
      setCurrentTreeForTest(undefined);
      controller.dispose();
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
