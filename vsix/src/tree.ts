import * as path from 'node:path';
import * as vscode from 'vscode';
import { composeDescription, defaultDisplayConfig, DisplayConfig } from './description';
import { DiscoveryCapabilities, DiscoveryDocument, DiscoveryItem } from './protocol';

export interface PublishedTree {
  readonly itemsById: Map<string, vscode.TestItem>;
  readonly protocolIdByItemId: Map<string, string>;
  readonly canonicalIdByAliasId: Map<string, string>;
  readonly aliasesByCanonicalId: Map<string, vscode.TestItem[]>;
  readonly discoveryItemsById: Map<string, DiscoveryItem>;
  readonly parentByItemId: Map<string, vscode.TestItem>;
  readonly capabilities: DiscoveryCapabilities;
  readonly modulePath?: string;
}

export function publishDiscovery(
  controller: vscode.TestController,
  workspaceRoot: string,
  discovery: DiscoveryDocument,
  generation = 0,
  display: DisplayConfig = defaultDisplayConfig()
): PublishedTree {
  void generation;
  const existing = collectExistingItems(controller);
  const itemsById = new Map<string, vscode.TestItem>();
  const protocolIdByItemId = new Map<string, string>();
  const canonicalIdByAliasId = new Map<string, string>();
  const aliasesByCanonicalId = new Map<string, vscode.TestItem[]>();
  const discoveryItemsById = new Map<string, DiscoveryItem>();
  const parentByItemId = new Map<string, vscode.TestItem>();
  const pending = topologicalOrder(discovery.items);
  const emissionIndex = deriveEmissionIndex(discovery.items);

  for (const item of pending) {
    const index = emissionIndex.get(item.id) ?? 0;
    const testItem = existing.itemsById.get(item.id) ?? toTestItem(controller, workspaceRoot, item, display, index);
    updateTestItem(testItem, item, display, index);
    itemsById.set(item.id, testItem);
    discoveryItemsById.set(item.id, item);
    protocolIdByItemId.set(testItem.id, item.id);
    if (item.canonical_id) {
      canonicalIdByAliasId.set(item.id, item.canonical_id);
      const aliases = aliasesByCanonicalId.get(item.canonical_id) ?? [];
      aliases.push(testItem);
      aliasesByCanonicalId.set(item.canonical_id, aliases);
    }

    if (item.parent_id) {
      const parent = itemsById.get(item.parent_id);
      if (parent) {
        parent.children.add(testItem);
        parentByItemId.set(testItem.id, parent);
        continue;
      }
    }
    controller.items.add(testItem);
  }
  deleteMissingItems(controller, existing.parentById, itemsById);

  return {
    itemsById,
    protocolIdByItemId,
    canonicalIdByAliasId,
    aliasesByCanonicalId,
    discoveryItemsById,
    parentByItemId,
    capabilities: discovery.capabilities ?? {},
    modulePath: discovery.module_path
  };
}

// DHF-REQ: keel/requirement-88
export function replacePublishedTestItem(
  controller: vscode.TestController,
  publishedTree: PublishedTree,
  id: string
): vscode.TestItem | undefined {
  const oldItem = publishedTree.itemsById.get(id);
  if (!oldItem) {
    return undefined;
  }
  const replacement = controller.createTestItem(oldItem.id, oldItem.label, oldItem.uri);
  replacement.sortText = oldItem.sortText;
  replacement.canResolveChildren = oldItem.canResolveChildren;
  replacement.description = oldItem.description;
  replacement.tags = oldItem.tags;
  replacement.range = oldItem.range;

  const children: vscode.TestItem[] = [];
  oldItem.children.forEach((child) => children.push(child));
  const parent = publishedTree.parentByItemId.get(id);
  if (parent) {
    parent.children.delete(id);
    parent.children.add(replacement);
  } else {
    controller.items.delete(id);
    controller.items.add(replacement);
  }
  for (const child of children) {
    replacement.children.add(child);
    publishedTree.parentByItemId.set(child.id, replacement);
  }

  publishedTree.itemsById.set(id, replacement);
  const protocolId = publishedTree.protocolIdByItemId.get(id);
  if (protocolId) {
    publishedTree.protocolIdByItemId.set(replacement.id, protocolId);
  }
  const canonicalId = publishedTree.canonicalIdByAliasId.get(id);
  if (canonicalId) {
    const aliases = publishedTree.aliasesByCanonicalId.get(canonicalId) ?? [];
    publishedTree.aliasesByCanonicalId.set(
      canonicalId,
      aliases.map((alias) => alias.id === id ? replacement : alias)
    );
  }
  return replacement;
}

function collectExistingItems(controller: vscode.TestController): {
  itemsById: Map<string, vscode.TestItem>;
  parentById: Map<string, vscode.TestItem>;
} {
  const itemsById = new Map<string, vscode.TestItem>();
  const parentById = new Map<string, vscode.TestItem>();
  const visit = (item: vscode.TestItem, parent?: vscode.TestItem) => {
    itemsById.set(item.id, item);
    if (parent) {
      parentById.set(item.id, parent);
    }
    item.children.forEach((child) => visit(child, item));
  };
  controller.items.forEach((item) => visit(item));
  return { itemsById, parentById };
}

function deleteMissingItems(
  controller: vscode.TestController,
  oldParentById: Map<string, vscode.TestItem>,
  nextItemsById: Map<string, vscode.TestItem>
): void {
  const existingIds = new Set([...oldParentById.keys()]);
  controller.items.forEach((item) => existingIds.add(item.id));
  const deleted = new Set<string>();
  const deleteOne = (id: string) => {
    if (nextItemsById.has(id) || deleted.has(id)) {
      return;
    }
    deleted.add(id);
    const parent = oldParentById.get(id);
    if (parent) {
      parent.children.delete(id);
      return;
    }
    controller.items.delete(id);
  };
  for (const id of existingIds) {
    deleteOne(id);
  }
}

function toTestItem(
  controller: vscode.TestController,
  workspaceRoot: string,
  item: DiscoveryItem,
  display: DisplayConfig,
  emissionIndex: number
): vscode.TestItem {
  const uri = item.uri ? vscode.Uri.file(path.join(workspaceRoot, item.uri)) : undefined;
  const testItem = controller.createTestItem(item.id, item.label, uri);
  updateTestItem(testItem, item, display, emissionIndex);
  return testItem;
}

/**
 * Maps every item to its zero-based index within its parent's emission
 * sequence. The producer's emission order is the one ordering fact: nothing is
 * authored and nothing is parsed back out of a display field, so renumbering
 * costs a producer nothing (keel/ac-546, keel/ac-547).
 *
 * The index is stated as a plain decimal. The platform comparator ends in
 * compareFileNames, backed by Intl.Collator(undefined, {numeric: true}), so 2
 * orders before 10 unpadded (platform fact F21). Comparison only ever runs
 * within one sibling set, so no cross-parent key is needed either.
 *
 * DHF-REQ: keel/requirement-137
 */
export function deriveEmissionIndex(items: readonly DiscoveryItem[]): Map<string, number> {
  const known = new Set(items.map((item) => item.id));
  const nextByParent = new Map<string, number>();
  const indexById = new Map<string, number>();
  for (const item of items) {
    // An unresolved parent_id makes the item a root here exactly as it does in
    // topologicalOrder, so the two walks cannot disagree about who is a sibling.
    const parent = item.parent_id && known.has(item.parent_id) ? item.parent_id : '';
    const index = nextByParent.get(parent) ?? 0;
    nextByParent.set(parent, index + 1);
    indexById.set(item.id, index);
  }
  return indexById;
}

// The extension is the sole composer of the secondary text: the producer
// contributes prose and facts, never order, separator, or format.
//
// DHF-REQ: keel/requirement-70, keel/requirement-139
function updateTestItem(
  testItem: vscode.TestItem,
  item: DiscoveryItem,
  display: DisplayConfig,
  emissionIndex: number
): void {
  testItem.label = item.label;
  testItem.sortText = String(emissionIndex);
  testItem.canResolveChildren = false;
  testItem.description = composeDescription(item, display);
  testItem.tags = item.required_resources?.map((resource) => new vscode.TestTag(resource)) ?? [];
  if (item.range) {
    testItem.range = new vscode.Range(
      item.range.start_line,
      item.range.start_column,
      item.range.end_line,
      item.range.end_column
    );
  } else {
    testItem.range = undefined;
  }
}

// topologicalOrder returns the discovery items in an order where every
// item's parent (if any) appears before it. Uses real parent_id
// adjacency rather than an ID-segment-count heuristic: discovery
// documents that adopt non-prefixed canonical IDs (e.g. flat alias
// items whose parent is a runner root) sort correctly. Items whose
// parent_id does not resolve are surfaced as roots — the controller
// treats them as top-level rather than orphans.
function topologicalOrder(items: readonly DiscoveryItem[]): DiscoveryItem[] {
  const byID = new Map<string, DiscoveryItem>();
  for (const item of items) {
    byID.set(item.id, item);
  }
  const childrenByParent = new Map<string, DiscoveryItem[]>();
  const roots: DiscoveryItem[] = [];
  for (const item of items) {
    if (item.parent_id && byID.has(item.parent_id)) {
      const siblings = childrenByParent.get(item.parent_id) ?? [];
      siblings.push(item);
      childrenByParent.set(item.parent_id, siblings);
      continue;
    }
    roots.push(item);
  }
  const ordered: DiscoveryItem[] = [];
  const queue: DiscoveryItem[] = [...roots];
  while (queue.length > 0) {
    const next = queue.shift() as DiscoveryItem;
    ordered.push(next);
    const children = childrenByParent.get(next.id);
    if (children) {
      queue.push(...children);
    }
  }
  return ordered;
}
