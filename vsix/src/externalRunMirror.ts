// ExternalRunMirror watches `.devtools/vscode-runs/<run-id>.jsonl`
// files emitted by `bin/keel-dev test run …` invocations that VS Code
// did not start itself, and replays each event into the same
// TestController used for in-Test-Explorer runs. Net effect:
// terminal-initiated lane runs land in the Test Results panel with the
// same result attribution as if the user had clicked Run. See
// docs/devtool/vscode-integration.md § External Run Mirror.
//
// Pulled out of extension.ts per Story 27.24 AC19. The class still
// depends on a handful of extension.ts helpers; those are imported
// directly. The cycle is runtime-safe: every cross-module reference
// is inside a method body, not at module-load time.

import * as fs from 'node:fs/promises';
import * as path from 'node:path';
import * as vscode from 'vscode';
import {
  appendRunOutput,
  applyRunEvent,
  currentTree,
  extensionOutput,
  getWorkspaceRoot,
  invalidateClearedResults,
  resultItemsForRunEvent,
  testItemsForRunEvent
} from './extension';
import { RunEvent } from './protocol';

export interface ExternalRunStateSnapshot {
  runId: string;
  finished: boolean;
  importedCompleted: boolean;
  resultIds: string[];
}

interface ExternalRunStreamState {
  file: string;
  runId: string;
  run: vscode.TestRun;
  selectedItemIds: Set<string>;
  selectedProtocolIds: Set<string>;
  resultItemIds: Set<string>;
  protocolResultIds: Set<string>;
  clearedResultIds: Set<string>;
  lineCount: number;
  finished: boolean;
  importedCompleted: boolean;
  staleTimer?: NodeJS.Timeout;
}

let externalRunStaleMs = 60_000;

// DHF-REQ: keel/requirement-36
export function setExternalRunStaleMsForTest(ms: number): void {
  externalRunStaleMs = ms;
}

export class ExternalRunMirror implements vscode.Disposable {
  private watcher?: vscode.FileSystemWatcher;
  private workspaceRoot?: string;
  private readonly streams = new Map<string, ExternalRunStreamState>();
  private readonly disposables: vscode.Disposable[] = [];

  constructor(private readonly controller: vscode.TestController) {}

  dispose(): void {
    this.watcher?.dispose();
    for (const state of this.streams.values()) {
      if (state.staleTimer) {
        clearTimeout(state.staleTimer);
      }
      state.run.end();
    }
    for (const disposable of this.disposables) {
      disposable.dispose();
    }
    this.streams.clear();
  }

  snapshots(): ExternalRunStateSnapshot[] {
    const tree = currentTree();
    return Array.from(this.streams.values()).map((state) => ({
      runId: state.runId,
      finished: state.finished,
      importedCompleted: state.importedCompleted,
      resultIds: Array.from(new Set([
        ...state.protocolResultIds,
        ...Array.from(state.resultItemIds).map((id) => tree?.protocolIdByItemId.get(id) ?? id)
      ]))
    }));
  }

  async syncWorkspace(): Promise<void> {
    const root = getWorkspaceRoot();
    if (!root) {
      return;
    }
    if (root !== this.workspaceRoot) {
      this.watcher?.dispose();
      this.workspaceRoot = root;
      const pattern = new vscode.RelativePattern(root, '.devtools/vscode-runs/*.jsonl');
      this.watcher = vscode.workspace.createFileSystemWatcher(pattern);
      this.disposables.push(
        this.watcher,
        this.watcher.onDidCreate((uri) => void this.process(uri.fsPath)),
        this.watcher.onDidChange((uri) => void this.process(uri.fsPath)),
        this.watcher.onDidDelete((uri) => this.forget(uri.fsPath))
      );
    }
    await this.importExisting(root);
  }

  private async importExisting(root: string): Promise<void> {
    const dir = path.join(root, '.devtools', 'vscode-runs');
    let names: string[];
    try {
      names = await fs.readdir(dir);
    } catch (error) {
      if ((error as NodeJS.ErrnoException).code !== 'ENOENT') {
        extensionOutput().appendLine(`External run import unavailable: ${error instanceof Error ? error.message : String(error)}`);
      }
      return;
    }
    for (const name of names.filter((candidate) => candidate.endsWith('.jsonl')).sort()) {
      await this.process(path.join(dir, name));
    }
  }

  private forget(file: string): void {
    const state = this.streams.get(file);
    if (!state) {
      return;
    }
    if (!state.finished) {
      if (state.staleTimer) {
        clearTimeout(state.staleTimer);
      }
      state.run.end();
    }
    this.streams.delete(file);
  }

  // DHF-REQ: keel/requirement-88
  private async process(file: string): Promise<void> {
    if (!this.workspaceRoot || !file.startsWith(path.join(this.workspaceRoot, '.devtools', 'vscode-runs') + path.sep)) {
      return;
    }
    let body: string;
    try {
      body = await fs.readFile(file, 'utf8');
    } catch (error) {
      if ((error as NodeJS.ErrnoException).code !== 'ENOENT') {
        extensionOutput().appendLine(`External run stream read failed for ${file}: ${error instanceof Error ? error.message : String(error)}`);
      }
      return;
    }
    const lines = body.split(/\r?\n/).filter((line) => line.trim().length > 0);
    let state = this.streams.get(file);
    if (state && lines.length <= state.lineCount) {
      return;
    }
    if (!state) {
      const first = firstRunEvent(lines);
      if (first?.test_id && testItemsForRunEvent(first.test_id).length === 0) {
        extensionOutput().appendLine(`External run stream ${file} references ${first.test_id} before discovery has published it; deferring import.`);
        return;
      }
      const completed = lines.some((line) => isRunFinishedLine(line));
      state = this.createState(file, lines, completed, first);
      this.streams.set(file, state);
      if (state.importedCompleted) {
        appendRunOutput(state.run, `Importing completed external run ${state.runId}.`);
      } else {
        appendRunOutput(state.run, `Mirroring external run ${state.runId}.`);
      }
    }
    for (const line of lines.slice(state.lineCount)) {
      const resultID = terminalRunEventTestID(line);
      if (resultID) {
        state.protocolResultIds.add(resultID);
      }
      const applied = applyRunEvent(state.run, line, state.selectedItemIds, state.resultItemIds);
      applied.clearedResultIds?.forEach((id) => state.clearedResultIds.add(id));
      if (applied.finished) {
        state.finished = true;
        if (state.staleTimer) {
          clearTimeout(state.staleTimer);
          state.staleTimer = undefined;
        }
        invalidateClearedResults(this.controller, state.clearedResultIds);
        state.run.end();
      }
    }
    state.lineCount = lines.length;
    this.scheduleStaleClose(state);
  }

  private createState(file: string, lines: string[], importedCompleted: boolean, first = firstRunEvent(lines)): ExternalRunStreamState {
    const runId = first?.run_id ?? path.basename(file, '.jsonl');
    const selectedItems = first?.test_id ? resultItemsForRunEvent(testItemsForRunEvent(first.test_id), first.test_id) : [];
    const request = selectedItems.length > 0 ? new vscode.TestRunRequest(selectedItems) : new vscode.TestRunRequest();
    return {
      file,
      runId,
      run: this.controller.createTestRun(request, `External ${runId}`),
      selectedItemIds: new Set(selectedItems.map((item) => item.id)),
      selectedProtocolIds: new Set(first?.test_id ? [first.test_id] : []),
      resultItemIds: new Set<string>(),
      protocolResultIds: new Set<string>(),
      clearedResultIds: new Set<string>(),
      lineCount: 0,
      finished: false,
      importedCompleted
    };
  }

  // DHF-REQ: keel/requirement-36
  private scheduleStaleClose(state: ExternalRunStreamState): void {
    if (state.finished || state.importedCompleted) {
      return;
    }
    if (state.staleTimer) {
      clearTimeout(state.staleTimer);
    }
    const expectedLineCount = state.lineCount;
    state.staleTimer = setTimeout(() => {
      state.staleTimer = undefined;
      if (state.finished || state.lineCount !== expectedLineCount) {
        return;
      }
      this.closeStaleStream(state);
    }, externalRunStaleMs);
  }

  private closeStaleStream(state: ExternalRunStreamState): void {
    if (state.finished) {
      return;
    }
    const time = new Date().toISOString();
    const resultIds = state.protocolResultIds.size > 0 ? Array.from(state.protocolResultIds) : Array.from(state.selectedProtocolIds);
    const targetIds = resultIds.length > 0 ? resultIds : [state.runId];
    for (const testId of targetIds) {
      applyRunEvent(state.run, JSON.stringify({
        version: 1,
        event: 'errored',
        time,
        run_id: state.runId,
        test_id: testId,
        message: `External run stream truncated at line ${state.lineCount}; no terminal event arrived before the ${externalRunStaleMs}ms stale timeout.`
      } satisfies RunEvent), state.selectedItemIds, state.resultItemIds);
      state.protocolResultIds.add(testId);
    }
    applyRunEvent(state.run, JSON.stringify({
      version: 1,
      event: 'run_finished',
      time,
      run_id: state.runId,
      exit_code: 1
    } satisfies RunEvent), state.selectedItemIds, state.resultItemIds);
    state.finished = true;
    state.run.end();
  }
}

function firstRunEvent(lines: string[]): RunEvent | undefined {
  for (const line of lines) {
    try {
      const event = JSON.parse(line) as RunEvent;
      if (event.version === 1 && event.event === 'run_started') {
        return event;
      }
    } catch {
      return undefined;
    }
  }
  return undefined;
}

function isRunFinishedLine(line: string): boolean {
  try {
    return (JSON.parse(line) as RunEvent).event === 'run_finished';
  } catch {
    return false;
  }
}

function terminalRunEventTestID(line: string): string | undefined {
  try {
    const event = JSON.parse(line) as RunEvent;
    if (event.test_id && ['passed', 'failed', 'errored', 'cancelled', 'skipped'].includes(event.event)) {
      return event.test_id;
    }
  } catch {
    return undefined;
  }
  return undefined;
}
