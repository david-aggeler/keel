import * as assert from 'node:assert/strict';
import * as fs from 'node:fs';
import * as os from 'node:os';
import * as path from 'node:path';
import * as vscode from 'vscode';
import {
  composeDescription,
  defaultDisplayConfig,
  descriptionSeparator,
  displayClassOrder,
  hasRenderableFacts,
  parseDisplayConfig,
  renderDescription,
  DisplayConfig
} from '../../description';
import { adapterConfig, configRelativePath, currentConfigVersion, defaultConfigTemplate, readAdapterConfig } from '../../bridgeAdapter';
import { DiscoveryItem } from '../../protocol';
import { publishDiscovery } from '../../tree';

interface GoldenCase {
  name: string;
  item: DiscoveryItem;
  display: DisplayConfig;
  expected: string;
  has_facts: boolean;
}

interface GoldenFixture {
  version: number;
  separator: string;
  class_order: string[];
  defaults: { display: DisplayConfig };
  cases: GoldenCase[];
}

function readGoldenFixture(): GoldenFixture {
  const fixturePath = path.resolve(__dirname, '../../../../testdata/description-golden.json');
  return JSON.parse(fs.readFileSync(fixturePath, 'utf8')) as GoldenFixture;
}

function makeWorkspace(prefix: string, config: unknown): string {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), prefix));
  fs.mkdirSync(path.join(root, '.vscode'), { recursive: true });
  fs.writeFileSync(path.join(root, configRelativePath), JSON.stringify(config, null, 2) + '\n');
  return root;
}

suite('description composition', () => {
  // The VSIX half of keel/ac-561. The Go renderer asserts the same committed
  // file, so changing either implementation alone reds a gate.
  // DHF-TEST: keel/requirement-139
  test('req-139 the composer renders every golden fixture case', () => {
    const fixture = readGoldenFixture();
    assert.equal(fixture.version, 1);
    assert.ok(fixture.cases.length > 0, 'golden fixture carries no cases');
    for (const testCase of fixture.cases) {
      assert.equal(renderDescription(testCase.item, testCase.display), testCase.expected, testCase.name);
      assert.equal(hasRenderableFacts(testCase.item), testCase.has_facts, testCase.name);
    }
  });

  // DHF-TEST: keel/requirement-139
  test('req-139 the composer declares the same contract as the fixture', () => {
    const fixture = readGoldenFixture();
    assert.equal(fixture.separator, descriptionSeparator);
    assert.deepEqual(fixture.class_order, displayClassOrder);
    assert.deepEqual(fixture.defaults.display, defaultDisplayConfig());
  });

  // keel/ac-554: field order inside the received object cannot reach the output.
  // DHF-TEST: keel/requirement-139
  test('req-139 reordering the received JSON object changes nothing', () => {
    const forward = JSON.parse(JSON.stringify({
      id: 'keel::lane::a',
      label: 'A',
      kind: 'lane',
      runnable: true,
      profiles: [],
      description: 'prose',
      last_run: { at: '2026-08-18T10:00:00Z', duration_ms: 9800 },
      findings: [{ rule: 'r', severity: 'warning', message: 'm' }]
    })) as DiscoveryItem;
    const reversed = JSON.parse(JSON.stringify({
      findings: [{ rule: 'r', severity: 'warning', message: 'm' }],
      last_run: { at: '2026-08-18T10:00:00Z', duration_ms: 9800 },
      description: 'prose',
      profiles: [],
      runnable: true,
      kind: 'lane',
      label: 'A',
      id: 'keel::lane::a'
    })) as DiscoveryItem;
    assert.equal(
      renderDescription(forward, defaultDisplayConfig()),
      renderDescription(reversed, defaultDisplayConfig())
    );
    assert.equal(renderDescription(forward, defaultDisplayConfig()), 'prose; · last 9.8s; r warning: m');
  });

  // keel/ac-553: the rendered text depends on the facts, never on the producer.
  // DHF-TEST: keel/requirement-139
  test('req-139 two producers emitting identical facts render identical text', () => {
    const facts = (id: string): DiscoveryItem => ({
      id,
      label: 'shared',
      kind: 'lane',
      runnable: true,
      profiles: [],
      description: 'the same prose',
      last_run: { at: '2026-08-18T10:00:00Z', duration_ms: 1500 },
      desired_state_row: { current: 'running', action: 'reuse', active: true },
      findings: [{ rule: 'r', severity: 'error', message: 'm' }]
    });
    const fromKeelDev = composeDescription(facts('keel::lane::a'), defaultDisplayConfig());
    const fromOpenbrainDev = composeDescription(facts('openbrain::lane::a'), defaultDisplayConfig());
    assert.equal(fromKeelDev, fromOpenbrainDev);
    assert.ok(fromKeelDev && fromKeelDev.length > 0);
  });

  // The fallback is retired: with the prose array gone from the wire, an item
  // carrying no typed fact renders nothing whatever producer emitted it. This is
  // asserted per producer shape so a producer that silently stopped emitting
  // typed facts is an observed fact rather than an invisible default.
  // DHF-TEST: keel/requirement-138, keel/requirement-139
  test('req-138 an openbrain-dev item with no typed facts renders nothing, the fallback being retired', () => {
    const item: DiscoveryItem = {
      id: 'openbrain::lane::a',
      label: 'A',
      kind: 'lane',
      runnable: true,
      profiles: []
    };
    assert.equal(hasRenderableFacts(item), false);
    assert.equal(composeDescription(item, defaultDisplayConfig()), undefined);
  });

  // DHF-TEST: keel/requirement-139
  test('req-139 a keel-demo-dev item with no typed facts renders nothing at all', () => {
    const item: DiscoveryItem = { id: 'demo::lane::a', label: 'A', kind: 'lane', runnable: true, profiles: [] };
    assert.equal(hasRenderableFacts(item), false);
    assert.equal(composeDescription(item, defaultDisplayConfig()), undefined);
  });

  // A suppressed class must stay suppressed: there is no prose channel left to
  // resurrect what the toggles removed.
  // DHF-TEST: keel/requirement-138, keel/requirement-139
  test('req-139 a typed item with every class disabled renders nothing', () => {
    const item: DiscoveryItem = {
      id: 'keel::lane::a',
      label: 'A',
      kind: 'lane',
      runnable: true,
      profiles: [],
      description: 'the lane prose',
      last_run: { at: '2026-08-18T10:00:00Z', duration_ms: 9800 }
    };
    assert.equal(composeDescription(item, { description: false, lastRun: false, desiredState: false, findings: false, ordinal: false }), undefined);
  });

  // keel/ac-555 end to end: the published tree carries the composed text, and
  // clearing one toggle removes exactly that class from it.
  // DHF-TEST: keel/requirement-139
  test('req-139 the published tree renders the composed description', () => {
    const controller = vscode.tests.createTestController('keel-description-test', 'Keel description test');
    try {
      const item: DiscoveryItem = {
        id: 'keel::lane::a',
        label: 'A',
        kind: 'lane',
        runnable: true,
        profiles: [],
        description: 'the lane prose',
        last_run: { at: '2026-08-18T10:00:00Z', duration_ms: 9800 },
        findings: [{ rule: 'lane-order', severity: 'warning', message: 'order drifted' }]
      };
      const discovery = { version: 1 as const, workspace: '/tmp', generated_at: '2026-08-18T10:00:00Z', items: [item] };

      const published = publishDiscovery(controller, '/tmp', discovery, 0, defaultDisplayConfig());
      assert.equal(published.itemsById.get('keel::lane::a')?.description, 'the lane prose; · last 9.8s; lane-order warning: order drifted');

      const withoutLastRun = publishDiscovery(controller, '/tmp', discovery, 0, { description: true, lastRun: false, desiredState: true, findings: true, ordinal: false });
      assert.equal(withoutLastRun.itemsById.get('keel::lane::a')?.description, 'the lane prose; lane-order warning: order drifted');
    } finally {
      controller.dispose();
    }
  });
});

suite('display configuration', () => {
  // keel/ac-566.
  // DHF-TEST: keel/requirement-139
  test('req-139 an unknown key inside the display block is rejected at config read', () => {
    const root = makeWorkspace('keel-display-unknown-', {
      version: currentConfigVersion,
      command: 'bin/keel-dev',
      args: [],
      displayName: 'Keel',
      display: { finding: false }
    });
    assert.throws(() => readAdapterConfig(root), /finding/);
  });

  // DHF-TEST: keel/requirement-139
  test('req-139 a non-boolean toggle is rejected at config read', () => {
    const root = makeWorkspace('keel-display-nonbool-', {
      version: currentConfigVersion,
      command: 'bin/keel-dev',
      args: [],
      displayName: 'Keel',
      display: { findings: 'no' }
    });
    assert.throws(() => readAdapterConfig(root), /findings/);
  });

  // DHF-TEST: keel/requirement-139
  test('req-139 an absent display block and an absent key both mean enabled', () => {
    const absent = makeWorkspace('keel-display-absent-', {
      version: currentConfigVersion,
      command: 'bin/keel-dev',
      args: [],
      displayName: 'Keel'
    });
    assert.deepEqual(adapterConfig(absent).display, defaultDisplayConfig());

    const partial = makeWorkspace('keel-display-partial-', {
      version: currentConfigVersion,
      command: 'bin/keel-dev',
      args: [],
      displayName: 'Keel',
      display: { findings: false }
    });
    assert.deepEqual(adapterConfig(partial).display, { description: true, lastRun: true, desiredState: true, findings: false, ordinal: false });
  });

  // DHF-TEST: keel/requirement-139
  test('req-139 parseDisplayConfig names the unknown key it refuses', () => {
    assert.throws(() => parseDisplayConfig({ lastrun: true }), /lastrun/);
    assert.deepEqual(parseDisplayConfig(undefined), defaultDisplayConfig());
    assert.deepEqual(parseDisplayConfig({}), defaultDisplayConfig());
  });

  // DHF-TEST: keel/requirement-59, keel/requirement-139
  test('req-139 the default template states every class enabled at the current version', () => {
    const parsed = JSON.parse(defaultConfigTemplate()) as { version: number; display: DisplayConfig };
    assert.equal(parsed.version, currentConfigVersion);
    assert.deepEqual(parsed.display, defaultDisplayConfig());
  });
});
