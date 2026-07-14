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
}

export interface DiscoveryItem {
  id: string;
  parent_id?: string;
  label: string;
  sort_text?: string;
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
  limitations?: string[];
}

export interface DiscoveryRange {
  start_line: number;
  start_column: number;
  end_line: number;
  end_column: number;
}

export type RunProfileKind = 'run' | 'debug' | 'coverage';

export interface SetupPlan {
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
  event: 'run_started' | 'test_started' | 'output' | 'passed' | 'failed' | 'errored' | 'cancelled' | 'skipped' | 'artifact' | 'run_finished';
  time: string;
  run_id?: string;
  source?: 'vscode' | 'external';
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
