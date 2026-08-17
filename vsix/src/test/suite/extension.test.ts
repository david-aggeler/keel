import * as assert from 'node:assert/strict';
import * as cp from 'node:child_process';
import * as fs from 'node:fs';
import * as os from 'node:os';
import * as path from 'node:path';
import { EventEmitter } from 'node:events';
import { PassThrough } from 'node:stream';
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
  exclusiveGroupPeerItems,
  ExternalRunMirror,
  finishActiveRun,
  isRunActive,
  isWatcherRefreshPending,
  parseGoCoverageProfile,
  publishedTestItemIds,
  rejectConcurrentRun,
  resetReconcileSignatureForTest,
  resultItemsForRunEvent,
  runEventApplicationSnapshot,
  runProfileHandlerForTest,
  runStartInvalidationRunName,
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
import * as bridgeAdapterModule from '../../bridgeAdapter';
import { adapterConfig, configRelativePath, currentConfigVersion, defaultConfigTemplate, discoveryOutputMaxBufferBytes, discoverTests, readDesiredState, readAdapterConfig, runTests, upgradeConfig } from '../../bridgeAdapter';
import { publishDiscovery, replacePublishedTestItem } from '../../tree';
import { DesiredStateGroup, DiscoveryDocument, DiscoveryItem, RunEvent } from '../../protocol';

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

  // The pair is asserted as a *relation*, never as two literals. Two literals only
  // prove the lines agree with each other, which stays green through any coordinated
  // edit — the blind spot keel/issue-147 records.
  // DHF-TEST: keel/requirement-119 (keel/ac-458, keel/ac-459, keel/ac-466)
  test('the declared runtime Node major is cited, and @types/node stays under it', () => {
    const manifestPath = path.resolve(__dirname, '../../../package.json');
    const manifest = JSON.parse(fs.readFileSync(manifestPath, 'utf8')) as {
      engines: { vscode: string };
      devDependencies: Record<string, string>;
    };
    const supportPolicy = fs.readFileSync(path.resolve(__dirname, '../../../SUPPORTED_VSCODE.md'), 'utf8');

    assert.equal(manifest.engines.vscode, '^1.125.0');
    assert.equal(manifest.devDependencies['@types/vscode'], '1.125.0');

    const declaredMajor = Number(/^VS Code runtime Node major: (\d+)$/m.exec(supportPolicy)?.[1]);
    const citation = /^VS Code runtime Node major source: (.+)$/m.exec(supportPolicy)?.[1] ?? '';
    const pinnedMajor = Number(/^\^?(\d+)\./.exec(manifest.devDependencies['@types/node'] ?? '')?.[1]);

    assert.ok(Number.isInteger(declaredMajor), 'the policy declares a runtime Node major');
    assert.ok(Number.isInteger(pinnedMajor), '@types/node is pinned to a readable major');
    assert.ok(
      pinnedMajor <= declaredMajor,
      `@types/node major ${pinnedMajor} must not exceed the declared runtime major ${declaredMajor}`
    );

    // keel/ac-466: the reference value is re-derivable from outside this repository.
    assert.ok(citation.includes(manifest.engines.vscode.replace('^', '')), 'the citation names the declared VS Code release');
    assert.match(citation, /Electron \d+\.\d+\.\d+/);
    assert.match(citation, new RegExp(`Node(?:\\.js)? ${declaredMajor}\\.\\d+\\.\\d+`));
    assert.ok(citation.includes('https://'), 'the citation names an external source');

    // keel/ac-459: a hold is legitimate, an unexplained hold is not.
    if (/is held at/.test(supportPolicy)) {
      assert.match(supportPolicy, /Reason:/);
      assert.match(supportPolicy, /Release condition:/);
    }
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
      "if (args.join(' ') === 'test-bridge discover --format json') {",
      "  console.log(JSON.stringify({ version: 1, workspace: seen, generated_at: now(), items: [] }));",
      "  process.exit(0);",
      "}",
      "if (args.slice(0, 2).join(' ') === 'test-bridge run') {",
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
        "if (args.join(' ') === 'test-bridge discover --format json') { console.log(JSON.stringify({ version: 2, items: null })); process.exit(0); }",
        "if (args.slice(0, 3).join(' ') === 'test-bridge desired-state --format') { console.log(JSON.stringify({ version: 1, groups: null })); process.exit(0); }",
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

  // DHF-TEST: keel/requirement-64, keel/requirement-66, keel/requirement-110
  test('adapter fails loud on released VSIX and devtool version skew before discovery', async function () {
    const manifest = JSON.parse(fs.readFileSync(path.resolve(__dirname, '../../../package.json'), 'utf8')) as { version: string };
    for (const [devtoolVersion, expectedDisplay] of [['v0.0.0', 'v0.0.0'], ['0.0.0+deadbeef', 'v0.0.0']]) {
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
            assert.match(message, new RegExp(`devtool ${expectedDisplay.replaceAll('.', '\\.')}`));
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

  // DHF-TEST: keel/requirement-115
  test('discovery and desired-state reads use one exported document-size bound', () => {
    assert.equal(discoveryOutputMaxBufferBytes, 16 * 1024 * 1024);
    const source = fs.readFileSync(path.resolve(__dirname, '../../../src/bridgeAdapter.ts'), 'utf8');
    assert.equal((source.match(/export const discoveryOutputMaxBufferBytes/g) ?? []).length, 1);
    assert.equal((source.match(/maxBuffer: discoveryOutputMaxBufferBytes/g) ?? []).length, 1);
    assert.doesNotMatch(source, /maxBuffer:\s*16\s*\*\s*1024\s*\*\s*1024/);
  });

  // DHF-TEST: keel/requirement-115
  test('oversized discovery output is diagnosed with the published byte bound', async function () {
    this.timeout(10_000);
    const testBound = 64;
    const restoreBound = setDiscoveryOutputMaxBufferBytesForTest(testBound);
    const root = fs.mkdtempSync(path.join(os.tmpdir(), 'keel-discovery-size-bound-'));
    const fake = path.join(root, 'oversized-adapter.cjs');
    fs.mkdirSync(path.join(root, '.vscode'), { recursive: true });
    fs.writeFileSync(fake, [
      "const args = process.argv.slice(2);",
      "if (args.includes('--version')) { console.log('dev'); process.exit(0); }",
      "if (args.join(' ') === 'test-bridge discover --format json') {",
      `  process.stdout.write('x'.repeat(${testBound + 1}));`,
      "  process.exit(0);",
      "}",
      "process.exit(2);"
    ].join('\n'));
    fs.writeFileSync(path.join(root, configRelativePath), JSON.stringify({
      version: currentConfigVersion,
      command: nodeExecutableForTest(),
      args: [fake],
      displayName: 'Oversized Producer'
    }, null, 2) + '\n');

    try {
      await assert.rejects(
        discoverTests(root),
        (err: unknown) => {
          const message = err instanceof Error ? err.message : String(err);
          assert.match(message, new RegExp(`${testBound}`));
          assert.match(message, /producer/i);
          assert.match(message, /document size/i);
          assert.doesNotMatch(message, /stdout maxBuffer length exceeded/);
          assert.doesNotMatch(message, /just build-dev/i);
          return true;
        }
      );
    } finally {
      restoreBound();
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
      'test-bridge config-upgrade',
      'test-bridge discover --format json',
      'test-bridge desired-state --format json --id keel::lane::ci',
      // The run argv declares the initiating surface so the spooled stream
      // carries it and the external-run mirror can skip its own run.
      // DHF-TEST: keel/requirement-36
      'test-bridge run --source editor --id keel::lane::ci'
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
        callsAfterRunnable.filter((call) => call === 'test-bridge desired-state --format json').length,
        0,
        'refresh must render Desired State from discovery without a live empty-selection probe'
      );
      assert.ok(callsAfterRunnable.includes(`test-bridge desired-state --format json --id ${servedRunID}`));
      assert.ok(callsAfterRunnable.includes(`test-bridge run --source editor --id ${servedRunID}`));

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
        calls.filter((call) => call === `test-bridge desired-state --format json --id ${runID}`).length,
        2,
        'run-finished must re-query desired-state after the devtool changes the selected row'
      );
      const refreshed = currentTree()?.discoveryItemsById.get(runID);
      assert.ok(refreshed, 'post-run refresh should keep the selected desired-state row published');
      assert.equal(refreshed.desired_state_row?.active, true, `post-run row facts = ${JSON.stringify(refreshed.desired_state_row)}`);
      assert.equal(refreshed.desired_state_row?.action, 'reuse', `post-run row facts = ${JSON.stringify(refreshed.desired_state_row)}`);
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
      { id: 'demo::desired-state::dataset', label: 'Data Set', kind: 'group', runnable: false, profiles: [], desired_state_group: { mutually_exclusive: true } },
      { id: 'demo::desired-state::dataset::small', parent_id: 'demo::desired-state::dataset', label: 'small', kind: 'test', runnable: true, profiles: ['run'], desired_state_row: { current: 'small', action: 'reuse', active: active === 'demo::desired-state::dataset::small' } },
      { id: 'demo::desired-state::dataset::full', parent_id: 'demo::desired-state::dataset', label: 'full', kind: 'test', runnable: true, profiles: ['run'], desired_state_row: { current: 'full', action: 'reuse', active: active === 'demo::desired-state::dataset::full' } },
      { id: 'demo::desired-state::dataset::unknown', parent_id: 'demo::desired-state::dataset', label: 'Unknown State', kind: 'test', runnable: true, profiles: ['run'], desired_state_row: { current: 'unknown', action: 'reuse', active: active === 'demo::desired-state::dataset::unknown' } }
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
if (args.slice(0, 3).join(' ') === 'test-bridge discover --format') {
  process.stdout.write(JSON.stringify(discovery()) + '\\n');
  process.exit(0);
}
if (args.slice(0, 3).join(' ') === 'test-bridge desired-state --format') {
  process.stdout.write(JSON.stringify(desiredState()) + '\\n');
  process.exit(0);
}
if (args.slice(0, 2).join(' ') === 'test-bridge run') {
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
  // flag. Allowed occurrences in production sources: two protocol.ts type
  // declarations — the desired-state group and the typed discovery-item facts
  // that replaced the k=v limitations encoding (requirement-127) — and, in
  // extension.ts, the display passthrough in formatDesiredStateGroup plus the
  // single membership read in exclusiveGroupPeerItems.
  //
  // The second extension.ts occurrence is the bounded narrowing
  // keel/requirement-132 makes to design_decision-5, and it is narrow on the
  // axis the decision protects. design_decision-5 forbids the VSIX DECIDING a
  // rendered state from the flag — that is the bridge's job, served as
  // reconcile_results and replayed verbatim. exclusiveGroupPeerItems decides no
  // state: it answers only "which rows form this group", for an interval the
  // bridge cannot serve because the run has not happened yet (keel/ac-517). The
  // transitional value it feeds is a fixed skipped, not a computed one, and the
  // bridge still overwrites every row at run end (keel/ac-513). Raising this
  // count again for a rendering branch would breach the decision.
  //
  // DHF-TEST: keel/requirement-97, keel/requirement-127, keel/requirement-132 (keel/ac-517)
  test('production sources do not branch on mutually_exclusive', () => {
    const srcDir = path.resolve(__dirname, '../../../src');
    const allowed = new Map([['protocol.ts', 2], ['extension.ts', 2]]);
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
      assert.match(calls, /test-bridge run --source editor --id testbridge::maintenance::clear-state/);
      assert.match(calls, /test-bridge run --source editor --id testbridge::maintenance::unlock/);
      assert.match(calls, /test-bridge run --source editor --id testbridge::maintenance::detect-lanes/);
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
      "if (args.join(' ') === 'test-bridge discover --format json') {",
      "  console.log(JSON.stringify({ version: 1, workspace: process.cwd(), generated_at: now(), capabilities: { clear_results_test_ids: ['case::maintenance::clear'] }, items: [{ id: selected, label: selected, kind: 'maintenance', runnable: true, profiles: ['run'] }] }));",
      "  process.exit(0);",
      "}",
      "if (args.slice(0, 3).join(' ') === 'test-bridge desired-state --format') {",
      "  if (mode === 'desired-fail') { console.error('desired-state failed intentionally'); process.exit(3); }",
      "  if (mode === 'start-fail') { fs.writeFileSync(sentinel, 'ready'); }",
      "  console.log(JSON.stringify({ version: 3, workspace: process.cwd(), generated_at: now(), groups: [{ label: 'Empty', order: 1, mutually_exclusive: false, rows: [] }] }));",
      "  process.exit(0);",
      "}",
      "if (args.slice(0, 2).join(' ') === 'test-bridge run') {",
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

  // DHF-TEST: keel/requirement-71, keel/requirement-88
  test('run profile enqueues the selected leaf scope before the first result event', async function () {
    this.timeout(10_000);
    const root = fs.mkdtempSync(path.join(os.tmpdir(), 'keel-enqueue-profile-'));
    const previousDevWorkspace = process.env.KEEL_VSCODE_BRIDGE_DEV_WORKSPACE;
    process.env.KEEL_VSCODE_BRIDGE_DEV_WORKSPACE = root;
    fs.mkdirSync(path.join(root, '.vscode'), { recursive: true });
    const fake = path.join(root, 'fake-enqueue-adapter.js');
    fs.writeFileSync(fake, [
      "const args = process.argv.slice(2);",
      "const now = () => new Date().toISOString();",
      "if (args.includes('--version')) { console.log('dev'); process.exit(0); }",
      "if (args.join(' ') === 'test-bridge discover --format json') {",
      "  console.log(JSON.stringify({ version: 1, workspace: process.cwd(), generated_at: now(), items: [",
      "    { id: 'case::suite', label: 'suite', kind: 'suite', runnable: true, profiles: ['run'] },",
      "    { id: 'case::test::a', parent_id: 'case::suite', label: 'a', kind: 'test', runnable: true, profiles: ['run'] },",
      "    { id: 'case::test::b', parent_id: 'case::suite', label: 'b', kind: 'test', runnable: true, profiles: ['run'] },",
      "    { id: 'case::covers', parent_id: 'case::suite', label: 'covers', kind: 'group', runnable: false, profiles: [] },",
      "    { id: 'case::covers::b', parent_id: 'case::covers', label: 'b via covers', kind: 'test', canonical_id: 'case::test::b', runnable: true, profiles: ['run'] },",
      "    { id: 'demo::desired-state::dataset::small', parent_id: 'case::suite', label: 'small', kind: 'test', runnable: true, profiles: ['run'] }",
      "  ] }));",
      "  process.exit(0);",
      "}",
      "if (args.slice(0, 3).join(' ') === 'test-bridge desired-state --format') {",
      "  console.log(JSON.stringify({ version: 3, workspace: process.cwd(), generated_at: now(), groups: [{ label: 'Empty', order: 1, mutually_exclusive: false, rows: [] }] }));",
      "  process.exit(0);",
      "}",
      "if (args.slice(0, 2).join(' ') === 'test-bridge run') {",
      "  const emit = (event) => process.stdout.write(JSON.stringify({ version: 1, time: now(), run_id: 'profile-enqueue', ...event }) + '\\n');",
      "  emit({ event: 'run_started', test_id: 'case::suite' });",
      "  emit({ event: 'test_started', test_id: 'case::test::a' });",
      "  emit({ event: 'passed', test_id: 'case::test::a', duration_ms: 1 });",
      "  emit({ event: 'test_started', test_id: 'case::test::b' });",
      "  emit({ event: 'passed', test_id: 'case::test::b', duration_ms: 1 });",
      "  emit({ event: 'run_finished', exit_code: 0 });",
      "  process.exit(0);",
      "}",
      "process.exit(2);"
    ].join('\n'));
    fs.writeFileSync(path.join(root, configRelativePath), JSON.stringify({
      version: currentConfigVersion,
      command: process.execPath,
      args: [fake],
      displayName: 'Keel'
    }, null, 2) + '\n');

    try {
      const extension = vscode.extensions.getExtension('aggeler.keel-test-bridge');
      assert.ok(extension, 'extension should be discoverable');
      await extension.activate();
      await vscode.commands.executeCommand('keel.tests.refresh');
      const controller = testControllerForTest();
      assert.ok(controller, 'extension should expose its active TestController for tests');
      const originalCreateTestRun = controller.createTestRun.bind(controller);
      const stamps: StateStamp[] = [];
      controller.createTestRun = ((request: vscode.TestRunRequest, name?: string, persist?: boolean) => {
        const run = originalCreateTestRun(request, name, persist);
        recordStateStamps(run, stamps);
        return run;
      }) as typeof controller.createTestRun;
      try {
        await runProfileHandlerForTest('case::suite');
      } finally {
        controller.createTestRun = originalCreateTestRun;
      }

      const enqueued = stamps.filter((stamp) => stamp.state === 'queued').map((stamp) => stamp.id);
      assert.deepEqual(enqueued.sort(), ['case::covers::b', 'case::test::a', 'case::test::b'], 'all non-no-result leaf items, including covers aliases, are enqueued');
      assert.ok(!enqueued.includes('demo::desired-state::dataset::small'), 'desired-state no-result namespace is not enqueued');
      assert.ok(stamps.findIndex((stamp) => stamp.state === 'queued') < stamps.findIndex((stamp) => stamp.state === 'running'), 'enqueue precedes the first start event');
      assertAncestorsNeverTerminalMidRun(stamps, [['case::suite', ['case::test::a', 'case::test::b', 'case::covers::b']]]);
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

  // DHF-TEST: keel/requirement-115
  test('failed discovery refresh clears the published tree and reports the bound-specific error', async function () {
    this.timeout(10_000);
    const testBound = 64;
    let restoreBound: (() => void) | undefined;
    const root = fs.mkdtempSync(path.join(os.tmpdir(), 'keel-refresh-failure-state-'));
    const previousDevWorkspace = process.env.KEEL_VSCODE_BRIDGE_DEV_WORKSPACE;
    process.env.KEEL_VSCODE_BRIDGE_DEV_WORKSPACE = root;
    fs.mkdirSync(path.join(root, '.vscode'), { recursive: true });
    const fake = path.join(root, 'refresh-failure-adapter.cjs');
    fs.writeFileSync(fake, [
      "const args = process.argv.slice(2);",
      "const mode = process.env.KEEL_REFRESH_FAILURE_MODE || 'ok';",
      "if (args.includes('--version')) { console.log('dev'); process.exit(0); }",
      "if (args.join(' ') === 'test-bridge discover --format json') {",
      `  if (mode === 'oversized') { process.stdout.write('x'.repeat(${testBound + 1})); process.exit(0); }`,
      "  if (mode === 'nonzero') { console.error('producer failed'); process.exit(3); }",
      "  if (mode === 'malformed') { process.stdout.write('{not-json'); process.exit(0); }",
      "  console.log(JSON.stringify({ version: 1, workspace: process.cwd(), generated_at: new Date().toISOString(), items: [{ id: 'case::lane', label: 'case lane', kind: 'lane', runnable: true, profiles: ['run'] }] }));",
      "  process.exit(0);",
      "}",
      "process.exit(2);"
    ].join('\n'));
    fs.writeFileSync(path.join(root, configRelativePath), JSON.stringify({
      version: currentConfigVersion,
      command: process.execPath,
      args: [fake],
      displayName: 'Refresh Producer',
      env: { KEEL_REFRESH_FAILURE_MODE: 'ok' }
    }, null, 2) + '\n');

    const windowWithSpy = vscode.window as typeof vscode.window & {
      showErrorMessage: (message: string, ...items: string[]) => Thenable<string | undefined>;
    };
    const originalShowErrorMessage = windowWithSpy.showErrorMessage.bind(vscode.window);
    const shownErrors: string[] = [];
    windowWithSpy.showErrorMessage = ((message: string, ...items: unknown[]) => {
      shownErrors.push(message);
      return Promise.resolve(items.find((item): item is string => typeof item === 'string'));
    }) as unknown as typeof windowWithSpy.showErrorMessage;

    try {
      const extension = vscode.extensions.getExtension('aggeler.keel-test-bridge');
      assert.ok(extension, 'extension should be discoverable');
      await extension.activate();
      await vscode.commands.executeCommand('keel.tests.refresh');
      assert.ok(publishedTestItemIds().includes('case::lane'), 'successful refresh publishes the baseline item');

      for (const mode of ['oversized', 'nonzero', 'malformed', 'missing-binary']) {
        fs.writeFileSync(path.join(root, configRelativePath), JSON.stringify({
          version: currentConfigVersion,
          command: process.execPath,
          args: [fake],
          displayName: 'Refresh Producer',
          env: { KEEL_REFRESH_FAILURE_MODE: 'ok' }
        }, null, 2) + '\n');
        await vscode.commands.executeCommand('keel.tests.refresh');
        assert.ok(publishedTestItemIds().includes('case::lane'), `${mode} setup must publish the baseline item`);

        if (mode === 'oversized') {
          restoreBound = setDiscoveryOutputMaxBufferBytesForTest(testBound);
        }
        fs.writeFileSync(path.join(root, configRelativePath), JSON.stringify({
          version: currentConfigVersion,
          command: mode === 'oversized'
            ? nodeExecutableForTest()
            : mode === 'missing-binary'
              ? path.join(root, 'absent-refresh-producer')
              : process.execPath,
          args: mode === 'missing-binary' ? [] : [fake],
          displayName: 'Refresh Producer',
          env: { KEEL_REFRESH_FAILURE_MODE: mode }
        }, null, 2) + '\n');
        await vscode.commands.executeCommand('keel.tests.refresh');
        assert.deepEqual(publishedTestItemIds(), [], `${mode} discovery failure must clear the tree`);
        restoreBound?.();
        restoreBound = undefined;
      }

      const oversizedMessage = shownErrors.find((message) => message.includes(`${testBound}`));
      assert.ok(oversizedMessage, `expected a bound-specific error in ${JSON.stringify(shownErrors)}`);
      assert.match(oversizedMessage, /producer/i);
      assert.match(oversizedMessage, /document size/i);
      assert.doesNotMatch(oversizedMessage, /stdout maxBuffer length exceeded/);
      assert.doesNotMatch(oversizedMessage, /just build-dev/i);
    } finally {
      restoreBound?.();
      windowWithSpy.showErrorMessage = originalShowErrorMessage;
      if (previousDevWorkspace === undefined) {
        delete process.env.KEEL_VSCODE_BRIDGE_DEV_WORKSPACE;
      } else {
        process.env.KEEL_VSCODE_BRIDGE_DEV_WORKSPACE = previousDevWorkspace;
      }
      fs.rmSync(root, { recursive: true, force: true });
    }
  });

  // DHF-TEST: keel/requirement-115
  test('wire schema publishes the discovery size bound and failed-refresh state', () => {
    const doc = fs.readFileSync(path.resolve(__dirname, '../../../../docs/wire-schema.md'), 'utf8');
    assert.match(doc, new RegExp(`${discoveryOutputMaxBufferBytes}`));
    assert.match(doc, /discovery document/i);
    assert.match(doc, /reject/i);
    assert.match(doc, /failed refresh/i);
    assert.match(doc, /clear/i);
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

  // DHF-TEST: keel/requirement-71, keel/requirement-88, keel/requirement-116
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
      assert.deepEqual(Object.keys(runEventApplicationSnapshot('case::test::a', new Set(['case::test::a']), resultIds)).sort(), ['resultIds', 'skippedSiblingIds']);
      assert.equal(shouldApplyResultToItem(tree.itemsById.get('case::suite') as vscode.TestItem, new Set(), new Set(['case::test::a', 'case::test::b', 'case::alias::a'])), true);
      assert.deepEqual(resultItemsForRunEvent([tree.itemsById.get('case::test::a') as vscode.TestItem, tree.itemsById.get('case::test::a') as vscode.TestItem]).map((item) => item.id), ['case::test::a']);

      const leafSelectedPassed: string[] = [];
      const leafSelectedSkipped: string[] = [];
      const leafSelectedRun = {
        passed(item: vscode.TestItem) { leafSelectedPassed.push(item.id); },
        skipped(item: vscode.TestItem) { leafSelectedSkipped.push(item.id); },
        appendOutput() { /* no-op */ }
      };
      applyRunEvent(leafSelectedRun as unknown as vscode.TestRun, JSON.stringify(runEvent({ event: 'passed', test_id: 'case::test::a' })), new Set(['case::test::a']), new Set());
      assert.deepEqual(leafSelectedPassed.sort(), ['case::alias::a', 'case::test::a'], 'targeted leaf pass is applied before run end');
      assert.deepEqual(leafSelectedSkipped, [], 'post-pass, pre-run.end targeted-leaf replay leaves ancestor state to VS Code rollup');

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

  // The bridge now emits per-test Mocha events keyed to vsix::test::* ids
  // (requirement-94); the extension replays them onto the discovered per-test
  // items verbatim, and one failing case isolates to its own id (ac-307).
  //
  // DHF-TEST: keel/requirement-94
  test('vsix per-test run events replay onto discovered vsix::test items', () => {
    const controller = vscode.tests.createTestController(`keelVSIXPerTest-${Date.now()}`, 'Keel VSIX Per Test');
    const root = fs.mkdtempSync(path.join(os.tmpdir(), 'keel-vsix-per-test-'));
    const fileId = 'vsix::file::src/test/suite/tree.test.ts';
    const alphaId = 'vsix::test::src/test/suite/tree.test.ts::alpha-case';
    const betaId = 'vsix::test::src/test/suite/tree.test.ts::beta-case';
    const tree = publishDiscovery(controller, root, {
      version: 1,
      workspace: root,
      generated_at: new Date().toISOString(),
      items: [
        { id: 'vsix::root', label: 'd.2 Mocha (vsix)', kind: 'root', framework: 'vsix', runnable: true, profiles: ['run'] },
        { id: fileId, parent_id: 'vsix::root', label: 'tree.test.ts', kind: 'file', framework: 'vsix', runnable: true, profiles: ['run'] },
        { id: alphaId, parent_id: fileId, label: 'alpha case', kind: 'test', framework: 'vsix', runnable: false, profiles: [] },
        { id: betaId, parent_id: fileId, label: 'beta case', kind: 'test', framework: 'vsix', runnable: false, profiles: [] }
      ]
    });
    setCurrentTreeForTest(tree);

    const terminal = new Map<string, string>();
    const run = {
      started() {},
      passed(item: vscode.TestItem) { terminal.set(item.id, 'passed'); },
      failed(item: vscode.TestItem) { terminal.set(item.id, 'failed'); },
      errored(item: vscode.TestItem) { terminal.set(item.id, 'errored'); },
      skipped(item: vscode.TestItem) { terminal.set(item.id, 'skipped'); },
      appendOutput() {},
      end() {}
    };
    try {
      const selected = new Set([fileId]);
      const resultIds = new Set<string>();
      // The exact stream shape a bridge run now emits from the Mocha JSONL:
      // serial per-test started→terminal events keyed to vsix::test ids.
      applyRunEvent(run as unknown as vscode.TestRun, JSON.stringify(runEvent({ event: 'test_started', test_id: alphaId })), selected, resultIds);
      applyRunEvent(run as unknown as vscode.TestRun, JSON.stringify(runEvent({ event: 'passed', test_id: alphaId, duration_ms: 5 })), selected, resultIds);
      applyRunEvent(run as unknown as vscode.TestRun, JSON.stringify(runEvent({ event: 'test_started', test_id: betaId })), selected, resultIds);
      applyRunEvent(run as unknown as vscode.TestRun, JSON.stringify(runEvent({ event: 'failed', test_id: betaId, message: 'boom' })), selected, resultIds);

      assert.equal(terminal.get(alphaId), 'passed', `alpha ends green; terminal=${JSON.stringify([...terminal])}`);
      assert.equal(terminal.get(betaId), 'failed', `one failing case isolates to its own id (ac-307); terminal=${JSON.stringify([...terminal])}`);
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
    assert.ok(bareDiscovery.items.some((item) => item.id === 'testbridge::maintenance::detect-lanes'));
    assert.ok(!bareDiscovery.items.some((item) => item.id === 'keel::lane::lint'));
    const detect = await collectChild(runTests(root, ['testbridge::maintenance::detect-lanes']));
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
      assert.ok(demoTree.discoveryItemsById.has('testbridge::maintenance'));
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
    const devDetect = await collectChild(runTests(devRoot, ['testbridge::maintenance::detect-lanes']));
    assert.equal(devDetect.code, 0);
    const runStreamRoot = devRoot;
    const runsDir = path.join(runStreamRoot, '.devtools', 'vscode-runs');
    // No run.lock cleanup here: under a bridge-launched run the parent keel-dev
    // legitimately holds the lock and nested runs proceed via the inherited
    // KEEL_TESTBRIDGE_RUN_LOCK_TOKEN (requirement-96, issue-88).
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
      // DHF-TEST: keel/requirement-36
      // The editor started this run, so the stream it spooled says so. This is
      // the wire value the external-run mirror reads to skip its own runs.
      assert.ok(
        runEvents.length > 0 && runEvents.every((event) => event.source === 'editor'),
        `editor-driven run stream must declare the editor surface on every event; got ${JSON.stringify(runEvents.map((event) => event.source))}`
      );
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
        clear_results_test_ids: ['testbridge::maintenance::clear-results']
      },
      items: [{ id: 'testbridge::maintenance::clear-results', label: 'clear Keel test results', kind: 'maintenance', runnable: true, profiles: ['run'] }]
    });
    setCurrentTreeForTest(tree);

    assert.equal(shouldInvalidateResultsForEvent({
      version: 1,
      event: 'passed',
      time: new Date().toISOString(),
      test_id: 'testbridge::maintenance::clear-results'
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

  // The mirror exists to surface runs started outside the editor. A run the
  // editor itself started declares that surface on the wire, and the mirror
  // leaves it alone; a stream that declares no recognized surface is still
  // imported, which is what a pre-upgrade spool file and a third-party
  // producer both look like.
  //
  // DHF-TEST: keel/requirement-36
  test('external run mirror skips an editor-initiated stream and imports an unattributed one', async function () {
    this.timeout(10_000);
    const root = fs.mkdtempSync(path.join(os.tmpdir(), 'keel-external-origin-'));
    const previousDevWorkspace = process.env.KEEL_VSCODE_BRIDGE_DEV_WORKSPACE;
    process.env.KEEL_VSCODE_BRIDGE_DEV_WORKSPACE = root;
    const controller = vscode.tests.createTestController(`keelExternalOrigin-${Date.now()}`, 'Keel External Origin');
    const tree = publishDiscovery(controller, root, {
      version: 1,
      workspace: root,
      generated_at: new Date().toISOString(),
      items: [
        { id: 'keel::lane::test-fast', label: 'test-fast', kind: 'lane', runnable: true, profiles: ['run'] },
        { id: 'keel::lane::lint', label: 'lint', kind: 'lane', runnable: true, profiles: ['run'] }
      ]
    });
    setCurrentTreeForTest(tree);
    const runNames: string[] = [];
    const passed: string[] = [];
    const spyTarget = controller as vscode.TestController & {
      createTestRun: (request: vscode.TestRunRequest, name?: string, persist?: boolean) => vscode.TestRun;
    };
    const originalCreateTestRun = spyTarget.createTestRun.bind(controller);
    spyTarget.createTestRun = (request: vscode.TestRunRequest, name?: string, persist?: boolean): vscode.TestRun => {
      runNames.push(name ?? '');
      const run = originalCreateTestRun(request, name, persist);
      const originalPassed = run.passed.bind(run);
      run.passed = (item: vscode.TestItem, duration?: number) => {
        passed.push(item.id);
        originalPassed(item, duration);
      };
      return run;
    };
    const mirror = new ExternalRunMirror(controller);
    const runsDir = path.join(root, '.devtools', 'vscode-runs');
    fs.mkdirSync(runsDir, { recursive: true });
    fs.writeFileSync(path.join(runsDir, '001-editor-initiated.jsonl'), [
      JSON.stringify(runEvent({ event: 'run_started', source: 'editor', run_id: 'editor-run', test_id: 'keel::lane::test-fast' })),
      JSON.stringify(runEvent({ event: 'test_started', source: 'editor', run_id: 'editor-run', test_id: 'keel::lane::test-fast' })),
      JSON.stringify(runEvent({ event: 'passed', source: 'editor', run_id: 'editor-run', test_id: 'keel::lane::test-fast' })),
      JSON.stringify(runEvent({ event: 'run_finished', source: 'editor', run_id: 'editor-run', exit_code: 0 }))
    ].join('\n') + '\n');
    fs.writeFileSync(path.join(runsDir, '002-unattributed.jsonl'), [
      JSON.stringify(runEvent({ event: 'run_started', run_id: 'unattributed-run', test_id: 'keel::lane::lint' })),
      JSON.stringify(runEvent({ event: 'test_started', run_id: 'unattributed-run', test_id: 'keel::lane::lint' })),
      JSON.stringify(runEvent({ event: 'passed', run_id: 'unattributed-run', test_id: 'keel::lane::lint' })),
      JSON.stringify(runEvent({ event: 'run_finished', run_id: 'unattributed-run', exit_code: 0 }))
    ].join('\n') + '\n');

    try {
      await mirror.syncWorkspace();
      assert.ok(!runNames.some((name) => name.includes('editor-run')), `editor-initiated stream must not open a test run; opened ${JSON.stringify(runNames)}`);
      assert.ok(!passed.includes('keel::lane::test-fast'), 'editor-initiated stream must stamp no result onto the tree');
      assert.ok(runNames.some((name) => name.includes('unattributed-run')), `unattributed stream must still be imported; opened ${JSON.stringify(runNames)}`);
      assert.ok(passed.includes('keel::lane::lint'), 'unattributed stream must still stamp its result onto the tree');
      assert.ok(!mirror.snapshots().some((snapshot) => snapshot.runId === 'editor-run'), 'editor-initiated stream must not be tracked as a mirrored stream');
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

  // DHF-TEST: keel/requirement-71, keel/requirement-88
  test('external run mirror enqueues the selected leaf scope before replaying results', async function () {
    this.timeout(10_000);
    const root = fs.mkdtempSync(path.join(os.tmpdir(), 'keel-external-enqueue-'));
    const previousDevWorkspace = process.env.KEEL_VSCODE_BRIDGE_DEV_WORKSPACE;
    process.env.KEEL_VSCODE_BRIDGE_DEV_WORKSPACE = root;
    const controller = vscode.tests.createTestController(`keelExternalEnqueue-${Date.now()}`, 'Keel External Enqueue');
    const tree = publishDiscovery(controller, root, {
      version: 1,
      workspace: root,
      generated_at: new Date().toISOString(),
      items: [
        { id: 'case::suite', label: 'suite', kind: 'suite', runnable: true, profiles: ['run'] },
        { id: 'case::test::a', parent_id: 'case::suite', label: 'a', kind: 'test', runnable: true, profiles: ['run'] },
        { id: 'case::test::b', parent_id: 'case::suite', label: 'b', kind: 'test', runnable: true, profiles: ['run'] },
        { id: 'case::covers', parent_id: 'case::suite', label: 'covers', kind: 'group', runnable: false, profiles: [] },
        { id: 'case::covers::b', parent_id: 'case::covers', label: 'b via covers', kind: 'test', canonical_id: 'case::test::b', runnable: true, profiles: ['run'] },
        { id: 'demo::desired-state::dataset::small', parent_id: 'case::suite', label: 'small', kind: 'test', runnable: true, profiles: ['run'] }
      ]
    });
    setCurrentTreeForTest(tree);
    const spyTarget = controller as vscode.TestController & {
      createTestRun: (request: vscode.TestRunRequest, name?: string, persist?: boolean) => vscode.TestRun;
    };
    const originalCreateTestRun = spyTarget.createTestRun.bind(controller);
    const stamps: StateStamp[] = [];
    spyTarget.createTestRun = (request: vscode.TestRunRequest, name?: string, persist?: boolean): vscode.TestRun => {
      const run = originalCreateTestRun(request, name, persist);
      recordStateStamps(run, stamps);
      return run;
    };
    const mirror = new ExternalRunMirror(controller);
    const runsDir = path.join(root, '.devtools', 'vscode-runs');
    fs.mkdirSync(runsDir, { recursive: true });
    const runFile = path.join(runsDir, `external-enqueue-${process.pid}-${Date.now()}.jsonl`);
    fs.writeFileSync(runFile, [
      JSON.stringify(runEvent({ event: 'run_started', run_id: 'external-enqueue', test_id: 'case::suite' })),
      JSON.stringify(runEvent({ event: 'test_started', run_id: 'external-enqueue', test_id: 'case::test::a' })),
      JSON.stringify(runEvent({ event: 'passed', run_id: 'external-enqueue', test_id: 'case::test::a' })),
      JSON.stringify(runEvent({ event: 'test_started', run_id: 'external-enqueue', test_id: 'case::test::b' })),
      JSON.stringify(runEvent({ event: 'passed', run_id: 'external-enqueue', test_id: 'case::test::b' })),
      JSON.stringify(runEvent({ event: 'run_finished', run_id: 'external-enqueue', exit_code: 0 }))
    ].join('\n') + '\n');

    try {
      await mirror.syncWorkspace();
      const enqueued = stamps.filter((stamp) => stamp.state === 'queued').map((stamp) => stamp.id);
      assert.deepEqual(enqueued.sort(), ['case::covers::b', 'case::test::a', 'case::test::b'], 'external mirror enqueues all non-no-result leaf items, including covers aliases');
      assert.ok(!enqueued.includes('demo::desired-state::dataset::small'), 'external mirror does not enqueue desired-state no-result ids');
      assert.ok(stamps.findIndex((stamp) => stamp.state === 'queued') < stamps.findIndex((stamp) => stamp.state === 'running'), 'external mirror enqueues before replaying starts');
      assertAncestorsNeverTerminalMidRun(stamps, [['case::suite', ['case::test::a', 'case::test::b', 'case::covers::b']]]);
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

  // DHF-TEST: keel/requirement-116
  test('external run mirror leaves targeted leaf ancestors unstamped after pass before run end', async function () {
    this.timeout(10_000);
    const root = fs.mkdtempSync(path.join(os.tmpdir(), 'keel-external-leaf-ancestor-'));
    const previousDevWorkspace = process.env.KEEL_VSCODE_BRIDGE_DEV_WORKSPACE;
    process.env.KEEL_VSCODE_BRIDGE_DEV_WORKSPACE = root;
    const controller = vscode.tests.createTestController(`keelExternalLeafAncestor-${Date.now()}`, 'Keel External Leaf Ancestor');
    const tree = publishDiscovery(controller, root, {
      version: 1,
      workspace: root,
      generated_at: new Date().toISOString(),
      items: [
        { id: 'case::suite', label: 'suite', kind: 'suite', runnable: true, profiles: ['run'] },
        { id: 'case::test::a', parent_id: 'case::suite', label: 'a', kind: 'test', runnable: true, profiles: ['run'] }
      ]
    });
    setCurrentTreeForTest(tree);
    const spyTarget = controller as vscode.TestController & {
      createTestRun: (request: vscode.TestRunRequest, name?: string, persist?: boolean) => vscode.TestRun;
    };
    const originalCreateTestRun = spyTarget.createTestRun.bind(controller);
    const stamps: StateStamp[] = [];
    spyTarget.createTestRun = (request: vscode.TestRunRequest, name?: string, persist?: boolean): vscode.TestRun => {
      const run = originalCreateTestRun(request, name, persist);
      recordStateStamps(run, stamps);
      return run;
    };
    const mirror = new ExternalRunMirror(controller);
    const runsDir = path.join(root, '.devtools', 'vscode-runs');
    fs.mkdirSync(runsDir, { recursive: true });
    const runFile = path.join(runsDir, `external-leaf-ancestor-${process.pid}-${Date.now()}.jsonl`);
    fs.writeFileSync(runFile, [
      JSON.stringify(runEvent({ event: 'run_started', run_id: 'external-leaf-ancestor', test_id: 'case::test::a' })),
      JSON.stringify(runEvent({ event: 'test_started', run_id: 'external-leaf-ancestor', test_id: 'case::test::a' })),
      JSON.stringify(runEvent({ event: 'passed', run_id: 'external-leaf-ancestor', test_id: 'case::test::a' }))
    ].join('\n') + '\n');

    try {
      await mirror.syncWorkspace();
      assert.ok(stamps.some((stamp) => stamp.id === 'case::test::a' && stamp.state === 'passed'), 'targeted leaf pass is replayed before run end');
      assert.ok(
        !stamps.some((stamp) => stamp.id === 'case::suite' && ['passed', 'failed', 'errored', 'skipped'].includes(stamp.state)),
        `post-pass, pre-run.end ancestor state is left to VS Code rollup; stamps=${JSON.stringify(stamps)}`
      );
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
if (args.slice(0, 3).join(' ') === 'test-bridge discover --format') {
  writeDiscovery();
  process.exit(0);
}
if (args.slice(0, 3).join(' ') === 'test-bridge desired-state --format') {
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
if (args.slice(0, 3).join(' ') === 'test-bridge discover --format') {
  process.stdout.write(JSON.stringify({
    version: 1,
    workspace: process.cwd(),
    generated_at: now(),
    items: [{ id: fullId, label: 'full', kind: 'test', runnable: true, profiles: ['run'] }]
  }) + '\\n');
  process.exit(0);
}
if (args.slice(0, 3).join(' ') === 'test-bridge desired-state --format') {
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
        calls.includes('test-bridge desired-state --format json --id demo::desired-state::dataset::full'),
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

  // DHF-TEST: keel/requirement-88, keel/requirement-116
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

  // DHF-TEST: keel/requirement-132 (keel/ac-517)
  test('exclusive-group peers are resolved from the published discovery tree alone', () => {
    const controller = vscode.tests.createTestController(`keelExclusivePeers-${Date.now()}`, 'Keel Exclusive Peers');
    const tree = publishDiscovery(controller, process.cwd(), exclusiveGroupsDiscoveryFixture());
    try {
      const full = tree.itemsById.get('demo::desired-state::dataset::full');
      const small = tree.itemsById.get('demo::desired-state::dataset::small');
      const warm = tree.itemsById.get('demo::desired-state::caches::warm');
      const lane = tree.itemsById.get('keel::lane::fast');
      assert.ok(full && small && warm && lane, 'the fixture publishes every row under test');

      // The launched row's own group yields every other row, the synthesized
      // Unknown State row included, and reaches no other group.
      assert.deepEqual(
        exclusiveGroupPeerItems(tree, [full]).map((item) => item.id).sort(),
        ['demo::desired-state::dataset::small', 'demo::desired-state::dataset::unknown']
      );

      // Two members of the same group leave only the rows outside the selection.
      assert.deepEqual(
        exclusiveGroupPeerItems(tree, [full, small]).map((item) => item.id).sort(),
        ['demo::desired-state::dataset::unknown']
      );

      // Selecting the group node covers every row, so nothing is a peer.
      const group = tree.itemsById.get('demo::desired-state::dataset');
      assert.ok(group, 'the fixture publishes the group node');
      assert.deepEqual(exclusiveGroupPeerItems(tree, [group]), []);

      // A non-exclusive group and an ordinary lane leaf own no peers at all.
      assert.deepEqual(exclusiveGroupPeerItems(tree, [warm]), []);
      assert.deepEqual(exclusiveGroupPeerItems(tree, [lane]), []);

      // With no published tree there is nothing to derive from.
      assert.deepEqual(exclusiveGroupPeerItems(undefined, [full]), []);
    } finally {
      controller.dispose();
    }
  });

  // DHF-TEST: keel/requirement-132 (keel/ac-512, keel/ac-514, keel/ac-515)
  test('every exclusive-group peer is stamped skipped before the devtool child is spawned', async function () {
    this.timeout(20_000);
    const workspace = createExclusiveGroupWorkspace('keel-run-start-invalidation-', 'activate');
    try {
      const extension = vscode.extensions.getExtension('aggeler.keel-test-bridge');
      assert.ok(extension, 'extension should be discoverable');
      await extension.activate();
      await vscode.commands.executeCommand('keel.tests.refresh');
      const controller = testControllerForTest();
      assert.ok(controller, 'extension should expose its active TestController for tests');

      const timeline: RunTimelineEntry[] = [];
      const restore = recordRunTimeline(controller, timeline);
      try {
        await runProfileHandlerForTest('demo::desired-state::dataset::full');
      } finally {
        restore();
      }

      const spawnIndex = timeline.findIndex((entry) => entry.kind === 'spawn');
      assert.ok(spawnIndex >= 0, `the devtool run child is spawned; timeline=${JSON.stringify(timeline)}`);
      const beforeSpawn = timeline.slice(0, spawnIndex);

      // ac-512: member 'small' rendered passed at rest; launching 'full'
      // stamps it and the synthesized Unknown State row skipped, and that
      // stamp is recorded before the child that will re-determine the truth.
      assert.deepEqual(
        stampedIds(beforeSpawn, 'skipped').sort(),
        ['demo::desired-state::dataset::small', 'demo::desired-state::dataset::unknown'],
        `peers are stamped skipped before the spawn; timeline=${JSON.stringify(timeline)}`
      );
      // ac-512: and no row of the group asserts it is the satisfied one from
      // the moment the run starts until the bridge says otherwise.
      assert.deepEqual(
        stampedIds(beforeSpawn, 'passed').filter((id) => datasetGroupRowIds.includes(id)),
        [],
        `no row of the group renders passed once the run has started; timeline=${JSON.stringify(timeline)}`
      );

      // ac-514: the invalidation reaches no row of the second exclusive group
      // and no lane, at any point in the run.
      const invalidationStamps = stampedIds(beforeSpawn, 'skipped');
      assert.deepEqual(
        invalidationStamps.filter((id) => !datasetGroupRowIds.includes(id)),
        [],
        'the run-start invalidation restamps only rows of the group that owns the selected member'
      );

      // ac-515: peers are restamped, never submitted. The argv id list is
      // exactly the selected runnable row.
      const spawn = timeline[spawnIndex];
      assert.equal(spawn.kind, 'spawn');
      assert.deepEqual(
        spawn.kind === 'spawn' ? spawn.ids : [],
        ['demo::desired-state::dataset::full'],
        'the invalidation adds no id to the devtool run invocation'
      );
    } finally {
      workspace.dispose();
    }
  });

  // DHF-TEST: keel/requirement-132 (keel/ac-513)
  test('an unchanged served reconcile list still replays after a run-start invalidation', async function () {
    this.timeout(20_000);
    // 'fail' leaves the active member exactly where it was, so the bridge
    // serves a list identical to the pre-run one. That is precisely when the
    // in-session signature guard would suppress the replay — and precisely
    // when the transitional all-skipped rendering is wrong.
    const workspace = createExclusiveGroupWorkspace('keel-run-start-replay-guard-', 'fail');
    try {
      const extension = vscode.extensions.getExtension('aggeler.keel-test-bridge');
      assert.ok(extension, 'extension should be discoverable');
      await extension.activate();
      resetReconcileSignatureForTest();
      await vscode.commands.executeCommand('keel.tests.refresh');
      const controller = testControllerForTest();
      assert.ok(controller, 'extension should expose its active TestController for tests');

      const timeline: RunTimelineEntry[] = [];
      const restore = recordRunTimeline(controller, timeline);
      try {
        await runProfileHandlerForTest('demo::desired-state::dataset::full');
      } finally {
        restore();
      }

      const spawnIndex = timeline.findIndex((entry) => entry.kind === 'spawn');
      assert.ok(spawnIndex >= 0, `the devtool run child is spawned; timeline=${JSON.stringify(timeline)}`);
      const afterSpawn = timeline.slice(spawnIndex + 1);

      // The served list is unchanged, so the replay must still fire and stamp
      // every row of the group with the served state.
      assert.ok(
        stampedIds(afterSpawn, 'passed').includes('demo::desired-state::dataset::small'),
        `the genuinely active member is restored to passed; timeline=${JSON.stringify(timeline)}`
      );
      assert.ok(
        stampedIds(afterSpawn, 'skipped').includes('demo::desired-state::dataset::full'),
        `the failed member renders the served state, not the transitional one; timeline=${JSON.stringify(timeline)}`
      );

      // The regression this guards against is worse than the bug it fixes: a
      // failed activation must never erase the record of what is active.
      const finalStateById = new Map<string, string>();
      for (const entry of timeline) {
        if (entry.kind !== 'spawn') {
          finalStateById.set(entry.id, entry.kind);
        }
      }
      assert.equal(
        finalStateById.get('demo::desired-state::dataset::small'),
        'passed',
        `the group is not left with every row skipped; timeline=${JSON.stringify(timeline)}`
      );
    } finally {
      workspace.dispose();
    }
  });

  // The three abort paths of keel/ac-516. Each reaches finishRun without a
  // normal completion, and one of them returns before the child is ever
  // spawned. Each has to converge on the bridge-served truth, or a failed
  // activation silently erases the record of what is actually active.
  //
  // DHF-TEST: keel/requirement-132 (keel/ac-516)
  test('a spawn failure settles the exclusive group on bridge-served truth', async function () {
    this.timeout(20_000);
    const workspace = createExclusiveGroupWorkspace('keel-run-start-spawn-failure-', 'activate');
    const adapterModule = bridgeAdapterModule as unknown as { runTests: typeof runTests };
    const realRunTests = adapterModule.runTests;
    try {
      const controller = await activateExclusiveGroupWorkspace();
      // The child never comes into existence — the one path that had no
      // post-run refresh at all before this unit.
      adapterModule.runTests = (() => {
        throw new Error('devtool binary is not executable');
      }) as typeof runTests;

      const timeline: RunTimelineEntry[] = [];
      const restore = recordRunTimeline(controller, timeline);
      try {
        await runProfileHandlerForTest('demo::desired-state::dataset::full');
      } finally {
        restore();
      }

      assert.ok(
        stampedIds(timeline, 'skipped').includes('demo::desired-state::dataset::small'),
        `the transitional stamp fired before the failed spawn; timeline=${JSON.stringify(timeline)}`
      );
      assertGroupSettledOnActiveMember(timeline, 'demo::desired-state::dataset::small');
    } finally {
      adapterModule.runTests = realRunTests;
      workspace.dispose();
    }
  });

  // DHF-TEST: keel/requirement-132 (keel/ac-516)
  test('a child error event settles the exclusive group on bridge-served truth', async function () {
    this.timeout(20_000);
    const workspace = createExclusiveGroupWorkspace('keel-run-start-child-error-', 'activate');
    const adapterModule = bridgeAdapterModule as unknown as { runTests: typeof runTests };
    const realRunTests = adapterModule.runTests;
    try {
      const controller = await activateExclusiveGroupWorkspace();
      // The child is handed back but immediately errors, so `close` never
      // arrives and the run settles through the error handler instead.
      adapterModule.runTests = (() => {
        const child = new EventEmitter() as unknown as cp.ChildProcessWithoutNullStreams;
        const mutable = child as unknown as { stdout: PassThrough; stderr: PassThrough; pid: number; kill: () => boolean };
        mutable.stdout = new PassThrough();
        mutable.stderr = new PassThrough();
        mutable.pid = 0;
        mutable.kill = () => true;
        // A real spawn failure emits BOTH, which is what makes the post-run
        // refresh dedupe observable: without it the devtool is re-queried for
        // a truth it has already read.
        setTimeout(() => {
          child.emit('error', new Error('devtool child failed'));
          child.emit('close', 1);
        }, 10);
        return child;
      }) as typeof runTests;

      const timeline: RunTimelineEntry[] = [];
      const restore = recordRunTimeline(controller, timeline);
      const desiredStateReads = countDesiredStateReads();
      try {
        await runProfileHandlerForTest('demo::desired-state::dataset::full');
      } finally {
        desiredStateReads.restore();
        restore();
      }

      assert.ok(
        stampedIds(timeline, 'skipped').includes('demo::desired-state::dataset::small'),
        `the transitional stamp fired before the child errored; timeline=${JSON.stringify(timeline)}`
      );
      assertGroupSettledOnActiveMember(timeline, 'demo::desired-state::dataset::small');
      // One read at run start, one to settle the group. A child that emits
      // both `error` and `close` must not re-query the devtool.
      assert.equal(desiredStateReads.count(), 2, 'the post-run refresh runs exactly once');
    } finally {
      adapterModule.runTests = realRunTests;
      workspace.dispose();
    }
  });

  // DHF-TEST: keel/requirement-132 (keel/ac-516)
  test('a cancelled run settles the exclusive group on bridge-served truth', async function () {
    this.timeout(30_000);
    // 'hang' starts the run and then waits to be signalled, so the cancel
    // arrives with the child genuinely in flight.
    const workspace = createExclusiveGroupWorkspace('keel-run-start-cancelled-', 'hang');
    try {
      const controller = await activateExclusiveGroupWorkspace();
      const timeline: RunTimelineEntry[] = [];
      const restore = recordRunTimeline(controller, timeline);
      const source = new vscode.CancellationTokenSource();
      const started = path.join(workspace.root, '.devtools-run-started');
      try {
        const running = runProfileHandlerForTest('demo::desired-state::dataset::full', source.token);
        await waitFor(() => fs.existsSync(started), 15_000);
        source.cancel();
        await running;
      } finally {
        source.dispose();
        restore();
      }

      assert.ok(
        stampedIds(timeline, 'skipped').includes('demo::desired-state::dataset::small'),
        `the transitional stamp fired before the cancel; timeline=${JSON.stringify(timeline)}`
      );
      assertGroupSettledOnActiveMember(timeline, 'demo::desired-state::dataset::small');
    } finally {
      workspace.dispose();
    }
  });

  // DHF-TEST: keel/requirement-132 (keel/ac-514, keel/ac-515)
  test('the run-start invalidation reaches only the selection own group and submits no peer id', async function () {
    this.timeout(20_000);
    const workspace = createExclusiveGroupWorkspace('keel-run-start-blast-radius-', 'activate');
    try {
      const controller = await activateExclusiveGroupWorkspace();
      const timeline: RunTimelineEntry[] = [];
      const restore = recordRunTimeline(controller, timeline);
      try {
        await runProfileHandlerForTest('demo::desired-state::dataset::full');
      } finally {
        restore();
      }

      // ac-514: isolate the invalidation from every other run of the session.
      // It restamps the rows of the selected member's group and nothing else —
      // not the second exclusive group, not the lane. This is the standing
      // guard against an implementation reaching for testing.clearTestResults,
      // which reaches true Unset but takes the whole Test Explorer with it.
      const invalidation = timeline.filter((entry) => entry.kind !== 'spawn' && entry.run === runStartInvalidationRunName);
      assert.deepEqual(
        stampedIds(invalidation, 'skipped').sort(),
        ['demo::desired-state::dataset::small', 'demo::desired-state::dataset::unknown'],
        `the invalidation restamps only the selected member's group; timeline=${JSON.stringify(timeline)}`
      );
      assert.deepEqual(
        stampedIds(invalidation, 'passed'),
        [],
        'the invalidation renders no row satisfied'
      );

      // The lane carries no desired-state truth, so the run touches it at no
      // point — neither the invalidation nor the post-run replay.
      assert.deepEqual(
        timeline.filter((entry) => entry.kind !== 'spawn' && entry.id === 'keel::lane::fast'),
        [],
        `lane results are never restamped by a desired-state run; timeline=${JSON.stringify(timeline)}`
      );
      // The second exclusive group is reached only by the bridge's own replay,
      // never by the invalidation, and its served truth does not move.
      const runtimeStamps = timeline.filter((entry) => entry.kind !== 'spawn' && entry.id.startsWith('demo::desired-state::runtime'));
      assert.ok(
        runtimeStamps.every((entry) => entry.kind !== 'spawn' && entry.run !== runStartInvalidationRunName),
        `the second group is never restamped by the invalidation; timeline=${JSON.stringify(timeline)}`
      );
      assert.deepEqual(
        stampedIds(runtimeStamps, 'passed').filter((id, index, ids) => ids.indexOf(id) === index),
        ['demo::desired-state::runtime::node'],
        'the second group keeps the member it had before the run'
      );

      // ac-515: the exception requirement-132 carves out of ac-348 is on the
      // rendering axis only. Widening it to the execution axis would submit
      // ids the bridge did not serve as runnable, which the bridge rejects.
      const spawns = timeline.flatMap((entry) => entry.kind === 'spawn' ? [entry.ids] : []);
      assert.deepEqual(spawns, [['demo::desired-state::dataset::full']], 'exactly the selected runnable row is submitted');
      for (const ids of spawns) {
        assert.deepEqual(
          ids.filter((id) => id.startsWith('testbridge::desired-state')),
          [],
          'no informational row id in the VSIX-private namespace is submitted'
        );
      }
    } finally {
      workspace.dispose();
    }
  });
});

// createExclusiveGroupWorkspace stands up a workspace whose adapter serves two
// mutually-exclusive desired-state groups and one ordinary lane, so a run of a
// member of the first group can be observed against everything it must not
// touch. `mode` selects what the run child does:
//
//   activate — records the selected member as active, passes, exits 0
//   fail     — leaves the active member where it was, fails, exits non-zero
//   hang     — starts and then waits to be signalled
//
// The served reconcile_results always describe the CURRENT active member, so
// the `fail` mode serves a list identical to the pre-run one — the case
// keel/ac-513 exists for.
interface ExclusiveGroupWorkspace {
  root: string;
  dispose(): void;
}

function createExclusiveGroupWorkspace(prefix: string, mode: 'activate' | 'fail' | 'hang'): ExclusiveGroupWorkspace {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), prefix));
  const previousDevWorkspace = process.env.KEEL_VSCODE_BRIDGE_DEV_WORKSPACE;
  process.env.KEEL_VSCODE_BRIDGE_DEV_WORKSPACE = root;
  fs.mkdirSync(path.join(root, '.vscode'), { recursive: true });
  const adapter = path.join(root, 'exclusive-groups-adapter.js');
  fs.writeFileSync(adapter, `
const fs = require('node:fs');
const path = require('node:path');
const args = process.argv.slice(2);
const now = () => new Date().toISOString();
const activePath = path.join(process.cwd(), '.devtools', 'active-member');
const mode = ${JSON.stringify(mode)};
if (args.includes('--version')) {
  process.stdout.write('dev\\n');
  process.exit(0);
}
const datasetRows = [
  ['demo::desired-state::dataset::small', 'small'],
  ['demo::desired-state::dataset::full', 'full'],
  ['demo::desired-state::dataset::unknown', 'Unknown State']
];
const runtimeRows = [
  ['demo::desired-state::runtime::node', 'node'],
  ['demo::desired-state::runtime::unknown', 'Unknown State']
];
function activeMember() {
  try {
    return fs.readFileSync(activePath, 'utf8').trim();
  } catch {
    return 'demo::desired-state::dataset::small';
  }
}
function discovery() {
  const active = activeMember();
  const rowItem = (parentId) => ([id, label]) => ({
    id, parent_id: parentId, label, kind: 'group', runnable: true, profiles: ['run'],
    desired_state_row: { current: label, action: 'reuse', active: id === active }
  });
  return {
    version: 1,
    workspace: process.cwd(),
    generated_at: now(),
    capabilities: {
      reconcile_results: datasetRows.map(([id]) => ({
        test_id: id,
        state: id === active ? 'passed' : 'skipped',
        message: id === active ? 'active' : 'not active'
      })).concat(runtimeRows.map(([id]) => ({
        test_id: id,
        state: id === 'demo::desired-state::runtime::node' ? 'passed' : 'skipped',
        message: id === 'demo::desired-state::runtime::node' ? 'active' : 'not active'
      })))
    },
    items: [
      { id: 'demo::desired-state::dataset', label: 'Data Set', kind: 'group', runnable: false, profiles: [], desired_state_group: { mutually_exclusive: true } },
      ...datasetRows.map(rowItem('demo::desired-state::dataset')),
      { id: 'demo::desired-state::runtime', label: 'Runtime', kind: 'group', runnable: false, profiles: [], desired_state_group: { mutually_exclusive: true } },
      ...runtimeRows.map(rowItem('demo::desired-state::runtime')),
      { id: 'keel::lane::fast', label: 'fast', kind: 'lane', runnable: true, profiles: ['run'] }
    ]
  };
}
function desiredState() {
  const active = activeMember();
  const group = (label, order, rows) => ({
    label, order, mutually_exclusive: true,
    rows: rows.map(([run_id, resource]) => ({
      run_id, resource, kind: 'dataset', desired: resource, current: resource,
      status: run_id === active ? 'satisfied' : 'available',
      action: run_id === active ? 'reuse' : 'none',
      message: resource, reusable: true, owned: false, active: run_id === active
    }))
  });
  return {
    version: 3,
    workspace: process.cwd(),
    generated_at: now(),
    groups: [group('Data Set', 1, datasetRows), group('Runtime', 2, runtimeRows)]
  };
}
if (args.slice(0, 3).join(' ') === 'test-bridge discover --format') {
  process.stdout.write(JSON.stringify(discovery()) + '\\n');
  process.exit(0);
}
if (args.slice(0, 3).join(' ') === 'test-bridge desired-state --format') {
  process.stdout.write(JSON.stringify(desiredState()) + '\\n');
  process.exit(0);
}
if (args.slice(0, 2).join(' ') === 'test-bridge run') {
  const selected = args[args.indexOf('--id') + 1];
  const emit = (event) => process.stdout.write(JSON.stringify({ version: 1, time: now(), run_id: 'mutex-run', ...event }) + '\\n');
  emit({ event: 'run_started', test_id: selected });
  emit({ event: 'test_started', test_id: selected });
  if (mode === 'hang') {
    fs.writeFileSync(path.join(process.cwd(), '.devtools-run-started'), selected + '\\n');
    setInterval(() => {}, 1000);
  } else if (mode === 'fail') {
    emit({ event: 'failed', test_id: selected, message: 'activation failed' });
    emit({ event: 'run_finished', exit_code: 1 });
    process.exit(1);
  } else {
    fs.mkdirSync(path.dirname(activePath), { recursive: true });
    fs.writeFileSync(activePath, selected + '\\n');
    emit({ event: 'passed', test_id: selected, duration_ms: 1 });
    emit({ event: 'run_finished', exit_code: 0 });
    process.exit(0);
  }
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
  return {
    root,
    dispose() {
      if (previousDevWorkspace === undefined) {
        delete process.env.KEEL_VSCODE_BRIDGE_DEV_WORKSPACE;
      } else {
        process.env.KEEL_VSCODE_BRIDGE_DEV_WORKSPACE = previousDevWorkspace;
      }
      fs.rmSync(root, { recursive: true, force: true });
    }
  };
}

// A single ordered timeline of every result stamp the extension makes and the
// moment the devtool run child is spawned. Ordering against the spawn is what
// makes keel/ac-512 falsifiable: a stamp that lands after the bridge has
// already reconciled proves nothing about the interval the requirement covers.
type RunTimelineEntry =
  | { kind: 'passed' | 'skipped'; id: string; run: string }
  | { kind: 'spawn'; ids: string[] };

// activateExclusiveGroupWorkspace activates the extension against the current
// workspace, arms a fresh reconcile signature, and hands back the controller.
async function activateExclusiveGroupWorkspace(): Promise<vscode.TestController> {
  const extension = vscode.extensions.getExtension('aggeler.keel-test-bridge');
  assert.ok(extension, 'extension should be discoverable');
  await extension.activate();
  resetReconcileSignatureForTest();
  await vscode.commands.executeCommand('keel.tests.refresh');
  const controller = testControllerForTest();
  assert.ok(controller, 'extension should expose its active TestController for tests');
  return controller;
}

// assertGroupSettledOnActiveMember reads the LAST state stamped onto each row
// of the group. keel/ac-516 is a claim about where the group comes to rest
// after an abort, not about the states it passes through on the way.
function assertGroupSettledOnActiveMember(timeline: readonly RunTimelineEntry[], activeId: string): void {
  const finalStateById = new Map<string, string>();
  for (const entry of timeline) {
    if (entry.kind !== 'spawn') {
      finalStateById.set(entry.id, entry.kind);
    }
  }
  assert.equal(
    finalStateById.get(activeId),
    'passed',
    `the genuinely active member is restored to passed; timeline=${JSON.stringify(timeline)}`
  );
  const settled = datasetGroupRowIds.filter((id) => finalStateById.get(id) === 'passed');
  assert.deepEqual(settled, [activeId], `exactly one row of the group renders passed; timeline=${JSON.stringify(timeline)}`);
}

// countDesiredStateReads counts devtool desired-state queries, so a settle path
// that fires twice is visible as a duplicate read rather than staying silent.
function countDesiredStateReads(): { count(): number; restore(): void } {
  const adapterModule = bridgeAdapterModule as unknown as { readDesiredState: typeof readDesiredState };
  const original = adapterModule.readDesiredState;
  let reads = 0;
  adapterModule.readDesiredState = ((workspaceRoot: string, ids: string[]) => {
    reads += 1;
    return original(workspaceRoot, ids);
  }) as typeof readDesiredState;
  return {
    count: () => reads,
    restore: () => {
      adapterModule.readDesiredState = original;
    }
  };
}

// stampedIds narrows the timeline to the ids stamped with one state, so an
// assertion reads as the sequence of rendered states rather than a type guard.
function stampedIds(timeline: readonly RunTimelineEntry[], kind: 'passed' | 'skipped'): string[] {
  return timeline.flatMap((entry) => entry.kind === kind ? [entry.id] : []);
}

function recordRunTimeline(controller: vscode.TestController, timeline: RunTimelineEntry[]): () => void {
  const originalCreateTestRun = controller.createTestRun.bind(controller);
  const adapterModule = bridgeAdapterModule as unknown as { runTests: typeof runTests };
  const originalRunTests = adapterModule.runTests;
  controller.createTestRun = ((request: vscode.TestRunRequest, name?: string, persist?: boolean) => {
    const run = originalCreateTestRun(request, name, persist);
    const originalPassed = run.passed.bind(run);
    const originalSkipped = run.skipped.bind(run);
    run.passed = (item: vscode.TestItem, duration?: number) => {
      timeline.push({ kind: 'passed', id: item.id, run: name ?? '' });
      originalPassed(item, duration);
    };
    run.skipped = (item: vscode.TestItem) => {
      timeline.push({ kind: 'skipped', id: item.id, run: name ?? '' });
      originalSkipped(item);
    };
    return run;
  }) as typeof controller.createTestRun;
  adapterModule.runTests = ((workspaceRoot: string, ids: string[]) => {
    timeline.push({ kind: 'spawn', ids: [...ids] });
    return originalRunTests(workspaceRoot, ids);
  }) as typeof runTests;
  return () => {
    controller.createTestRun = originalCreateTestRun;
    adapterModule.runTests = originalRunTests;
  };
}

const datasetGroupRowIds = [
  'demo::desired-state::dataset::small',
  'demo::desired-state::dataset::full',
  'demo::desired-state::dataset::unknown'
];

// exclusiveGroupsDiscoveryFixture publishes two mutually-exclusive
// desired-state groups, one non-exclusive group, and one ordinary lane leaf,
// so a peer-set assertion can prove both what is reached and what is not.
function exclusiveGroupsDiscoveryFixture(): DiscoveryDocument {
  const row = (id: string, parentId: string, label: string, active: boolean): DiscoveryItem => ({
    id,
    parent_id: parentId,
    label,
    kind: 'group',
    runnable: true,
    profiles: ['run'],
    desired_state_row: { current: label, action: 'reuse', active }
  });
  return {
    version: 1,
    workspace: process.cwd(),
    generated_at: new Date().toISOString(),
    items: [
      { id: 'demo::desired-state::dataset', label: 'Data Set', kind: 'group', runnable: false, profiles: [], desired_state_group: { mutually_exclusive: true } },
      row('demo::desired-state::dataset::small', 'demo::desired-state::dataset', 'small', true),
      row('demo::desired-state::dataset::full', 'demo::desired-state::dataset', 'full', false),
      row('demo::desired-state::dataset::unknown', 'demo::desired-state::dataset', 'Unknown State', false),
      { id: 'demo::desired-state::runtime', label: 'Runtime', kind: 'group', runnable: false, profiles: [], desired_state_group: { mutually_exclusive: true } },
      row('demo::desired-state::runtime::node', 'demo::desired-state::runtime', 'node', true),
      row('demo::desired-state::runtime::unknown', 'demo::desired-state::runtime', 'Unknown State', false),
      { id: 'demo::desired-state::caches', label: 'Caches', kind: 'group', runnable: true, profiles: ['run'], desired_state_group: { mutually_exclusive: false } },
      row('demo::desired-state::caches::warm', 'demo::desired-state::caches', 'warm', false),
      { id: 'keel::lane::fast', label: 'fast', kind: 'lane', runnable: true, profiles: ['run'] }
    ]
  };
}

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

function nodeExecutableForTest(): string {
  for (const candidate of [process.env.npm_node_execpath, process.env.NODE, '/usr/bin/node', '/usr/local/bin/node']) {
    if (candidate && path.isAbsolute(candidate) && fs.existsSync(candidate) && /^node(?:\.exe)?$/.test(path.basename(candidate))) {
      return candidate;
    }
  }
  return process.execPath;
}

function setDiscoveryOutputMaxBufferBytesForTest(bytes: number): () => void {
  const mutable = bridgeAdapterModule as unknown as { discoveryOutputMaxBufferBytes: number };
  const previous = mutable.discoveryOutputMaxBufferBytes;
  mutable.discoveryOutputMaxBufferBytes = bytes;
  return () => {
    mutable.discoveryOutputMaxBufferBytes = previous;
  };
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

type TestState = 'queued' | 'running' | 'passed' | 'failed' | 'errored' | 'skipped';

interface StateStamp {
  id: string;
  state: TestState;
}

function recordStateStamps(run: vscode.TestRun, stamps: StateStamp[]): void {
  const originalEnqueued = run.enqueued.bind(run);
  const originalStarted = run.started.bind(run);
  const originalPassed = run.passed.bind(run);
  const originalFailed = run.failed.bind(run);
  const originalErrored = run.errored.bind(run);
  const originalSkipped = run.skipped.bind(run);
  run.enqueued = (item: vscode.TestItem) => {
    stamps.push({ id: item.id, state: 'queued' });
    originalEnqueued(item);
  };
  run.started = (item: vscode.TestItem) => {
    stamps.push({ id: item.id, state: 'running' });
    originalStarted(item);
  };
  run.passed = (item: vscode.TestItem, duration?: number) => {
    stamps.push({ id: item.id, state: 'passed' });
    originalPassed(item, duration);
  };
  run.failed = (item: vscode.TestItem, message: vscode.TestMessage | readonly vscode.TestMessage[], duration?: number) => {
    stamps.push({ id: item.id, state: 'failed' });
    originalFailed(item, message, duration);
  };
  run.errored = (item: vscode.TestItem, message: vscode.TestMessage | readonly vscode.TestMessage[], duration?: number) => {
    stamps.push({ id: item.id, state: 'errored' });
    originalErrored(item, message, duration);
  };
  run.skipped = (item: vscode.TestItem) => {
    stamps.push({ id: item.id, state: 'skipped' });
    originalSkipped(item);
  };
}

function assertAncestorsNeverTerminalMidRun(stamps: readonly StateStamp[], ancestors: Array<[string, string[]]>): void {
  const priority: Record<TestState, number> = {
    running: 6,
    errored: 5,
    failed: 4,
    queued: 3,
    passed: 2,
    skipped: 1
  };
  const terminal = new Set<TestState>(['passed', 'failed', 'errored', 'skipped']);
  const states = new Map<string, TestState>();
  for (const stamp of stamps) {
    states.set(stamp.id, stamp.state);
    for (const [ancestor, leaves] of ancestors) {
      const leafStates = leaves.map((leaf) => states.get(leaf));
      if (leafStates.every((state) => state && terminal.has(state))) {
        continue;
      }
      const rollup = leafStates.reduce<TestState | undefined>((best, state) => {
        if (!state) {
          return best;
        }
        return !best || priority[state] > priority[best] ? state : best;
      }, undefined);
      assert.ok(!rollup || !terminal.has(rollup), `${ancestor} computed terminal ${rollup} before every descendant settled after ${stamp.state} ${stamp.id}`);
    }
  }
}
