import * as cp from 'node:child_process';
import * as nodefs from 'node:fs';
import * as fs from 'node:fs/promises';
import * as path from 'node:path';
import { fileURLToPath } from 'node:url';
import * as vscode from 'vscode';
import { adapterConfig, configRelativePath, currentConfigVersion, defaultAdapterConfig, defaultConfigTemplate, discoverTests, planTests, readAdapterConfig, runTests } from './bridgeAdapter';
import { ExternalRunMirror, ExternalRunStateSnapshot, setExternalRunStaleMsForTest } from './externalRunMirror';
import { publishDiscovery, PublishedTree } from './tree';
import { DesiredState, RunEvent, SetupPlan } from './protocol';

let tree: PublishedTree | undefined;
let output: vscode.OutputChannel;
let activeRun = false;
let statusItem: vscode.StatusBarItem;
let activeRunLabel = '';
let testTreeGeneration = 0;
let externalRunMirror: ExternalRunMirror | undefined;

// Re-export for any caller that still imports ExternalRunMirror /
// ExternalRunStateSnapshot from './extension' (the pre-Story-27.24 path).
export { ExternalRunMirror };
export { setExternalRunStaleMsForTest };
export type { ExternalRunStateSnapshot };

// DHF-REQ: keel/requirement-40
export async function activate(context: vscode.ExtensionContext): Promise<void> {
  const workspaceRoot = getWorkspaceRoot();
  if (workspaceRoot) {
    await migrateWorkspaceConfig(workspaceRoot);
  }
  const initialConfig = currentAdapterConfig();
  output = vscode.window.createOutputChannel(initialConfig.outputChannel);
  statusItem = vscode.window.createStatusBarItem(vscode.StatusBarAlignment.Left, 100);
  statusItem.text = `$(testing-run-icon) ${initialConfig.displayName} tests running`;
  statusItem.tooltip = `A ${initialConfig.displayName} test run is active`;
  void openDevWorkspaceWhenLaunchedEmpty();
  const controller = vscode.tests.createTestController('keelTests', initialConfig.displayName);
  context.subscriptions.push(output, controller, statusItem);
  controller.refreshHandler = async () => {
    await refresh(controller);
  };

  const runProfile = controller.createRunProfile('Run', vscode.TestRunProfileKind.Run, (request, token) => {
    void runSelected(controller, request, token);
  });
  context.subscriptions.push(runProfile);
  const coverageProfile = controller.createRunProfile('Coverage', vscode.TestRunProfileKind.Coverage, (request, token) => {
    void runSelected(controller, request, token, true);
  });
  coverageProfile.loadDetailedCoverage = async () => [];
  context.subscriptions.push(coverageProfile);

  context.subscriptions.push(
    vscode.commands.registerCommand('keel.tests.refresh', async () => {
      await refresh(controller);
    }),
    vscode.commands.registerCommand('keel.tests.runSelected', async () => {
      await vscode.commands.executeCommand('testing.runSelected');
    }),
    vscode.commands.registerCommand('keel.tests.clearTestResults', async () => {
      await resetKeelTestResults(controller);
      void vscode.window.showInformationMessage(`Cleared ${currentAdapterConfig().displayName} Test Explorer result state.`);
    }),
    vscode.commands.registerCommand('keel.tests.openArtifact', async (uri: vscode.Uri | string) => {
      const target = typeof uri === 'string' ? vscode.Uri.file(uri) : uri;
      try {
        await vscode.workspace.fs.stat(target);
      } catch {
        void vscode.window.showErrorMessage(`${currentAdapterConfig().displayName} test artifact is no longer available: ${target.fsPath}`);
        return;
      }
      await vscode.window.showTextDocument(target);
    }),
    vscode.commands.registerCommand('keel.tests.clearLocalState', async () => {
      try {
        await runAdapterMaintenance(controller, tree?.capabilities.clear_state_test_ids ?? []);
        void vscode.window.showInformationMessage(`Cleared ${currentAdapterConfig().displayName} local test state.`);
      } catch (error) {
        const message = error instanceof Error ? error.message : String(error);
        output.appendLine(`Clear local state failed: ${message}`);
        void vscode.window.showErrorMessage(message);
      }
    }),
    vscode.commands.registerCommand('keel.tests.initConfig', async () => {
      await initializeConfig();
    })
  );

  const watcher = vscode.workspace.createFileSystemWatcher('**/{*_test.go,*.test.ts,*.test.tsx,*.spec.ts,.vscode/test-bridge.json}');
  const dispatchWatcherEvent = () => scheduleWatcherRefresh(controller);
  context.subscriptions.push(
    watcher,
    watcher.onDidCreate(dispatchWatcherEvent),
    watcher.onDidChange(dispatchWatcherEvent),
    watcher.onDidDelete(dispatchWatcherEvent),
    { dispose: () => clearPendingWatcherRefresh() }
  );
  externalRunMirror = new ExternalRunMirror(controller);
  context.subscriptions.push(externalRunMirror);

  output.appendLine(`[${new Date().toISOString()}] ${initialConfig.displayName} Test Bridge activated.`);
  void refresh(controller);
}

export function deactivate(): void {
  tree = undefined;
  finishActiveRun();
}

export function publishedTestItemIds(): string[] {
  return Array.from(tree?.itemsById.keys() ?? []);
}

export interface PublishedTestItemSnapshot {
  id: string;
  itemId: string;
  label: string;
  sortText?: string;
  description?: string;
  framework?: string;
  runner?: string;
  runnerLabel?: string;
  canonicalId?: string;
  tags: string[];
  uri?: string;
}

export function publishedTestItemsSnapshot(): PublishedTestItemSnapshot[] {
  return Array.from(tree?.itemsById.entries() ?? []).map(([id, item]) => ({
    id,
    itemId: item.id,
    label: item.label,
    sortText: item.sortText,
    description: item.description,
    framework: tree?.discoveryItemsById.get(id)?.framework,
    runner: tree?.discoveryItemsById.get(id)?.runner,
    runnerLabel: tree?.discoveryItemsById.get(id)?.runner_label,
    canonicalId: canonicalIdForItem(id),
    tags: item.tags.map((tag) => tag.id),
    uri: item.uri?.fsPath
  }));
}

export function isRunActive(): boolean {
  return activeRun;
}

export interface ActiveRunStatusSnapshot {
  active: boolean;
  text: string;
  tooltip?: string;
}

export function activeRunStatusSnapshot(): ActiveRunStatusSnapshot {
  return {
    active: activeRun,
    text: statusItem?.text ?? '',
    tooltip: typeof statusItem?.tooltip === 'string' ? statusItem.tooltip : undefined
  };
}

export function beginActiveRun(selected: readonly vscode.TestItem[]): void {
  activeRun = true;
  activeRunLabel = activeRunLabelForSelection(selected);
  statusItem.text = `$(testing-run-icon) ${activeRunLabel}`;
  statusItem.tooltip = `Keel test run active: ${selected.map((item) => item.label).slice(0, 5).join(', ')}${selected.length > 5 ? ', ...' : ''}`;
  statusItem.show();
}

export function finishActiveRun(): void {
  activeRun = false;
  activeRunLabel = '';
  statusItem?.hide();
  flushDeferredWatcherRefresh();
}

// Watcher debounce + run-active gate. Coalesces burst filesystem
// events (e.g. format-on-save across N files) into a single refresh
// at the trailing edge of a quiet window; defers refreshes entirely
// while a test run is active so an in-flight run's TestItem references
// are not invalidated mid-flight by a tree replacement. Closes Story
// 27.24 AC6.
let pendingWatcherRefresh: NodeJS.Timeout | undefined;
let lastWatcherController: vscode.TestController | undefined;
let watcherSawEventsDuringActiveRun = false;
let watcherDebounceMs = 300;

// setWatcherDebounceMs is a test seam. Production never calls this.
export function setWatcherDebounceMs(ms: number): void {
  watcherDebounceMs = ms;
}

export function watcherDebounceMsForTest(): number {
  return watcherDebounceMs;
}

export function isWatcherRefreshPending(): boolean {
  return pendingWatcherRefresh !== undefined;
}

// triggerWatcherEventForTest fires the watcher path as if a save
// arrived. Production code never calls this; the test uses it to
// observe burst-coalescing + run-active gating without standing up a
// real filesystem watcher.
export function triggerWatcherEventForTest(controller: vscode.TestController): void {
  scheduleWatcherRefresh(controller);
}

export function deferredWatcherEventCountForTest(): boolean {
  return watcherSawEventsDuringActiveRun;
}

function scheduleWatcherRefresh(controller: vscode.TestController): void {
  lastWatcherController = controller;
  if (activeRun) {
    watcherSawEventsDuringActiveRun = true;
    return;
  }
  if (pendingWatcherRefresh) {
    clearTimeout(pendingWatcherRefresh);
  }
  pendingWatcherRefresh = setTimeout(() => {
    pendingWatcherRefresh = undefined;
    void refresh(controller);
  }, watcherDebounceMs);
}

function flushDeferredWatcherRefresh(): void {
  if (!watcherSawEventsDuringActiveRun || !lastWatcherController) {
    return;
  }
  watcherSawEventsDuringActiveRun = false;
  scheduleWatcherRefresh(lastWatcherController);
}

function clearPendingWatcherRefresh(): void {
  if (pendingWatcherRefresh) {
    clearTimeout(pendingWatcherRefresh);
    pendingWatcherRefresh = undefined;
  }
  watcherSawEventsDuringActiveRun = false;
  lastWatcherController = undefined;
}

export interface ChildProcessHandle {
  kill(signal?: NodeJS.Signals | number): boolean;
  pid?: number;
}

// signalProcessGroup is exported for tests. It signals the entire
// process group on POSIX (so vitest workers and playwright browser
// processes — grandchildren of the bridge — receive the signal too).
// On Windows, process-group kill via negative PID is not supported;
// the fallback is child.kill(signal). Closes Story 27.24 AC5.
export function signalProcessGroup(child: ChildProcessHandle, signal: NodeJS.Signals): boolean {
  if (process.platform !== 'win32' && typeof child.pid === 'number' && child.pid > 0) {
    try {
      process.kill(-child.pid, signal);
      return true;
    } catch {
      // Fall through to child.kill if the group has already exited.
    }
  }
  return child.kill(signal);
}

export function cancelActiveRun(run: vscode.TestRun, selected: readonly vscode.TestItem[], child: ChildProcessHandle): void {
  appendRunOutput(run, 'Keel test run cancelled by user.', 'WARN');
  for (const item of selected) {
    run.skipped(item);
  }
  signalProcessGroup(child, 'SIGTERM');
}

export function rejectConcurrentRun(
  controller: vscode.TestController,
  request: vscode.TestRunRequest,
  selected: readonly vscode.TestItem[]
): void {
  const run = controller.createTestRun(request);
  appendRunOutput(run, 'Keel test run already active; ignoring concurrent play request.', 'WARN');
  for (const item of selected) {
    run.skipped(item);
  }
  run.end();
}

async function refresh(controller: vscode.TestController): Promise<void> {
  const workspaceRoot = getWorkspaceRoot();
  output.appendLine(`[${new Date().toISOString()}] refresh requested`);
  controller.invalidateTestResults();
  if (!workspaceRoot) {
    testTreeGeneration++;
    output.appendLine('No workspace root is open; clearing Keel test tree.');
    controller.items.replace([]);
    return;
  }
  try {
    const generation = ++testTreeGeneration;
    const discovery = await discoverTests(workspaceRoot);
    if (generation !== testTreeGeneration) {
      output.appendLine(`Ignored stale discovery generation ${generation}.`);
      return;
    }
    tree = publishDiscovery(controller, workspaceRoot, discovery, generation);
    output.appendLine(`Published ${discovery.items.length} Keel test items from ${workspaceRoot}.`);
    void externalRunMirror?.syncWorkspace();
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error);
    output.appendLine(`Discovery failed: ${message}`);
    void vscode.window.showErrorMessage(`Keel test discovery failed: ${message}. Run just build-dev if bin/keel-dev is missing.`);
  }
}

async function resetKeelTestResults(controller: vscode.TestController): Promise<void> {
  await vscode.commands.executeCommand('testing.clearTestResults');
  controller.invalidateTestResults();
  await refresh(controller);
}

async function runAdapterMaintenance(controller: vscode.TestController, ids: readonly string[]): Promise<void> {
  const workspaceRoot = getWorkspaceRoot();
  const adapter = currentAdapterConfig();
  if (!workspaceRoot) {
    throw new Error(`No ${adapter.displayName} workspace is open.`);
  }
  if (ids.length === 0) {
    await refresh(controller);
  }
  const refreshedIDs = ids.length > 0 ? ids : tree?.capabilities.clear_state_test_ids ?? [];
  if (refreshedIDs.length === 0) {
    throw new Error(`${adapter.displayName} adapter does not advertise a clear-state maintenance item.`);
  }
  output.appendLine(`[${new Date().toISOString()}] running maintenance ${refreshedIDs.join(', ')}`);
  const child = runTests(workspaceRoot, [...refreshedIDs]);
  await new Promise<void>((resolve, reject) => {
    child.stdout.on('data', (chunk: Buffer) => output.append(chunk.toString('utf8')));
    child.stderr.on('data', (chunk: Buffer) => output.append(chunk.toString('utf8')));
    child.on('error', reject);
    child.on('close', (code) => {
      if (code && code !== 0) {
        reject(new Error(`${adapter.displayName} maintenance failed with exit code ${code}`));
        return;
      }
      resolve();
    });
  });
  await refresh(controller);
}

async function runSelected(
  controller: vscode.TestController,
  request: vscode.TestRunRequest,
  token: vscode.CancellationToken,
  coverage = false
): Promise<void> {
  const selected = request.include ?? Array.from(tree?.itemsById.values() ?? []);
  if (activeRun) {
    rejectConcurrentRun(controller, request, selected);
    return;
  }
  const run = controller.createTestRun(request);
  const workspaceRoot = getWorkspaceRoot();
  if (!workspaceRoot) {
    appendRunOutput(run, 'No Keel workspace is open.', 'WARN');
    run.end();
    return;
  }
  beginActiveRun(selected);
  let finished = false;
  const finishRun = () => {
    if (finished) {
      return;
    }
    finished = true;
    run.end();
    finishActiveRun();
  };

  const selectedProtocolIds = selected.map(protocolIDForTestItem);
  try {
    const plan = await planTests(workspaceRoot, selectedProtocolIds);
    appendSetupPlan(run, plan);
  } catch (error) {
    appendRunOutput(run, `Failed to plan ${currentAdapterConfig().displayName} test run: ${error instanceof Error ? error.message : String(error)}`, 'ERROR');
    finishRun();
    return;
  }

  let child: ReturnType<typeof runTests>;
  try {
    child = runTests(workspaceRoot, selectedProtocolIds);
  } catch (error) {
    appendRunOutput(run, `Failed to start Keel test run: ${error instanceof Error ? error.message : String(error)}`, 'ERROR');
    finishRun();
    return;
  }
  let forceKill: NodeJS.Timeout | undefined;
  let resetResultsAfterRun = false;
  const selectedItemIds = new Set(selected.map((item) => item.id));
  const resultItemIds = new Set<string>();
  const applyOptions = { coverage, workspaceRoot, modulePath: tree?.modulePath };
  const cancellation = token.onCancellationRequested(() => {
    cancelActiveRun(run, selected, child);
    forceKill = setTimeout(() => signalProcessGroup(child, 'SIGKILL'), 2000);
  });

  let stdout = '';
  child.stdout.on('data', (chunk: Buffer) => {
    stdout += chunk.toString('utf8');
    const lines = stdout.split(/\r?\n/);
    stdout = lines.pop() ?? '';
    for (const line of lines) {
      if (line.trim()) {
        const applied = applyRunEvent(run, line, selectedItemIds, resultItemIds, applyOptions);
        resetResultsAfterRun = applied.resetResults || resetResultsAfterRun;
        if (applied.finished) {
          finishRun();
        }
      }
    }
  });
  child.stderr.on('data', (chunk: Buffer) => {
    appendRunOutput(run, chunk.toString('utf8'), 'WARN');
  });
  await new Promise<void>((resolve) => {
    child.on('error', (error) => {
      appendRunOutput(run, `Keel test process error: ${error.message}`, 'ERROR');
      cancellation.dispose();
      if (forceKill) {
        clearTimeout(forceKill);
      }
      finishRun();
      resolve();
    });
    child.on('close', () => {
      if (stdout.trim()) {
        const applied = applyRunEvent(run, stdout, selectedItemIds, resultItemIds, applyOptions);
        resetResultsAfterRun = applied.resetResults || resetResultsAfterRun;
        if (applied.finished) {
          finishRun();
        }
      }
      cancellation.dispose();
      if (forceKill) {
        clearTimeout(forceKill);
      }
      finishRun();
      if (resetResultsAfterRun) {
        void resetKeelTestResults(controller);
      }
      resolve();
    });
  });
}

export function setupPlanOutputLines(plan: SetupPlan): string[] {
  const lines: string[] = [];
  if (!plan.desired_state?.length) {
    return lines;
  }
  if (plan.devtool) {
    lines.push('======');
    lines.push(`${plan.devtool.name} ${plan.devtool.version} (${plan.devtool.commit}, built ${plan.devtool.built_at})`);
    lines.push('======');
    lines.push('desired state (computed by keel-dev):');
  } else {
    lines.push('desired state:');
  }
  for (const state of plan.desired_state) {
    lines.push(`- ${formatDesiredState(state)}`);
  }
  return lines;
}

function appendSetupPlan(run: vscode.TestRun, plan: SetupPlan): void {
  for (const line of setupPlanOutputLines(plan)) {
    appendRunOutput(run, line);
  }
}

type RunOutputLevel = 'INFO' | 'WARN' | 'ERROR' | 'DEBUG';

export function timestampedRunOutputLines(text: string, now = new Date(), defaultLevel: RunOutputLevel = 'INFO'): string[] {
  const timestamp = formatRunOutputTimestamp(now);
  return text
    .replace(/\r\n/g, '\n')
    .replace(/\r/g, '\n')
    .split('\n')
    .filter((line) => line.trim().length > 0)
    .map((line) => {
      const normalized = normalizeRunOutputLine(line);
      return `${timestamp} ${hasLogLevel(normalized) ? normalized : `${defaultLevel} ${normalized}`}`;
    });
}

export function appendRunOutput(run: vscode.TestRun, text: string, defaultLevel: RunOutputLevel = 'INFO'): void {
  for (const line of timestampedRunOutputLines(text, new Date(), defaultLevel)) {
    run.appendOutput(`${line}\r\n`);
  }
}

function formatRunOutputTimestamp(now: Date): string {
  const pad = (value: number) => String(value).padStart(2, '0');
  return `${now.getFullYear()}-${pad(now.getMonth() + 1)}-${pad(now.getDate())} ${pad(now.getHours())}:${pad(now.getMinutes())}:${pad(now.getSeconds())}`;
}

function normalizeRunOutputLine(line: string): string {
  let current = line.trimEnd();
  for (;;) {
    const match = /^(\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2})\s+(\S+)\s+(.*)$/.exec(current);
    if (!match) {
      return current;
    }
    const kind = match[2];
    const rest = match[3];
    if (kind === 'stdout' || kind === 'stderr') {
      current = rest;
      continue;
    }
    const level = normalizeLogLevel(kind);
    if (level) {
      return `${level} ${rest}`;
    }
    return current;
  }
}

function hasLogLevel(line: string): boolean {
  return /^(INFO|WARN|ERROR|DEBUG)\s+/.test(line);
}

function normalizeLogLevel(kind: string): RunOutputLevel | undefined {
  switch (kind) {
    case 'INFO':
    case 'WARN':
    case 'ERROR':
    case 'DEBUG':
      return kind;
    case 'info':
    case 'ok':
      return 'INFO';
    case 'warn':
    case 'hint':
      return 'WARN';
    case 'error':
      return 'ERROR';
    default:
      return undefined;
  }
}

function formatDesiredState(state: DesiredState): string {
  const ownership = state.owned ? 'owned' : 'shared';
  const reuse = state.reusable ? 'reusable' : 'not reusable';
  const detail = state.detail ? ` (${state.detail})` : '';
  return `${state.resource} ${state.status}: ${state.current} -> ${state.desired}; action=${state.action}; ${ownership}, ${reuse}; ${state.message}${detail}`;
}

function protocolIDForTestItem(item: vscode.TestItem): string {
  return tree?.protocolIdByItemId.get(item.id) ?? item.id;
}

function activeRunLabelForSelection(selected: readonly vscode.TestItem[]): string {
  if (selected.length === 0) {
    return `${currentAdapterConfig().displayName} tests running`;
  }
  if (selected.length === 1) {
    return `${currentAdapterConfig().displayName} running: ${selected[0].label}`;
  }
  return `${currentAdapterConfig().displayName} running ${selected.length} tests`;
}

export function currentAdapterConfig(): ReturnType<typeof defaultAdapterConfig> {
  const workspaceRoot = getWorkspaceRoot();
  if (!workspaceRoot) {
    return defaultAdapterConfig('');
  }
  try {
    return adapterConfig(workspaceRoot);
  } catch {
    return defaultAdapterConfig(workspaceRoot);
  }
}

// DHF-REQ: keel/requirement-40
async function migrateWorkspaceConfig(workspaceRoot: string): Promise<void> {
  let cfg: ReturnType<typeof readAdapterConfig>;
  try {
    cfg = readAdapterConfig(workspaceRoot);
  } catch {
    return;
  }
  if (cfg.version >= currentConfigVersion) {
    return;
  }
  await new Promise<void>((resolve, reject) => {
    const command = path.isAbsolute(cfg.command) ? cfg.command : path.join(workspaceRoot, cfg.command);
    const child = cp.execFile(command, ['vscode', 'config', 'upgrade'], { cwd: workspaceRoot, env: cfg.env ? { ...process.env, ...cfg.env } : process.env }, (error) => {
      if (error) {
        reject(error);
        return;
      }
      resolve();
    });
    child.stdout?.on('data', (chunk: Buffer) => output?.append(chunk.toString('utf8')));
    child.stderr?.on('data', (chunk: Buffer) => output?.append(chunk.toString('utf8')));
  });
  void vscode.window.showInformationMessage(`${cfg.displayName} Test Bridge config upgraded; review the workspace git diff.`);
}

// DHF-REQ: keel/requirement-40
async function initializeConfig(): Promise<void> {
  const workspaceRoot = getWorkspaceRoot();
  if (!workspaceRoot) {
    void vscode.window.showErrorMessage('No workspace is open.');
    return;
  }
  const target = path.join(workspaceRoot, configRelativePath);
  await fs.mkdir(path.dirname(target), { recursive: true });
  await fs.writeFile(target, defaultConfigTemplate(), 'utf8');
  void vscode.window.showInformationMessage('Keel Test Bridge config initialized.');
}

export function getWorkspaceRoot(): string | undefined {
  if (process.env.KEEL_VSCODE_BRIDGE_DEV_WORKSPACE) {
    return process.env.KEEL_VSCODE_BRIDGE_DEV_WORKSPACE;
  }
  return vscode.workspace.workspaceFolders?.[0]?.uri.fsPath;
}

export function externalRunSnapshots(): ExternalRunStateSnapshot[] {
  return Array.from(externalRunMirror?.snapshots() ?? []);
}

// extensionOutput + currentTree are exported for externalRunMirror.ts
// (and only that consumer). Kept here because the OutputChannel and
// the published-tree state are owned by activate() in this file.
export function extensionOutput(): vscode.OutputChannel {
  return output;
}

export function currentTree(): PublishedTree | undefined {
  return tree;
}

export function setCurrentTreeForTest(next: PublishedTree | undefined): void {
  tree = next;
}

async function openDevWorkspaceWhenLaunchedEmpty(): Promise<void> {
  if (vscode.workspace.workspaceFolders?.length) {
    return;
  }
  const devWorkspace = process.env.KEEL_VSCODE_BRIDGE_DEV_WORKSPACE;
  if (!devWorkspace) {
    return;
  }
  output.appendLine(`Opening debug workspace ${devWorkspace}.`);
  await vscode.commands.executeCommand('vscode.openFolder', vscode.Uri.file(devWorkspace), false);
}

interface AppliedRunEvent {
  resetResults: boolean;
  finished: boolean;
}

export interface ApplyRunEventOptions {
  coverage?: boolean;
  workspaceRoot?: string;
  modulePath?: string;
}

export function applyRunEvent(
  run: vscode.TestRun,
  line: string,
  selectedItemIds: ReadonlySet<string>,
  resultItemIds: Set<string>,
  options: ApplyRunEventOptions = {}
): AppliedRunEvent {
  let event: RunEvent;
  try {
    event = JSON.parse(line) as RunEvent;
  } catch {
    appendRunOutput(run, line);
    return { resetResults: false, finished: false };
  }
  const items = event.test_id ? testItemsForRunEvent(event.test_id) : [];
  switch (event.event) {
    case 'run_started':
      appendRunOutput(run, event.message ?? 'Keel test run started');
      break;
    case 'test_started':
      for (const item of resultItemsForRunEvent(items, event.test_id)) {
        if (shouldApplyResultToItem(item, selectedItemIds, resultItemIds, event.test_id)) {
          run.started(item);
        }
      }
      appendRunOutput(run, `started ${event.test_id ?? 'unknown'}`);
      break;
    case 'passed':
      for (const item of resultItemsForRunEvent(items, event.test_id)) {
        if (shouldApplyResultToItem(item, selectedItemIds, resultItemIds, event.test_id)) {
          run.passed(item, event.duration_ms);
          resultItemIds.add(item.id);
        }
      }
      for (const item of skippedSiblingItemsForRunEvent(items, event.test_id, selectedItemIds, resultItemIds)) {
        run.skipped(item);
        resultItemIds.add(item.id);
      }
      for (const item of neutralAncestorItemsForRunEvent(items, event.test_id, selectedItemIds)) {
        run.skipped(item);
      }
      appendRunOutput(run, `passed ${event.test_id ?? 'unknown'}${event.duration_ms !== undefined ? ` (${event.duration_ms} ms)` : ''}`);
      return { resetResults: shouldInvalidateResultsForEvent(event), finished: false };
    case 'failed':
      for (const item of resultItemsForRunEvent(items, event.test_id)) {
        if (shouldApplyResultToItem(item, selectedItemIds, resultItemIds, event.test_id)) {
          run.failed(item, testMessageFromEvent(event, 'Keel test failed'), event.duration_ms);
          resultItemIds.add(item.id);
        }
      }
      appendRunOutput(run, `failed ${event.test_id ?? 'unknown'}: ${event.message ?? 'Keel test failed'}`, 'ERROR');
      break;
    case 'errored':
      for (const item of resultItemsForRunEvent(items, event.test_id)) {
        if (shouldApplyResultToItem(item, selectedItemIds, resultItemIds, event.test_id)) {
          run.errored(item, testMessageFromEvent(event, 'Keel test errored'), event.duration_ms);
          resultItemIds.add(item.id);
        }
      }
      appendRunOutput(run, `errored ${event.test_id ?? 'unknown'}: ${event.message ?? 'Keel test errored'}`, 'ERROR');
      break;
    case 'cancelled':
      for (const item of resultItemsForRunEvent(items, event.test_id)) {
        if (shouldApplyResultToItem(item, selectedItemIds, resultItemIds, event.test_id)) {
          run.skipped(item);
          resultItemIds.add(item.id);
        }
      }
      appendRunOutput(run, `cancelled ${event.test_id ?? 'unknown'}: ${event.message ?? 'Keel test cancelled'}`, 'WARN');
      break;
    case 'skipped':
      for (const item of resultItemsForRunEvent(items, event.test_id)) {
        if (shouldApplyResultToItem(item, selectedItemIds, resultItemIds, event.test_id)) {
          run.skipped(item);
          resultItemIds.add(item.id);
        }
      }
      if (event.message) {
        appendRunOutput(run, event.message);
      }
      break;
    case 'output':
      if (event.message) {
        appendRunOutput(run, event.message);
      }
      break;
    case 'artifact':
      appendArtifact(run, event, options);
      break;
    case 'run_finished':
      appendRunOutput(run, `finished${event.exit_code !== undefined ? ` exit_code=${event.exit_code}` : ''}`);
      return { resetResults: false, finished: true };
    default:
      break;
  }
  return { resetResults: false, finished: false };
}

export interface RunEventApplicationSnapshot {
  resultIds: string[];
  skippedSiblingIds: string[];
  neutralAncestorIds: string[];
}

export function runEventApplicationSnapshot(
  testId: string,
  selectedItemIds: ReadonlySet<string>,
  resultItemIds: ReadonlySet<string>
): RunEventApplicationSnapshot {
  const items = testItemsForRunEvent(testId);
  return {
    resultIds: resultItemsForRunEvent(items, testId).map(protocolIDForTestItem),
    skippedSiblingIds: skippedSiblingItemsForRunEvent(items, testId, selectedItemIds, resultItemIds).map(protocolIDForTestItem),
    neutralAncestorIds: neutralAncestorItemsForRunEvent(items, testId, selectedItemIds).map(protocolIDForTestItem)
  };
}

export function shouldApplyResultToItem(
  item: vscode.TestItem,
  selectedItemIds: ReadonlySet<string>,
  resultItemIds: ReadonlySet<string>,
  explicitResultId?: string
): boolean {
  if (explicitResultId && resultExplicitlyTargetsItem(explicitResultId, item)) {
    return true;
  }
  if (selectedItemIds.has(item.id) || item.children.size === 0) {
    return true;
  }
  const leafIds = leafDescendantIds(item);
  return leafIds.length > 0 && leafIds.every((id) => resultItemIds.has(id));
}

function resultExplicitlyTargetsItem(protocolId: string, item: vscode.TestItem): boolean {
  const itemProtocolId = protocolIDForTestItem(item);
  return itemProtocolId === protocolId || canonicalIdForItem(itemProtocolId) === protocolId;
}

export function resultItemsForRunEvent(items: readonly vscode.TestItem[], explicitResultId?: string): vscode.TestItem[] {
  const result = new Map<string, vscode.TestItem>();
  for (const item of items) {
    result.set(item.id, item);
  }
  return Array.from(result.values());
}

function skippedSiblingItemsForRunEvent(
  items: readonly vscode.TestItem[],
  explicitResultId: string | undefined,
  selectedItemIds: ReadonlySet<string>,
  resultItemIds: ReadonlySet<string>
): vscode.TestItem[] {
  if (!explicitResultId) {
    return [];
  }
  const skipped = new Map<string, vscode.TestItem>();
  for (const item of items) {
    if (item.children.size > 0 || !resultExplicitlyTargetsItem(explicitResultId, item)) {
      continue;
    }
    const parent = tree?.parentByItemId.get(item.id);
    if (!parent || selectedItemIds.has(parent.id)) {
      continue;
    }
    parent.children.forEach((sibling) => {
      if (sibling.id !== item.id && sibling.children.size === 0 && !selectedItemIds.has(sibling.id) && !resultItemIds.has(sibling.id)) {
        skipped.set(sibling.id, sibling);
      }
    });
  }
  return Array.from(skipped.values());
}

function neutralAncestorItemsForRunEvent(
  items: readonly vscode.TestItem[],
  explicitResultId: string | undefined,
  selectedItemIds: ReadonlySet<string>
): vscode.TestItem[] {
  if (!explicitResultId) {
    return [];
  }
  const neutral = new Map<string, vscode.TestItem>();
  for (const item of items) {
    if (item.children.size > 0 || !resultExplicitlyTargetsItem(explicitResultId, item)) {
      continue;
    }
    let parent = tree?.parentByItemId.get(item.id);
    while (parent) {
      if (selectedItemIds.has(parent.id) || resultExplicitlyTargetsItem(explicitResultId, parent)) {
        break;
      }
      neutral.set(parent.id, parent);
      parent = tree?.parentByItemId.get(parent.id);
    }
  }
  return Array.from(neutral.values());
}

function descendantsOf(item: vscode.TestItem): vscode.TestItem[] {
  const descendants: vscode.TestItem[] = [];
  item.children.forEach((child) => {
    descendants.push(child);
    descendants.push(...descendantsOf(child));
  });
  return descendants;
}

function leafDescendantIds(item: vscode.TestItem): string[] {
  const leaves: string[] = [];
  const visit = (candidate: vscode.TestItem): void => {
    if (candidate.children.size === 0) {
      leaves.push(candidate.id);
      return;
    }
    candidate.children.forEach((child) => visit(child));
  };
  item.children.forEach((child) => visit(child));
  return leaves;
}

export function shouldInvalidateResultsForEvent(event: RunEvent): boolean {
  if (event.event !== 'passed' || !event.test_id) {
    return false;
  }
  const clearIDs = tree?.capabilities.clear_results_test_ids ?? [];
  return clearIDs.includes(event.test_id);
}

export function testItemsForRunEvent(id: string): vscode.TestItem[] {
  const canonical = tree?.itemsById.get(id);
  const aliases = tree?.aliasesByCanonicalId.get(id) ?? [];
  return canonical ? [canonical, ...aliases] : aliases;
}

function canonicalIdForItem(id: string): string | undefined {
  return tree?.canonicalIdByAliasId.get(id);
}

export function testMessageFromEvent(event: RunEvent, fallback: string): vscode.TestMessage {
  const message = new vscode.TestMessage(event.message ?? fallback);
  if (event.location) {
    message.location = new vscode.Location(
      vscode.Uri.file(event.location.uri),
      new vscode.Position(event.location.line, event.location.column)
    );
  }
  return message;
}

function appendArtifact(run: vscode.TestRun, event: RunEvent, options: ApplyRunEventOptions): void {
  if (!event.artifact) {
    if (event.message) {
      appendRunOutput(run, event.message);
    }
    return;
  }
  if (event.artifact.kind === 'coverage') {
    appendCoverageArtifact(run, event, options);
    return;
  }
  appendRunOutput(run, artifactOutputLine(event));
}

// DHF-REQ: keel/requirement-39
function appendCoverageArtifact(run: vscode.TestRun, event: RunEvent, options: ApplyRunEventOptions): void {
  if (!options.coverage) {
    appendRunOutput(run, `coverage artifact ignored outside Coverage profile: ${event.artifact?.uri ?? 'unknown'}`);
    return;
  }
  if (!event.artifact || !options.workspaceRoot || !options.modulePath) {
    appendRunOutput(run, 'coverage artifact cannot be applied because discovery did not provide module_path', 'ERROR');
    return;
  }
  let profilePath: string;
  try {
    profilePath = fileURLToPath(event.artifact.uri);
  } catch {
    appendRunOutput(run, `coverage artifact URI is not a file URI: ${event.artifact.uri}`, 'ERROR');
    return;
  }
  if (!nodefs.existsSync(profilePath)) {
    appendRunOutput(run, `coverage artifact is no longer available: ${profilePath}`, 'ERROR');
    return;
  }
  const profile = nodefs.readFileSync(profilePath, 'utf8');
  for (const fileCoverage of parseGoCoverageProfile(profile, options.workspaceRoot, options.modulePath)) {
    run.addCoverage(fileCoverage);
  }
  appendRunOutput(run, artifactOutputLine(event));
}

export function artifactOutputLine(event: RunEvent): string {
  if (!event.artifact) {
    return event.message ? `${event.message}\r\n` : '';
  }
  return `artifact ${event.test_id ?? 'run'}: ${event.artifact.kind} ${event.artifact.name} ${event.artifact.uri} (${artifactCommandUri(event.artifact.uri)})\r\n`;
}

export function artifactCommandUri(uri: string): string {
  return `command:keel.tests.openArtifact?${encodeURIComponent(JSON.stringify([uri]))}`;
}

// DHF-REQ: keel/requirement-39
export function parseGoCoverageProfile(profile: string, workspaceRoot: string, modulePath: string): vscode.FileCoverage[] {
  const byFile = new Map<string, { covered: number; total: number }>();
  const prefix = `${modulePath}/`;
  for (const line of profile.split(/\r?\n/)) {
    const trimmed = line.trim();
    if (!trimmed || trimmed.startsWith('mode:')) {
      continue;
    }
    const match = /^([^:]+):\d+\.\d+,\d+\.\d+\s+(\d+)\s+(\d+)$/.exec(trimmed);
    if (!match || !match[1].startsWith(prefix)) {
      continue;
    }
    const relative = match[1].slice(prefix.length);
    const statements = Number.parseInt(match[2], 10);
    const count = Number.parseInt(match[3], 10);
    const current = byFile.get(relative) ?? { covered: 0, total: 0 };
    current.total += statements;
    if (count > 0) {
      current.covered += statements;
    }
    byFile.set(relative, current);
  }
  return Array.from(byFile.entries())
    .sort(([left], [right]) => left.localeCompare(right))
    .map(([relative, counts]) => new vscode.FileCoverage(
      vscode.Uri.file(path.join(workspaceRoot, relative)),
      new vscode.TestCoverageCount(counts.covered, counts.total)
    ));
}

export function coverageFileSnapshotsForTest(coverages: readonly vscode.FileCoverage[]): Array<{ uri: string; covered: number; total: number }> {
  return coverages.map((coverage) => ({
    uri: coverage.uri.fsPath,
    covered: coverage.statementCoverage.covered,
    total: coverage.statementCoverage.total
  }));
}
