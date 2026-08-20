import * as assert from 'node:assert/strict';
import * as os from 'node:os';
import * as vscode from 'vscode';
import { defaultDisplayConfig, parseDisplayConfig } from '../../description';
import { DiscoveryDocument, DiscoveryItem } from '../../protocol';
import { deriveEmissionIndex, publishDiscovery } from '../../tree';

function laneDocument(order: string[]): DiscoveryDocument {
  const items: DiscoveryItem[] = [
    { id: 'keel::lanes', label: 'C - Lanes', kind: 'group', runnable: false, profiles: [] }
  ];
  for (const id of order) {
    items.push({
      id: `keel::lane::${id}`,
      parent_id: 'keel::lanes',
      label: id,
      kind: 'lane',
      runnable: true,
      profiles: ['run']
    });
  }
  return {
    version: 1,
    workspace: 'emission-order',
    generated_at: new Date().toISOString(),
    items
  };
}

function sortTextByID(tree: ReturnType<typeof publishDiscovery>, ids: string[]): string[] {
  return ids.map((id) => tree.itemsById.get(id)?.sortText ?? '<missing>');
}

suite('emission-order sort keys', () => {
  // keel/ac-546: the key is the decimal emission index within the parent, and
  // no authored ordinal string is read from any wire field — there is no longer
  // one to read.
  // DHF-TEST: keel/requirement-137
  test('req-137 sortText is the decimal emission index within the parent', () => {
    const controller = vscode.tests.createTestController(`keelOrder-${process.pid}-a`, 'Keel Order A');
    try {
      const tree = publishDiscovery(controller, os.tmpdir(), laneDocument(['x', 'y', 'z']));
      assert.deepEqual(
        sortTextByID(tree, ['keel::lane::x', 'keel::lane::y', 'keel::lane::z']),
        ['0', '1', '2']
      );
      // The group is the document's first root, so it is index 0 of the root
      // sequence rather than a letter.
      assert.equal(tree.itemsById.get('keel::lanes')?.sortText, '0');
    } finally {
      controller.dispose();
    }
  });

  // keel/ac-546 again, on the numeric-collation claim: the key is unpadded
  // because the platform comparator collates numerically (the sibling comparator),
  // so a ten-sibling parent still keeps 2 before 10.
  // DHF-TEST: keel/requirement-137
  test('req-137 the derived key is a plain unpadded decimal', () => {
    const ids = Array.from({ length: 11 }, (unused, index) => `lane${index}`);
    const derived = deriveEmissionIndex(laneDocument(ids).items);
    assert.equal(derived.get('keel::lane::lane1'), 1);
    assert.equal(derived.get('keel::lane::lane10'), 10);
    assert.equal(String(derived.get('keel::lane::lane1')), '1');
  });

  // keel/ac-547: reordering the emission sequence is the only edit a consumer
  // devtool makes to reorder its unlocated siblings — every id and label is
  // byte-identical to the previous document.
  // DHF-TEST: keel/requirement-137
  test('req-137 reordering the emission sequence reorders nothing else', () => {
    const controller = vscode.tests.createTestController(`keelOrder-${process.pid}-b`, 'Keel Order B');
    try {
      const before = publishDiscovery(controller, os.tmpdir(), laneDocument(['x', 'y', 'z']));
      const beforeLabels = sortTextByID(before, ['keel::lane::x']).length;
      assert.equal(beforeLabels, 1);
      const after = publishDiscovery(controller, os.tmpdir(), laneDocument(['z', 'x', 'y']));
      assert.deepEqual(
        sortTextByID(after, ['keel::lane::z', 'keel::lane::x', 'keel::lane::y']),
        ['0', '1', '2']
      );
      for (const id of ['keel::lane::x', 'keel::lane::y', 'keel::lane::z']) {
        assert.equal(after.itemsById.get(id)?.id, before.itemsById.get(id)?.id);
        assert.equal(after.itemsById.get(id)?.label, before.itemsById.get(id)?.label);
      }
    } finally {
      controller.dispose();
    }
  });

  // keel/ac-560: two siblings that share a uri and each carry a range are
  // ordered by the platform on range.startLineNumber before it reads sortText
  // (the sibling comparator). The extension must not defeat that: it forwards both
  // ranges verbatim and still derives the emission key, leaving the decision
  // where the platform makes it.
  // DHF-TEST: keel/requirement-137
  test('req-137 located siblings keep their ranges whatever their emission index', () => {
    const controller = vscode.tests.createTestController(`keelOrder-${process.pid}-c`, 'Keel Order C');
    try {
      const document: DiscoveryDocument = {
        version: 1,
        workspace: 'located-siblings',
        generated_at: new Date().toISOString(),
        items: [
          { id: 'go::file::a', label: 'a_test.go', kind: 'file', runnable: false, profiles: [] },
          {
            id: 'go::test::later',
            parent_id: 'go::file::a',
            label: 'TestLater',
            kind: 'test',
            uri: 'a_test.go',
            range: { start_line: 40, start_column: 0, end_line: 41, end_column: 0 },
            runnable: true,
            profiles: ['run']
          },
          {
            id: 'go::test::earlier',
            parent_id: 'go::file::a',
            label: 'TestEarlier',
            kind: 'test',
            uri: 'a_test.go',
            range: { start_line: 10, start_column: 0, end_line: 11, end_column: 0 },
            runnable: true,
            profiles: ['run']
          }
        ]
      };
      const tree = publishDiscovery(controller, os.tmpdir(), document);
      // Emission order decides the derived key; source position outranks it in
      // the platform sorter, so both items must keep their range and their uri.
      assert.deepEqual(sortTextByID(tree, ['go::test::later', 'go::test::earlier']), ['0', '1']);
      assert.equal(tree.itemsById.get('go::test::later')?.range?.start.line, 40);
      assert.equal(tree.itemsById.get('go::test::earlier')?.range?.start.line, 10);
      assert.ok(tree.itemsById.get('go::test::later')?.uri);
      assert.equal(
        tree.itemsById.get('go::test::later')?.uri?.fsPath,
        tree.itemsById.get('go::test::earlier')?.uri?.fsPath
      );
    } finally {
      controller.dispose();
    }
  });

  // keel/ac-562: the ordinal prefix is a rendering option derived from the
  // emission index. Enabled, every child label carries it; disabled, the same
  // publish renders each label with no prefix and no other difference.
  // DHF-TEST: keel/requirement-137
  test('req-137 the ordinal prefix renders only when its display toggle is enabled', () => {
    const off = vscode.tests.createTestController(`keelOrdinal-${process.pid}-off`, 'Keel Ordinal Off');
    const on = vscode.tests.createTestController(`keelOrdinal-${process.pid}-on`, 'Keel Ordinal On');
    try {
      const document = laneDocument(['lint', 'test-fast']);
      const plain = publishDiscovery(off, os.tmpdir(), document, 0, defaultDisplayConfig());
      const prefixed = publishDiscovery(on, os.tmpdir(), document, 0, parseDisplayConfig({ ordinal: true }));

      assert.equal(plain.itemsById.get('keel::lane::lint')?.label, 'lint');
      assert.equal(plain.itemsById.get('keel::lane::test-fast')?.label, 'test-fast');
      // The lanes group is the document's first root, so its frame letter is A
      // and its children number from one.
      assert.equal(prefixed.itemsById.get('keel::lane::lint')?.label, 'A.1 lint');
      assert.equal(prefixed.itemsById.get('keel::lane::test-fast')?.label, 'A.2 test-fast');
      // A root names its own frame position already; it gains no prefix.
      assert.equal(prefixed.itemsById.get('keel::lanes')?.label, 'C - Lanes');
      // Nothing else differs.
      assert.equal(
        prefixed.itemsById.get('keel::lane::lint')?.sortText,
        plain.itemsById.get('keel::lane::lint')?.sortText
      );
      assert.equal(
        prefixed.itemsById.get('keel::lane::lint')?.description,
        plain.itemsById.get('keel::lane::lint')?.description
      );
    } finally {
      off.dispose();
      on.dispose();
    }
  });

  // The toggle is off unless a workspace asks for it, which is what makes the
  // lost prefix a stated change rather than a silent one (keel/ac-562).
  // DHF-TEST: keel/requirement-137
  test('req-137 the ordinal toggle defaults off and is refused when misspelled', () => {
    assert.equal(defaultDisplayConfig().ordinal, false);
    assert.equal(parseDisplayConfig({}).ordinal, false);
    assert.equal(parseDisplayConfig({ ordinal: true }).ordinal, true);
    assert.throws(() => parseDisplayConfig({ ordinals: true }), /ordinals/);
  });
});
