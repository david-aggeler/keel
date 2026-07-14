import * as path from 'node:path';
import * as vscode from 'vscode';
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
  generation = 0
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

  for (const item of pending) {
    const testItem = existing.itemsById.get(item.id) ?? toTestItem(controller, workspaceRoot, item);
    updateTestItem(testItem, item);
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

function toTestItem(controller: vscode.TestController, workspaceRoot: string, item: DiscoveryItem): vscode.TestItem {
  const uri = item.uri ? vscode.Uri.file(path.join(workspaceRoot, item.uri)) : undefined;
  const testItem = controller.createTestItem(item.id, item.label, uri);
  updateTestItem(testItem, item);
  return testItem;
}

// DHF-REQ: keel/requirement-70
function updateTestItem(testItem: vscode.TestItem, item: DiscoveryItem): void {
  testItem.label = item.label;
  testItem.sortText = item.sort_text;
  testItem.canResolveChildren = false;
  testItem.description = item.limitations?.join('; ');
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
