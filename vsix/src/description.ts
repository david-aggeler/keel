import { DiscoveryItem, Finding, LastRunFacts } from './protocol';

/**
 * The separator joining the segments of a composed item description.
 *
 * This module mirrors the canonical Go renderer in `keel/vscode`
 * (`vscode/description.go`). The Go copy is canonical because the facts are
 * defined in Go and the core gate stays node-free, so `keel-dev vscode lanes
 * list` can never call this composer. The committed golden fixture at
 * `testdata/description-golden.json` is asserted by both sides, which is what
 * makes this second copy pinned rather than accidental (keel/ac-561).
 *
 * DHF-REQ: keel/requirement-139
 */
export const descriptionSeparator = '; ';

export type DisplayClass = 'description' | 'lastRun' | 'desiredState' | 'findings';

/** The fixed sequence the fact classes render in. No producer can influence it. */
export const displayClassOrder: DisplayClass[] = ['description', 'lastRun', 'desiredState', 'findings'];

/**
 * `ordinal` is a display toggle rather than a description segment: it governs
 * the ordinal prefix rendered onto a label, derived at render time from the
 * item's emission index. It is therefore absent from displayClassOrder, and it
 * is the one toggle whose default is off — the shipped tree loses a prefix it
 * used to show, and that change is deliberate (keel/ac-562).
 *
 * DHF-REQ: keel/requirement-137
 */
export type DisplayToggle = DisplayClass | 'ordinal';

/** Every toggle the display block accepts. */
export const displayToggles: DisplayToggle[] = [...displayClassOrder, 'ordinal'];

export interface DisplayConfig {
  description: boolean;
  lastRun: boolean;
  desiredState: boolean;
  findings: boolean;
  ordinal: boolean;
}

/**
 * Every fact class enabled and the label ordinal off — the meaning of an absent
 * `display` block. The asymmetry is deliberate: an absent description class
 * must not hide text a workspace already saw, while the ordinal prefix is the
 * visible change keel/requirement-137 makes and a workspace opts back into it.
 */
export function defaultDisplayConfig(): DisplayConfig {
  return { description: true, lastRun: true, desiredState: true, findings: true, ordinal: false };
}

/**
 * Decodes the `display` block of .vscode/test-bridge.json. An absent block, and
 * an absent key inside a present block, both mean enabled, so upgrading a
 * workspace hides nothing that was visible beforehand. An unknown or
 * non-boolean key is refused by name rather than ignored: a silently ignored
 * toggle reads to a user as a broken feature rather than as a typo
 * (keel/ac-566).
 *
 * DHF-REQ: keel/requirement-139
 */
export function parseDisplayConfig(raw: unknown): DisplayConfig {
  const display = defaultDisplayConfig();
  if (raw === undefined || raw === null) {
    return display;
  }
  if (typeof raw !== 'object' || Array.isArray(raw)) {
    throw new Error('test bridge config display must be an object');
  }
  for (const [key, value] of Object.entries(raw as Record<string, unknown>)) {
    if (!isDisplayToggle(key)) {
      throw new Error(`test bridge config display has unknown class "${key}"; known classes are ${displayToggles.join(', ')}`);
    }
    if (typeof value !== 'boolean') {
      throw new Error(`test bridge config display class "${key}" must be a boolean`);
    }
    display[key] = value;
  }
  return display;
}

function isDisplayToggle(key: string): key is DisplayToggle {
  return (displayToggles as string[]).includes(key);
}

/**
 * Composes the value of `vscode.TestItem.description` for one discovery item.
 *
 * Every item renders through the renderer alone. The legacy `limitations` prose
 * channel is retired, so there is no fallback left to resurrect text a toggle
 * suppressed: an item carrying no renderable fact renders nothing.
 *
 * DHF-REQ: keel/requirement-138, keel/requirement-139
 */
export function composeDescription(item: DiscoveryItem, display: DisplayConfig): string | undefined {
  return renderDescription(item, display) || undefined;
}

/**
 * The mirror of the canonical Go renderer. It reads the item's fields by name,
 * so the order the producer wrote them in the JSON object cannot reach the
 * output (keel/ac-554).
 *
 * DHF-REQ: keel/requirement-139
 */
export function renderDescription(item: DiscoveryItem, display: DisplayConfig): string {
  const segments: string[] = [];
  for (const displayClass of displayClassOrder) {
    if (!display[displayClass]) {
      continue;
    }
    segments.push(...classSegments(item, displayClass));
  }
  return segments.join(descriptionSeparator);
}

/**
 * Reports whether the item carries any fact the renderer composes from,
 * independent of the display configuration. It separates "the renderer had
 * nothing to say" from "every class is switched off": the first may fall back
 * to the prose channel, the second must not.
 *
 * DHF-REQ: keel/requirement-139
 */
export function hasRenderableFacts(item: DiscoveryItem): boolean {
  return displayClassOrder.some((displayClass) => classSegments(item, displayClass).length > 0);
}

function classSegments(item: DiscoveryItem, displayClass: DisplayClass): string[] {
  switch (displayClass) {
    case 'description':
      return nonEmpty(item.description);
    case 'lastRun':
      return nonEmpty(formatLastRun(item.last_run));
    case 'desiredState':
      return desiredStateSegments(item);
    case 'findings':
      // An error-severity finding is routed to vscode.TestItem.error instead,
      // so the description must not carry it too: a text on both surfaces is
      // one condition reported twice (keel/ac-559).
      // DHF-REQ: keel/requirement-140
      return (item.findings ?? [])
        .filter((finding) => !isErrorSeverity(finding))
        .flatMap((finding) => nonEmpty(formatFinding(finding)));
  }
}

/**
 * Renders the measured duration of an item's newest run. An absent run, or a
 * run whose duration was never measured, renders nothing at all — the separator
 * must never lead on a zero standing in for "not measured". A measured zero is
 * a measurement and does render.
 *
 * The arithmetic works in whole milliseconds rather than in seconds so that
 * this copy and the Go one cannot round a boundary value differently.
 *
 * DHF-REQ: keel/requirement-139
 */
export function formatLastRun(last?: LastRunFacts): string {
  const durationMS = last?.duration_ms;
  if (durationMS === undefined || durationMS < 0) {
    return '';
  }
  if (durationMS > 90_000) {
    const seconds = Math.floor((durationMS + 500) / 1000);
    return `· last ${Math.floor(seconds / 60)}m ${String(seconds % 60).padStart(2, '0')}s`;
  }
  const tenths = Math.floor((durationMS + 50) / 100);
  return `· last ${Math.floor(tenths / 10)}.${tenths % 10}s`;
}

/**
 * Reports whether a finding's typed severity selects the persistent error
 * surface rather than this composed description. It reads the enum member and
 * nothing else — the severity is closed on the wire precisely so that no
 * consumer parses a message to route it (keel/ac-551, keel/ac-559). It mirrors
 * the canonical Go predicate `vscode.IsErrorSeverity`.
 *
 * DHF-REQ: keel/requirement-140
 */
export function isErrorSeverity(finding: Finding): boolean {
  return finding.severity === 'error';
}

/** Renders one typed validation finding. */
export function formatFinding(finding: Finding): string {
  if (!finding.rule && !finding.severity && !finding.message) {
    return '';
  }
  return `${finding.rule} ${finding.severity}: ${finding.message}`;
}

/**
 * Renders the typed desired-state facts of a row.
 *
 * A desired-state GROUP contributes nothing here on purpose. Its exclusivity
 * flag is a bridge input, not a rendered fact: keel/design_decision-5 reserves
 * deciding a rendered state from it to the bridge, which serves the decision as
 * reconcile_results for this extension to replay verbatim. Rendering it as
 * secondary text would put a second, weaker answer to the same question on the
 * screen — and would breach the occurrence budget the config-contract test
 * holds over these sources.
 */
function desiredStateSegments(item: DiscoveryItem): string[] {
  const segments: string[] = [];
  const row = item.desired_state_row;
  if (row) {
    if (row.current) {
      segments.push(`current=${row.current}`);
    }
    if (row.action) {
      segments.push(`action=${row.action}`);
    }
    segments.push(`active=${row.active}`);
  }
  return segments;
}

function nonEmpty(value: string | undefined): string[] {
  return value ? [value] : [];
}
