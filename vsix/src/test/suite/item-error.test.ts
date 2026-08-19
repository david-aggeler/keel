import * as assert from 'node:assert/strict';
import * as os from 'node:os';
import * as vscode from 'vscode';
import { DiscoveryDocument, DiscoveryItem } from '../../protocol';
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
  // force-expands the parent for good (platform fact F20 in
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
});
