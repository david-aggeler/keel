export interface DiscoveryDocument {
  version: 1;
  workspace: string;
  module_path?: string;
  generated_at: string;
  capabilities?: DiscoveryCapabilities;
  items: DiscoveryItem[];
}

export interface DiscoveryCapabilities {
  clear_results?: boolean;
  refresh_invalidates_results?: boolean;
  neutral_parent_rollups?: boolean;
  clear_results_test_ids?: string[];
  clear_state_test_ids?: string[];
  /**
   * Bridge-computed rendered truth for exclusive desired-state rows: one
   * stamp per row with a run id (active row passed, every other row
   * skipped). The VSIX replays the entries verbatim through one
   * non-persisted TestRun per refresh, overwriting stale results —
   * including results restored from persistence after a window reload.
   * DHF-REQ: keel/requirement-97
   */
  reconcile_results?: ReconcileResult[];
}

export interface ReconcileResult {
  test_id: string;
  state: 'passed' | 'skipped';
  message?: string;
}

export interface DiscoveryItem {
  id: string;
  parent_id?: string;
  label: string;
  kind: 'root' | 'lane' | 'package' | 'file' | 'suite' | 'test' | 'project' | 'group' | 'maintenance';
  framework?: string;
  runner?: string;
  runner_label?: string;
  uri?: string;
  range?: DiscoveryRange;
  runnable: boolean;
  profiles: RunProfileKind[];
  lane_id?: string;
  playwright_project?: string;
  canonical_id?: string;
  required_resources?: string[];
  /**
   * The producer's own prose for this item — one string, never a composed
   * line. Sequencing it against anything else is the consumer's job
   * (keel/requirement-138).
   */
  description?: string;
  /** Typed validation findings raised against this item. */
  findings?: Finding[];
  /**
   * Typed measurement of the newest run attributable to this item alone.
   * Absent — never zeroed — when no run is attributable.
   */
  last_run?: LastRunFacts;
  desired_state_group?: DesiredStateGroupFacts;
  desired_state_row?: DesiredStateRowFacts;
}

/** One typed validation finding raised against a discovery item. */
export interface Finding {
  rule: string;
  severity: FindingSeverity;
  message: string;
}

export type FindingSeverity = 'error' | 'warning';

/** Typed measurement of the newest run attributable to one discovery item. */
export interface LastRunFacts {
  at: string;
  duration_ms?: number;
  exit_code?: number;
}

/** Typed desired-state facts a discovery item carries when it is a group. */
export interface DesiredStateGroupFacts {
  mutually_exclusive: boolean;
}

/** Typed desired-state facts a discovery item carries when it is a row. */
export interface DesiredStateRowFacts {
  current: string;
  action: DesiredStateAction;
  active: boolean;
}

export type DesiredStateAction = 'reuse' | 'manual_setup_required' | 'reconcile' | 'reconcile_during_run';

export interface DiscoveryRange {
  start_line: number;
  start_column: number;
  end_line: number;
  end_column: number;
}

export type RunProfileKind = 'run' | 'debug' | 'coverage';

export interface DesiredStateDocument {
  version: 3;
  devtool?: {
    name: string;
    version: string;
    commit: string;
    built_at: string;
  };
  workspace: string;
  generated_at: string;
  groups: DesiredStateGroup[];
  teardown_policy?: string;
}

export interface DesiredStateGroup {
  label: string;
  order: number;
  mutually_exclusive: boolean;
  rows: DesiredState[];
}

export interface DesiredState {
  /**
   * Canonical devtool-served id that makes this row runnable through the
   * ordinary run interaction. Absent = informational row, never submitted.
   */
  run_id?: string;
  resource: string;
  kind: string;
  desired: string;
  current: string;
  status: string;
  action: string;
  message: string;
  detail?: string;
  reusable: boolean;
  owned: boolean;
  active?: boolean;
}

export interface RunEvent {
  version: 1;
  event: 'run_started' | 'test_started' | 'output' | 'passed' | 'failed' | 'errored' | 'cancelled' | 'skipped' | 'cleared' | 'artifact' | 'run_finished';
  time: string;
  run_id?: string;
  // Two axes on one field, per keel/design_decision-14: 'vscode' and 'external'
  // name which producer normalized the events, 'editor' names the surface that
  // initiated the run. A stream carrying no recognized value is unattributed.
  source?: 'vscode' | 'external' | 'editor';
  workspace?: string;
  live?: boolean;
  requested?: Array<{ id: string; label: string }>;
  test_id?: string;
  message?: string;
  duration_ms?: number;
  location?: {
    uri: string;
    line: number;
    column: number;
  };
  artifact?: {
    name: string;
    uri: string;
    kind: string;
  };
  exit_code?: number;
}
