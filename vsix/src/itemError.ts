import { formatFinding, isErrorSeverity } from './description';
import { DiscoveryItem } from './protocol';

/**
 * The separator joining the texts an item's error row carries.
 *
 * `vscode.TestItem.error` is a single value and the platform renders one child
 * row per item whatever it holds (the error slot is not a result in
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
 * Two carriers reach this surface, and for the same reason — both state a
 * condition that stands outside any run:
 *
 * - every `condition` on the item, whatever its kind; and
 * - every `finding` whose typed severity is `error`.
 *
 * A warning-severity finding is deliberately absent. An item carrying `error`
 * gains a permanent child row that sorts first and force-expands its parent
 * (the error row's structural cost), which is proportionate for a condition
 * that blocks the item and disproportionate for a warning — so a warning stays
 * in the composed description alone (keel/ac-559).
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
  for (const finding of item.findings ?? []) {
    if (!isErrorSeverity(finding)) {
      continue;
    }
    const text = formatFinding(finding);
    if (text) {
      texts.push(text);
    }
  }
  return texts.length > 0 ? texts.join(itemErrorSeparator) : undefined;
}
