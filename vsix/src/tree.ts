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
  controller.items.replace([]);
  const itemsById = new Map<string, vscode.TestItem>();
  const protocolIdByItemId = new Map<string, string>();
  const canonicalIdByAliasId = new Map<string, string>();
  const aliasesByCanonicalId = new Map<string, vscode.TestItem[]>();
  const discoveryItemsById = new Map<string, DiscoveryItem>();
  const parentByItemId = new Map<string, vscode.TestItem>();
  const pending = topologicalOrder(discovery.items);

  for (const item of pending) {
    const testItem = toTestItem(controller, workspaceRoot, item, generation);
    testItem.canResolveChildren = false;
    testItem.description = item.limitations?.join('; ');
    testItem.tags = item.required_resources?.map((resource) => new vscode.TestTag(resource)) ?? [];
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

function toTestItem(controller: vscode.TestController, workspaceRoot: string, item: DiscoveryItem, generation: number): vscode.TestItem {
  const uri = item.uri ? vscode.Uri.file(path.join(workspaceRoot, item.uri)) : undefined;
  const testItem = controller.createTestItem(vscodeItemID(item.id, generation), item.label, uri);
  testItem.sortText = item.sort_text;
  if (item.range) {
    testItem.range = new vscode.Range(
      item.range.start_line,
      item.range.start_column,
      item.range.end_line,
      item.range.end_column
    );
  }
  return testItem;
}

function vscodeItemID(protocolID: string, generation: number): string {
  if (generation <= 0) {
    return protocolID;
  }
  return `keel-v${generation}::${protocolID}`;
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
