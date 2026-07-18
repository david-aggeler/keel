import * as assert from 'node:assert/strict';
import * as cp from 'node:child_process';
import * as fs from 'node:fs';
import * as os from 'node:os';
import * as path from 'node:path';
import * as vscode from 'vscode';
import { applyReconcileResultsCapability, applyRunEvent, invalidateClearedResults, resetReconcileSignatureForTest, setCurrentTreeForTest } from '../../extension';
import { configRelativePath, currentConfigVersion, discoverTests, runTests } from '../../bridgeAdapter';
import { DiscoveryDocument, DiscoveryItem, ReconcileResult, RunEvent } from '../../protocol';
import { publishDiscovery } from '../../tree';

suite('Keel Test Bridge expected-red specs', () => {
  // DHF-TEST: keel/requirement-69, keel/requirement-70, keel/requirement-71, keel/requirement-72
  test('expected-red manifest is explicit and visible', () => {
    const entries = readRedlist();
    console.warn(`EXPECTED RED manifest contains ${entries.length} entries`);
    for (const entry of entries) {
      console.warn(`EXPECTED RED ${entry.id}: ${entry.requirement} fixed by ${entry.fixing_cr} - ${entry.reason}`);
    }
  });

  // DHF-TEST: keel/requirement-69, keel/requirement-70, keel/requirement-71, keel/requirement-72
  test('expected-red manifest is shrink-only', () => {
    assertRedlistIsShrinkOnly(readRedlist());
  });

  // DHF-TEST: keel/requirement-69, keel/requirement-70, keel/requirement-71, keel/requirement-72
  test('expected-red manifest allows the empty target state', async () => {
    assert.deepEqual(validateRedlistManifest({ version: 1, entries: [] }), []);
    assertRedlistIsShrinkOnly([]);
    await expectKnownRed('vsix:absent:green-target-state', () => {});
  });

  // DHF-TEST: keel/requirement-69
  test('req-69 real binary discovery emits the approved four-group tree', async function () {
    this.timeout(30_000);
    await expectKnownRed('vsix:req-69:approved-four-group-tree', async () => {
      const root = createRealBridgeWorkspace('keel-req69-');
      try {
        const detect = await collectChild(runTests(root, ['keel::maintenance::detect-lanes']));
        assert.equal(detect.code, 0, detect.stderr + detect.stdout);

        const discovery = await discoverTests(root);
        const top = discovery.items
          .filter((item) => !item.parent_id)
          .sort((a, b) => (a.sort_text ?? '').localeCompare(b.sort_text ?? ''));
        assert.deepEqual(top.map((item) => ({ label: item.label, kind: item.kind })), [
          { label: 'A - Test Bridge Maintenance', kind: 'group' },
          { label: 'B - Desired State', kind: 'group' },
          { label: 'C - Lanes', kind: 'group' },
          { label: 'D - Frameworks', kind: 'group' }
        ]);
        assert.deepEqual(top.map((item) => item.id), [
          'keel::maintenance',
          'keel::desired-state',
          'keel::lanes',
          'keel::frameworks'
        ]);
        for (const item of discovery.items.filter((candidate) => /^[a-z]\.[0-9]+ /i.test(candidate.label))) {
          assert.equal(item.sort_text, zeroPaddedOrdinal(item.label.split(/\s+/, 1)[0]));
          assert.doesNotMatch(item.id, /::[a-z]\.[0-9]+(?:$|::)/i);
        }
      } finally {
        fs.rmSync(root, { recursive: true, force: true });
      }
    });
  });

  // DHF-TEST: keel/requirement-70
  test('req-70 refresh reconciles real TestItems in place by stable id', async () => {
    await expectKnownRed('vsix:req-70:refresh-reconciles-testitems', async () => {
      const controller = vscode.tests.createTestController(`keelReq70-${Date.now()}`, 'Keel Req 70');
      try {
        const root = os.tmpdir();
        const firstTree = publishDiscovery(controller, root, reconcileDiscovery('first'), 1);
        const firstLanes = firstTree.itemsById.get('keel::lanes');
        assert.ok(firstLanes);
        const firstLane = firstTree.itemsById.get('keel::lane::lint');
        assert.ok(firstLane);
        firstLane.description = 'terminal result state marker';

        const secondTree = publishDiscovery(controller, root, reconcileDiscovery('second'), 2);
        const secondLanes = secondTree.itemsById.get('keel::lanes');
        assert.equal(secondLanes, firstLanes, 'refresh should preserve surviving parent TestItem objects');
        const secondLane = secondTree.itemsById.get('keel::lane::lint');
        assert.ok(secondLane);
        assert.equal(secondLane, firstLane, 'refresh should preserve the TestItem object for a surviving protocol id');
        assert.equal(secondLane.id, firstLane.id, 'refresh should not mint a generation-prefixed VS Code item id for a surviving protocol id');
        assert.equal(secondLane.description, 'updated by second discovery');
        assert.ok(secondTree.itemsById.has('keel::lane::unit'), 'new discovery ids should be added under the existing parent');
        assert.equal(secondLanes.children.get('keel::lane::stale'), undefined, 'ids absent from discovery should be deleted from their parent');
      } finally {
        controller.dispose();
      }
    });
  });

  // DHF-TEST: keel/requirement-71
  test('req-71 terminal run state is not replaced by later queued/running events', async () => {
    await expectKnownRed('vsix:req-71:terminal-state-never-reverts', async () => {
      const controller = vscode.tests.createTestController(`keelReq71-${Date.now()}`, 'Keel Req 71');
      let run: vscode.TestRun | undefined;
      try {
        const tree = publishDiscovery(controller, os.tmpdir(), {
          version: 1,
          workspace: 'req-71',
          generated_at: new Date().toISOString(),
          items: [{ id: 'keel::lane::lint', label: 'lint', kind: 'lane', runnable: true, profiles: ['run'] }]
        });
        setCurrentTreeForTest(tree);
        const item = tree.itemsById.get('keel::lane::lint');
        assert.ok(item);
        run = controller.createTestRun(new vscode.TestRunRequest([item]));
        const calls: string[] = [];
        const originalStarted = run.started.bind(run);
        const originalPassed = run.passed.bind(run);
        run.started = (target: vscode.TestItem) => {
          calls.push(`started:${target.id}`);
          originalStarted(target);
        };
        run.passed = (target: vscode.TestItem, duration?: number) => {
          calls.push(`passed:${target.id}`);
          originalPassed(target, duration);
        };
        const selected = new Set([item.id]);
        const results = new Set<string>();

        applyRunEvent(run, JSON.stringify(runEvent({ event: 'test_started', test_id: 'keel::lane::lint' })), selected, results);
        applyRunEvent(run, JSON.stringify(runEvent({ event: 'passed', test_id: 'keel::lane::lint' })), selected, results);
        applyRunEvent(run, JSON.stringify(runEvent({ event: 'test_started', test_id: 'keel::lane::lint' })), selected, results);

        assert.deepEqual(calls, [
          'started:keel::lane::lint',
          'passed:keel::lane::lint'
        ]);
      } finally {
        run?.end();
        setCurrentTreeForTest(undefined);
        controller.dispose();
      }
    });
  });

  // DHF-TEST: keel/requirement-71
  test('req-71 single-item runs do not stamp siblings outside the request scope', async () => {
    await expectKnownRed('vsix:req-71:sibling-results-accumulate', async () => {
      const controller = vscode.tests.createTestController(`keelReq71SiblingScope-${Date.now()}`, 'Keel Req 71 sibling scope');
      let run: vscode.TestRun | undefined;
      try {
        const tree = publishDiscovery(controller, os.tmpdir(), {
          version: 1,
          workspace: 'req-71-sibling-scope',
          generated_at: new Date().toISOString(),
          items: [
            { id: 'keel::lanes', label: 'lanes', kind: 'group', runnable: false, profiles: [] },
            { id: 'keel::lane::a', parent_id: 'keel::lanes', label: 'A', kind: 'lane', runnable: true, profiles: ['run'] },
            { id: 'keel::lane::b', parent_id: 'keel::lanes', label: 'B', kind: 'lane', runnable: true, profiles: ['run'] }
          ]
        });
        setCurrentTreeForTest(tree);
        const parent = tree.itemsById.get('keel::lanes');
        const itemA = tree.itemsById.get('keel::lane::a');
        const itemB = tree.itemsById.get('keel::lane::b');
        assert.ok(parent);
        assert.ok(itemA);
        assert.ok(itemB);
        const terminalStates = new Map<string, string>();
        run = controller.createTestRun(new vscode.TestRunRequest([itemA]));
        recordTerminalStates(run, terminalStates);

        applyRunEvent(
          run,
          JSON.stringify(runEvent({ event: 'passed', test_id: 'keel::lane::a' })),
          new Set([itemA.id]),
          new Set()
        );
        assert.equal(terminalStates.get(itemA.id), 'passed', 'the first single-item run should leave A green');

        run.end();
        run = controller.createTestRun(new vscode.TestRunRequest([itemB]));
        recordTerminalStates(run, terminalStates);

        applyRunEvent(
          run,
          JSON.stringify(runEvent({ event: 'passed', test_id: 'keel::lane::b' })),
          new Set([itemB.id]),
          new Set()
        );

        const leafTerminalStates = [itemA, itemB]
          .map((item) => [item.id, terminalStates.get(item.id)] as const)
          .sort();
        assert.deepEqual(
          leafTerminalStates,
          [
            [itemA.id, 'passed'],
            [itemB.id, 'passed']
          ],
          'single-item runs should accumulate terminal icons without stamping siblings outside the request footprint'
        );

        run.end();
        run = controller.createTestRun(new vscode.TestRunRequest([parent]));
        const parentRunCalls: string[] = [];
        run.passed = (target: vscode.TestItem) => {
          parentRunCalls.push(`passed:${target.id}`);
        };
        run.skipped = (target: vscode.TestItem) => {
          parentRunCalls.push(`skipped:${target.id}`);
        };
        applyRunEvent(
          run,
          JSON.stringify(runEvent({ event: 'passed', test_id: 'keel::lane::b' })),
          new Set([parent.id]),
          new Set()
        );

        assert.ok(parentRunCalls.includes('passed:keel::lane::b'), 'the executed child should receive its terminal result');
        assert.ok(parentRunCalls.includes('skipped:keel::lane::a'), 'an unreported sibling inside a parent run should be skipped');
      } finally {
        run?.end();
        setCurrentTreeForTest(undefined);
        controller.dispose();
      }
    });
  });

  // DHF-TEST: keel/requirement-72
  test('req-72 every discovery-served runnable id resolves through the real binary dry-run sweep', async function () {
    this.timeout(30_000);
    await expectKnownRed('vsix:req-72:runnable-id-dry-run-sweep', async () => {
      const root = createRealBridgeWorkspace('keel-req72-');
      try {
        const detect = await collectChild(runTests(root, ['keel::maintenance::detect-lanes']));
        assert.equal(detect.code, 0, detect.stderr + detect.stdout);
        const discovery = await discoverTests(root);
        const runnableIds = discovery.items.filter((item) => item.runnable).map((item) => item.id);
        assert.ok(runnableIds.length > 0, 'discovery should serve runnable ids');

        const resolved = await collectChild(spawnKeelDev(root, [
          'test-bridge',
          'tests',
          'run',
          '--dry-run',
          ...runnableIds.flatMap((id) => ['--id', id])
        ]));
        assert.equal(resolved.code, 0, resolved.stderr + resolved.stdout);
        assert.doesNotMatch(resolved.stderr + resolved.stdout, /unknown .*id/i);

        const rejected = await collectChild(spawnKeelDev(root, [
          'test-bridge',
          'tests',
          'run',
          '--dry-run',
          '--id',
          'keel::missing::not-served'
        ]));
        assert.notEqual(rejected.code, 0, 'a non-served id should still be rejected');
        assert.match(rejected.stderr + rejected.stdout, /unknown .*id/i);
      } finally {
        fs.rmSync(root, { recursive: true, force: true });
      }
    });
  });

  // req-88: mutually-exclusive group — switching the active member must clear
  // the deactivated sibling's result on a REAL vscode.TestController. This
  // upgrades the mocked-controller assertion in extension.test.ts (which only
  // proves invalidateTestResults is *called*) to the real controller, where
  // invalidateTestResults marks a result OUTDATED rather than removing it — so
  // the sibling keeps its green check (issue-80). VS Code exposes no API to read
  // a rendered result or to clear a single item's result, so the only faithful,
  // observable proxy for "result removed" is that the deactivated sibling's
  // TestItem is rebuilt/removed on the controller (mechanism (b), cr-115).
  //
  // DHF-TEST: keel/requirement-88
  test('req-88 exclusive-group switch clears the deactivated sibling on a real TestController', async () => {
    await expectKnownRed('vsix:req-88:exclusive-real-controller-clear', () => {
      const controller = vscode.tests.createTestController(`keelReq88Exclusive-${Date.now()}`, 'Keel Req 88 exclusive');
      const groupId = 'demo::desired-state::dataset';
      const fullId = 'demo::desired-state::dataset::full';
      const smallId = 'demo::desired-state::dataset::small';
      try {
        const tree = publishDiscovery(controller, os.tmpdir(), {
          version: 1,
          workspace: 'req-88-exclusive',
          generated_at: new Date().toISOString(),
          items: [
            { id: groupId, label: 'app-db data set', kind: 'group', runnable: false, profiles: [] },
            { id: fullId, parent_id: groupId, label: 'full', kind: 'test', runnable: true, profiles: ['run'] },
            { id: smallId, parent_id: groupId, label: 'small', kind: 'test', runnable: true, profiles: ['run'] }
          ]
        });
        setCurrentTreeForTest(tree);

        const memberOnController = (id: string): vscode.TestItem | undefined =>
          controller.items.get(groupId)?.children.get(id);

        // Run 1 — activate the concrete member 'full' on a REAL TestRun, so the
        // pass is stamped on the real controller.
        const fullItem = tree.itemsById.get(fullId);
        assert.ok(fullItem, 'full item is discovered');
        let run = controller.createTestRun(new vscode.TestRunRequest([fullItem]));
        applyRunEvent(run, JSON.stringify(runEvent({ event: 'passed', test_id: fullId })), new Set([fullId]), new Set());
        run.end();

        const fullBefore = memberOnController(fullId);
        assert.ok(fullBefore, 'full is present on the controller after its own run');

        // Run 2 — activate 'small'; the bridge deactivates 'full' with a
        // 'cleared' event and the run loop invalidates it on the controller.
        // This is the exact production sibling-deactivation path.
        const smallItem = tree.itemsById.get(smallId);
        assert.ok(smallItem, 'small item is discovered');
        run = controller.createTestRun(new vscode.TestRunRequest([smallItem]));
        applyRunEvent(run, JSON.stringify(runEvent({ event: 'passed', test_id: smallId })), new Set([smallId]), new Set());
        const smallBefore = memberOnController(smallId);
        assert.ok(smallBefore, 'small is present on the controller after its own run');
        const applied = applyRunEvent(run, JSON.stringify(runEvent({
          event: 'cleared', test_id: fullId, message: 'deactivated by exclusive desired-state selection'
        })), new Set([smallId]), new Set());
        invalidateClearedResults(controller, new Set(applied.clearedResultIds ?? []));
        run.end();

        // Desired (requirement-88 ac-283): after switching to 'small', the
        // deactivated sibling 'full' must have its result actually removed, while
        // the newly-active 'small' and the group parent are retained in place.
        // Today the clear path only calls invalidateTestResults, which marks the
        // result OUTDATED but leaves the same TestItem carrying its green check —
        // so 'full' is the unchanged object and the first assertion fails
        // (issue-80). cr-115 rebuilds/removes only the sibling so its result drops.
        const fullAfter = memberOnController(fullId);
        assert.notStrictEqual(fullAfter, fullBefore,
          'deactivated sibling full must be rebuilt/removed so its result drops; invalidateTestResults only marks it outdated, so the same TestItem persists and keeps its green check (issue-80)');
        // Guard against a "cleared too much" fix: the active member and the group
        // must survive. A mechanism that rebuilds the whole subtree would also drop
        // small's just-stamped result, violating at-most-one-*retained*-result.
        const smallAfter = memberOnController(smallId);
        assert.strictEqual(smallAfter, smallBefore,
          'the newly-active member small must be retained in place (not rebuilt) when a sibling is deactivated');
        assert.ok(controller.items.get(groupId), 'the exclusive group parent must survive the switch');
      } finally {
        setCurrentTreeForTest(undefined);
        controller.dispose();
      }
    });
  });

  // req-97: verbatim reconcile replay — the bridge-served reconcile_results
  // entries are stamped through EXACTLY ONE non-persisted, focus-preserving
  // TestRun per refresh apply, one-to-one and in order; an unchanged list
  // does not re-stamp (signature guard); a changed list stamps again.
  // Behavioral contract only — the object-identity proxy was falsified by
  // owner live validation (persisted results re-associate by id, F14);
  // rendered efficacy is proven by the owner (ac-322).
  //
  // DHF-TEST: keel/requirement-97
  test('req-97 reconcile replay stamps served states through one non-persisted run', async () => {
    await expectKnownRed('vsix:req-97:reconcile-replay', () => {
      const controller = vscode.tests.createTestController(`keelReq97Replay-${Date.now()}`, 'Keel Req 97 replay');
      const groupId = 'demo::desired-state::dataset';
      const fullId = 'demo::desired-state::dataset::full';
      const smallId = 'demo::desired-state::dataset::small';
      const unknownId = 'demo::desired-state::dataset::unknown';
      const ghostId = 'demo::desired-state::dataset::ghost';
      const discovery = (reconcile: ReconcileResult[]): DiscoveryDocument => ({
        version: 1,
        workspace: 'req-97-replay',
        generated_at: new Date().toISOString(),
        capabilities: { reconcile_results: reconcile },
        items: [
          { id: groupId, label: 'app-db data set', kind: 'group', runnable: false, profiles: [] },
          { id: fullId, parent_id: groupId, label: 'full', kind: 'test', runnable: true, profiles: ['run'] },
          { id: smallId, parent_id: groupId, label: 'small', kind: 'test', runnable: true, profiles: ['run'] },
          { id: unknownId, parent_id: groupId, label: 'Unknown State', kind: 'test', runnable: true, profiles: ['run'] }
        ]
      });
      interface ObservedRun {
        name: string | undefined;
        persisted: boolean;
        preserveFocus: boolean;
        includeIds: string[];
        stamps: Array<[string, string]>;
        ended: boolean;
      }
      const observed: ObservedRun[] = [];
      const originalCreateTestRun = controller.createTestRun.bind(controller);
      controller.createTestRun = ((request: vscode.TestRunRequest, name?: string, persist?: boolean) => {
        const run = originalCreateTestRun(request, name, persist);
        const record: ObservedRun = {
          name,
          persisted: run.isPersisted,
          preserveFocus: request.preserveFocus,
          includeIds: (request.include ?? []).map((item) => item.id),
          stamps: [],
          ended: false
        };
        observed.push(record);
        const originalPassed = run.passed.bind(run);
        const originalSkipped = run.skipped.bind(run);
        const originalEnd = run.end.bind(run);
        run.passed = (item: vscode.TestItem, duration?: number) => {
          record.stamps.push([item.id, 'passed']);
          originalPassed(item, duration);
        };
        run.skipped = (item: vscode.TestItem) => {
          record.stamps.push([item.id, 'skipped']);
          originalSkipped(item);
        };
        run.end = () => {
          record.ended = true;
          originalEnd();
        };
        return run;
      }) as typeof controller.createTestRun;

      try {
        resetReconcileSignatureForTest();
        const served: ReconcileResult[] = [
          { test_id: fullId, state: 'passed', message: 'app-db-full is active' },
          { test_id: smallId, state: 'skipped', message: 'not active (app-db-full is active)' },
          { test_id: unknownId, state: 'skipped', message: 'not active (app-db-full is active)' },
          { test_id: ghostId, state: 'skipped', message: 'not in the tree; must be ignored' }
        ];
        const tree = publishDiscovery(controller, os.tmpdir(), discovery(served));
        setCurrentTreeForTest(tree);

        applyReconcileResultsCapability(controller, tree);
        assert.equal(observed.length, 1, 'exactly one reconcile TestRun is created for a served list');
        const first = observed[0];
        assert.equal(first.name, 'desired-state reconcile', 'the reconcile run is named for the Test Results panel');
        assert.equal(first.persisted, false, 'the reconcile run must not persist (persist=false)');
        assert.equal(first.preserveFocus, true, 'the reconcile run must not steal focus');
        assert.deepEqual(first.stamps, [
          [fullId, 'passed'],
          [smallId, 'skipped'],
          [unknownId, 'skipped']
        ], 'stamps replay the served entries verbatim, in order, restricted to tree items');
        assert.deepEqual(first.includeIds.sort(), [fullId, smallId, unknownId].sort(), 'the run request includes exactly the stamped items');
        assert.ok(first.ended, 'the reconcile run is ended in the same apply');

        applyReconcileResultsCapability(controller, tree);
        assert.equal(observed.length, 1, 'an unchanged served list must not re-stamp (signature guard)');

        const flipped: ReconcileResult[] = [
          { test_id: fullId, state: 'skipped', message: 'not active (app-db-small is active)' },
          { test_id: smallId, state: 'passed', message: 'app-db-small is active' },
          { test_id: unknownId, state: 'skipped', message: 'not active (app-db-small is active)' }
        ];
        const refreshed = publishDiscovery(controller, os.tmpdir(), discovery(flipped));
        setCurrentTreeForTest(refreshed);
        applyReconcileResultsCapability(controller, refreshed);
        assert.equal(observed.length, 2, 'a changed served list stamps again');
        assert.deepEqual(observed[1].stamps, [
          [fullId, 'skipped'],
          [smallId, 'passed'],
          [unknownId, 'skipped']
        ], 'the second run replays the flipped states verbatim');
      } finally {
        controller.createTestRun = originalCreateTestRun;
        resetReconcileSignatureForTest();
        setCurrentTreeForTest(undefined);
        controller.dispose();
      }
    });
  });
});

interface RedlistManifest {
  version: 1;
  entries: RedlistEntry[];
}

interface RedlistEntry {
  id: string;
  requirement: string;
  fixing_cr: string;
  reason: string;
}

function readRedlist(): RedlistEntry[] {
  const manifest = readRedlistManifest('redlist.json');
  const entries = validateRedlistManifest(manifest);
  assertRedlistIsShrinkOnly(entries);
  return entries;
}

function readRedlistManifest(fileName: string): RedlistManifest {
  const manifestPath = path.resolve(__dirname, '../../../../testdata', fileName);
  const manifest = JSON.parse(fs.readFileSync(manifestPath, 'utf8')) as RedlistManifest;
  return manifest;
}

function validateRedlistManifest(manifest: RedlistManifest): RedlistEntry[] {
  assert.equal(manifest.version, 1);
  const seen = new Set<string>();
  for (const entry of manifest.entries) {
    assert.ok(entry.id, 'redlist entry id is required');
    assert.match(entry.requirement, /^keel\/requirement-[0-9]+$/);
    assert.match(entry.fixing_cr, /^keel\/change_request-[0-9]+$/);
    assert.ok(entry.reason, `redlist entry ${entry.id} reason is required`);
    assert.ok(!seen.has(entry.id), `duplicate redlist entry id ${entry.id}`);
    seen.add(entry.id);
  }
  return manifest.entries;
}

function assertRedlistIsShrinkOnly(entries: RedlistEntry[]): void {
  const baseline = new Map(validateRedlistManifest(readRedlistManifest('redlist.baseline.json')).map((entry) => [entry.id, entry]));
  for (const entry of entries) {
    const baselineEntry = baseline.get(entry.id);
    assert.ok(
      baselineEntry,
      `expected-red entry ${entry.id} is not in the authorized baseline; new known-red entries need a new approved record`
    );
    assert.equal(entry.requirement, baselineEntry.requirement, `expected-red entry ${entry.id} requirement changed`);
    assert.equal(entry.fixing_cr, baselineEntry.fixing_cr, `expected-red entry ${entry.id} fixing_cr changed`);
  }
}

async function expectKnownRed(id: string, body: () => Promise<void> | void): Promise<void> {
  const entry = readRedlist().find((candidate) => candidate.id === id);
  if (!entry) {
    await body();
    return;
  }
  try {
    await body();
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error);
    console.warn(`EXPECTED RED ${id}: ${entry.requirement} fixed by ${entry.fixing_cr}: ${message}`);
    return;
  }
  throw new assert.AssertionError({
    message: `known-red spec ${id} passed; remove its redlist entry and let the gate enforce it as green`
  });
}

function createRealBridgeWorkspace(prefix: string): string {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), prefix));
  fs.mkdirSync(path.join(root, '.vscode'), { recursive: true });
  fs.writeFileSync(path.join(root, 'go.mod'), 'module github.com/david-aggeler/keel\n\ngo 1.25\n');
  fs.writeFileSync(path.join(root, 'go.sum'), '');
  fs.writeFileSync(path.join(root, configRelativePath), JSON.stringify({
    version: currentConfigVersion,
    command: realKeelDevBinary(),
    args: [],
    displayName: 'Keel'
  }, null, 2) + '\n');
  return root;
}

function reconcileDiscovery(labelSuffix: string): DiscoveryDocument {
  const changingLane: DiscoveryItem = labelSuffix === 'first'
    ? {
      id: 'keel::lane::stale',
      parent_id: 'keel::lanes',
      label: 'stale',
      sort_text: 'c.002',
      kind: 'lane',
      runnable: true,
      profiles: ['run']
    }
    : {
      id: 'keel::lane::unit',
      parent_id: 'keel::lanes',
      label: 'unit',
      sort_text: 'c.002',
      kind: 'lane',
      runnable: true,
      profiles: ['run']
    };
  return {
    version: 1,
    workspace: `req-70-${labelSuffix}`,
    generated_at: new Date().toISOString(),
    items: [
      { id: 'keel::lanes', label: 'C - Lanes', kind: 'group', runnable: false, profiles: [] },
      {
        id: 'keel::lane::lint',
        parent_id: 'keel::lanes',
        label: 'lint',
        sort_text: 'c.001',
        kind: 'lane',
        runnable: true,
        profiles: ['run'],
        limitations: [`updated by ${labelSuffix} discovery`]
      },
      changingLane
    ]
  };
}

function zeroPaddedOrdinal(ordinal: string): string {
  return ordinal
    .split('.')
    .map((segment, index) => index === 0 ? segment.toLowerCase() : segment.padStart(3, '0'))
    .join('.');
}

function realKeelDevBinary(): string {
  const exe = process.platform === 'win32' ? 'keel-dev.exe' : 'keel-dev';
  return path.resolve(__dirname, '../../../../bin', exe);
}

function spawnKeelDev(root: string, args: string[]): cp.ChildProcessWithoutNullStreams {
  return cp.spawn(realKeelDevBinary(), args, { cwd: root });
}

function runEvent(partial: Partial<RunEvent>): RunEvent {
  return {
    version: 1,
    event: 'output',
    time: new Date().toISOString(),
    ...partial
  } as RunEvent;
}

function recordTerminalStates(run: vscode.TestRun, states: Map<string, string>): void {
  const originalPassed = run.passed.bind(run);
  const originalFailed = run.failed.bind(run);
  const originalErrored = run.errored.bind(run);
  const originalSkipped = run.skipped.bind(run);
  run.passed = (target: vscode.TestItem, duration?: number) => {
    states.set(target.id, 'passed');
    originalPassed(target, duration);
  };
  run.failed = (target: vscode.TestItem, message: vscode.TestMessage | readonly vscode.TestMessage[], duration?: number) => {
    states.set(target.id, 'failed');
    originalFailed(target, message, duration);
  };
  run.errored = (target: vscode.TestItem, message: vscode.TestMessage | readonly vscode.TestMessage[], duration?: number) => {
    states.set(target.id, 'errored');
    originalErrored(target, message, duration);
  };
  run.skipped = (target: vscode.TestItem) => {
    states.set(target.id, 'skipped');
    originalSkipped(target);
  };
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
