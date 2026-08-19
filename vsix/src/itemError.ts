import { DiscoveryItem } from './protocol';

/**
 * The separator joining the texts an item's error row carries.
 *
 * `vscode.TestItem.error` is a single value and the platform renders one child
 * row per item whatever it holds (platform fact F19 in
 * `keel/interface_spec-7`), so several qualifying conditions accumulate into
 * one value rather than overwriting each other. A newline is the separator
 * because the row is prose read by a human, not a composed secondary line.
 *
 * DHF-REQ: keel/requirement-140
 */
export const itemErrorSeparator = '\n';

/**
 * Composes the value of `vscode.TestItem.error` for one discovery item.
 *
 * Every `condition` on the item reaches this surface, whatever its kind: a
 * condition is by definition a statement that stands outside any run.
 *
 * The return is `undefined`, never the empty string, when nothing qualifies:
 * an empty value still force-expands the parent, so "no condition" and "an
 * empty condition" must not render the same.
 *
 * DHF-REQ: keel/requirement-140
 */
export function composeItemError(item: DiscoveryItem): string | undefined {
  const texts: string[] = [];
  for (const condition of item.conditions ?? []) {
    if (condition.message) {
      texts.push(condition.message);
    }
  }
  return texts.length > 0 ? texts.join(itemErrorSeparator) : undefined;
}
