import * as assert from 'node:assert/strict';
import * as cp from 'node:child_process';
import * as fs from 'node:fs';
import * as os from 'node:os';
import * as path from 'node:path';
import * as vscode from 'vscode';
import { applyRunEvent, setCurrentTreeForTest } from '../../extension';
import { configRelativePath, currentConfigVersion, discoverTests, runTests } from '../../bridgeAdapter';
import { DiscoveryDocument, RunEvent } from '../../protocol';
import { publishDiscovery } from '../../tree';

suite('Keel Test Bridge expected-red specs', () => {
  // DHF-TEST: keel/requirement-69, keel/requirement-70, keel/requirement-71, keel/requirement-72
  test('expected-red manifest is explicit and visible', () => {
    const entries = readRedlist();
    assert.ok(entries.length > 0, 'expected-red manifest should list the current known-red specs');
    for (const entry of entries) {
      console.warn(`EXPECTED RED ${entry.id}: ${entry.requirement} fixed by ${entry.fixing_cr} - ${entry.reason}`);
    }
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
        const firstLane = firstTree.itemsById.get('keel::lane::lint');
        assert.ok(firstLane);
        firstLane.description = 'terminal result state marker';

        const secondTree = publishDiscovery(controller, root, reconcileDiscovery('second'), 2);
        const secondLane = secondTree.itemsById.get('keel::lane::lint');
        assert.ok(secondLane);
        assert.equal(secondLane, firstLane, 'refresh should preserve the TestItem object for a surviving protocol id');
        assert.equal(secondLane.id, firstLane.id, 'refresh should not mint a generation-prefixed VS Code item id for a surviving protocol id');
        assert.equal(secondLane.description, 'updated by second discovery');
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
  const manifestPath = path.resolve(__dirname, '../../../../testdata/redlist.json');
  const manifest = JSON.parse(fs.readFileSync(manifestPath, 'utf8')) as RedlistManifest;
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

async function expectKnownRed(id: string, body: () => Promise<void> | void): Promise<void> {
  const entry = readRedlist().find((candidate) => candidate.id === id);
  assert.ok(entry, `missing expected-red manifest entry for ${id}`);
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
      }
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
