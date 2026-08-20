import * as assert from 'node:assert/strict';
import * as os from 'node:os';
import * as vscode from 'vscode';
import { applyRunEvent, setCurrentTreeForTest } from '../../extension';
import { DiscoveryDocument, DiscoveryItem, RunEvent } from '../../protocol';
import { publishDiscovery } from '../../tree';

function documentOf(...items: DiscoveryItem[]): DiscoveryDocument {
  return {
    version: 1,
    workspace: 'item-error',
    generated_at: new Date().toISOString(),
    items
  };
}

let controllerSeq = 0;

function withController(run: (controller: vscode.TestController) => void): void {
  controllerSeq += 1;
  const controller = vscode.tests.createTestController(`keelItemError-${process.pid}-${controllerSeq}`, 'Keel Item Error');
  try {
    run(controller);
  } finally {
    controller.dispose();
  }
}

function errorTextOf(item: vscode.TestItem | undefined): string | undefined {
  const error = item?.error;
  if (error === undefined) {
    return undefined;
  }
  return typeof error === 'string' ? error : error.value;
}

suite('persistent conditions route to TestItem.error', () => {
  // keel/ac-557: a discovery-time parse failure reaches the platform's own slot
  // for a discovery error, and nothing of it reaches the description.
  // DHF-TEST: keel/requirement-140
  test('req-140 a persistent condition becomes the item error and not its description', () => {
    withController((controller) => {
      const tree = publishDiscovery(controller, os.tmpdir(), documentOf({
        id: 'go::file::broken/broken_test.go',
        label: 'broken_test.go',
        kind: 'file',
        runnable: false,
        profiles: [],
        conditions: [{ kind: 'parse_error', message: 'broken/broken_test.go:3:18: expected declaration' }]
      }));
      const item = tree.itemsById.get('go::file::broken/broken_test.go');
      assert.equal(errorTextOf(item), 'broken/broken_test.go:3:18: expected declaration');
      assert.equal(item?.description, undefined);
    });
  });

  // An item carrying no condition must leave the slot untouched: setting it
  // force-expands the parent for good (the error row's structural cost in
  // keel/interface_spec-7), so an empty value is not the same as no value.
  // DHF-TEST: keel/requirement-140
  test('req-140 an item carrying no condition carries no error', () => {
    withController((controller) => {
      const tree = publishDiscovery(controller, os.tmpdir(), documentOf({
        id: 'go::file::ok_test.go',
        label: 'ok_test.go',
        kind: 'file',
        runnable: true,
        profiles: ['run'],
        description: 'the prose channel'
      }));
      const item = tree.itemsById.get('go::file::ok_test.go');
      assert.equal(errorTextOf(item), undefined);
      assert.equal(item?.description, 'the prose channel');
    });
  });

  // keel/ac-559, negative half first: a warning-severity finding must not reach
  // the error surface. Every item carrying one would otherwise force-expand its
  // parent at every refresh (the error row's structural cost).
  // DHF-TEST: keel/requirement-140
  test('req-140 a warning-severity finding stays in the description and raises no error row', () => {
    withController((controller) => {
      const tree = publishDiscovery(controller, os.tmpdir(), documentOf({
        id: 'keel::lane::warned',
        label: 'warned',
        kind: 'lane',
        runnable: true,
        profiles: ['run'],
        findings: [{ rule: 'lane-order', severity: 'warning', message: 'order drifted' }]
      }));
      const item = tree.itemsById.get('keel::lane::warned');
      assert.equal(errorTextOf(item), undefined);
      assert.equal(item?.description, 'lane-order warning: order drifted');
    });
  });

  // keel/ac-559, positive half: the typed severity selects the surface, and the
  // text reaches exactly one of them.
  // DHF-TEST: keel/requirement-140
  test('req-140 an error-severity finding reaches the error row and not the description', () => {
    withController((controller) => {
      const tree = publishDiscovery(controller, os.tmpdir(), documentOf(
        {
          id: 'keel::lane::blocked',
          label: 'blocked',
          kind: 'lane',
          runnable: true,
          profiles: ['run'],
          findings: [{ rule: 'lane-prereq', severity: 'error', message: 'resource is unavailable' }]
        },
        {
          id: 'keel::lane::warned',
          label: 'warned',
          kind: 'lane',
          runnable: true,
          profiles: ['run'],
          findings: [{ rule: 'lane-order', severity: 'warning', message: 'order drifted' }]
        }
      ));
      const blocked = tree.itemsById.get('keel::lane::blocked');
      const warned = tree.itemsById.get('keel::lane::warned');
      assert.equal(errorTextOf(blocked), 'lane-prereq error: resource is unavailable');
      assert.equal(blocked?.description, undefined);
      assert.equal(errorTextOf(warned), undefined);
      assert.equal(warned?.description, 'lane-order warning: order drifted');
    });
  });

  // keel/ac-567: TestItem.error is a single value, so two qualifying conditions
  // on one item must accumulate into it. A dropped condition here is invisible —
  // the item still shows an error row, so nothing signals the second problem.
  // DHF-TEST: keel/requirement-140
  test('req-140 several persistent conditions on one item accumulate into one error value', () => {
    withController((controller) => {
      const tree = publishDiscovery(controller, os.tmpdir(), documentOf({
        id: 'keel::lane::doubly-blocked',
        label: 'doubly blocked',
        kind: 'lane',
        runnable: true,
        profiles: ['run'],
        conditions: [{ kind: 'prerequisite_unsatisfied', message: 'lane blocked: demo-database' }],
        findings: [{ rule: 'lane-prereq', severity: 'error', message: 'resource is unavailable' }]
      }));
      const text = errorTextOf(tree.itemsById.get('keel::lane::doubly-blocked'));
      assert.ok(text?.includes('lane blocked: demo-database'), `error text ${String(text)} dropped the condition`);
      assert.ok(text?.includes('lane-prereq error: resource is unavailable'), `error text ${String(text)} dropped the finding`);
    });
  });

  // Two conditions of the same kind accumulate for the same reason.
  // DHF-TEST: keel/requirement-140
  test('req-140 two conditions on one item both survive', () => {
    withController((controller) => {
      const tree = publishDiscovery(controller, os.tmpdir(), documentOf({
        id: 'go::file::broken/broken_test.go',
        label: 'broken_test.go',
        kind: 'file',
        runnable: false,
        profiles: [],
        conditions: [
          { kind: 'parse_error', message: 'expected declaration' },
          { kind: 'prerequisite_unsatisfied', message: 'go toolchain is unavailable' }
        ]
      }));
      assert.equal(
        errorTextOf(tree.itemsById.get('go::file::broken/broken_test.go')),
        'expected declaration\ngo toolchain is unavailable'
      );
    });
  });

  // A republished tree must clear a condition that the producer stopped
  // emitting: a stale error row outlives the condition it reports otherwise.
  // DHF-TEST: keel/requirement-140
  test('req-140 a resolved condition clears the item error on the next publish', () => {
    withController((controller) => {
      const broken: DiscoveryItem = {
        id: 'go::file::broken/broken_test.go',
        label: 'broken_test.go',
        kind: 'file',
        runnable: false,
        profiles: [],
        conditions: [{ kind: 'parse_error', message: 'expected declaration' }]
      };
      publishDiscovery(controller, os.tmpdir(), documentOf(broken));
      const tree = publishDiscovery(controller, os.tmpdir(), documentOf({
        ...broken,
        runnable: true,
        profiles: ['run'],
        conditions: undefined
      }));
      assert.equal(errorTextOf(tree.itemsById.get('go::file::broken/broken_test.go')), undefined);
    });
  });

  // keel/ac-568: the third leg of the taxonomy, asserted from its own side. A
  // run that cannot execute at all stamps run.errored on the item hosting the
  // run — adopting TestItem.error for the persistent conditions above must not
  // annex the run-scoped one. Without this assertion the boundary is stated
  // from one direction only.
  // DHF-TEST: keel/requirement-140
  test('req-140 a run that cannot execute errors on its item and leaves TestItem.error untouched', () => {
    withController((controller) => {
      const tree = publishDiscovery(controller, os.tmpdir(), documentOf({
        id: 'keel::lane::go-fail',
        label: 'real Go fail',
        kind: 'lane',
        runnable: true,
        profiles: ['run']
      }));
      setCurrentTreeForTest(tree);
      try {
        const errored: string[] = [];
        const failed: string[] = [];
        const run = {
          started() { /* no-op */ },
          passed() { /* no-op */ },
          failed(item: vscode.TestItem) { failed.push(item.id); },
          errored(item: vscode.TestItem) { errored.push(item.id); },
          skipped() { /* no-op */ },
          appendOutput() { /* no-op */ }
        };
        const event: RunEvent = {
          version: 1,
          event: 'errored',
          time: new Date().toISOString(),
          test_id: 'keel::lane::go-fail',
          message: 'lane blocked: keel::lane::go-fail'
        };
        applyRunEvent(
          run as unknown as vscode.TestRun,
          JSON.stringify(event),
          new Set(['keel::lane::go-fail']),
          new Set<string>()
        );

        assert.deepEqual(errored, ['keel::lane::go-fail']);
        assert.deepEqual(failed, []);
        // The run-scoped condition stays run-scoped: nothing about it reaches
        // the persistent surface, which the item never carried.
        assert.equal(errorTextOf(tree.itemsById.get('keel::lane::go-fail')), undefined);
      } finally {
        setCurrentTreeForTest(undefined);
      }
    });
  });
});
