import * as cp from 'node:child_process';
import * as fs from 'node:fs';
import * as path from 'node:path';
import { promisify } from 'node:util';
import { DiscoveryDocument, SetupPlan } from './protocol';

const execFile = promisify(cp.execFile);
export const currentConfigVersion = 3;
export const configRelativePath = path.join('.vscode', 'test-bridge.json');

export interface BridgeAdapterConfig {
  version: number;
  command: string;
  args: string[];
  displayName: string;
  outputChannel: string;
  env?: Record<string, string>;
}

export interface DemoBlockStatus {
  blocked_lane?: string;
  source: string;
  path: string;
}

export function adapterConfig(workspaceRoot: string): BridgeAdapterConfig {
  const config = readAdapterConfig(workspaceRoot);
  return {
    version: config.version,
    command: resolveAdapterCommand(workspaceRoot, config.command),
    args: config.args,
    displayName: config.displayName,
    outputChannel: `${config.displayName} Test Bridge`,
    env: config.env
  };
}

export function defaultAdapterConfig(workspaceRoot: string): BridgeAdapterConfig {
  return {
    version: currentConfigVersion,
    command: defaultAdapterCommand(workspaceRoot),
    args: [],
    displayName: 'Keel',
    outputChannel: 'Keel Test Bridge'
  };
}

// DHF-REQ: keel/requirement-59
export function defaultConfigTemplate(): string {
  return `${JSON.stringify(defaultAdapterConfig(''), ['version', 'command', 'args', 'displayName', 'env'], 2)}\n`;
}

// DHF-REQ: keel/requirement-40
export function readAdapterConfig(workspaceRoot: string): BridgeAdapterConfig {
  const raw = fs.readFileSync(path.join(workspaceRoot, configRelativePath), 'utf8');
  const parsed = JSON.parse(raw) as Partial<BridgeAdapterConfig>;
  if (typeof parsed.version !== 'number') {
    throw new Error('test bridge config is missing numeric version');
  }
  if (typeof parsed.command !== 'string' || parsed.command.length === 0) {
    throw new Error('test bridge config is missing command');
  }
  if (!Array.isArray(parsed.args) || !parsed.args.every((arg) => typeof arg === 'string')) {
    throw new Error('test bridge config args must be strings');
  }
  if (parsed.version >= currentConfigVersion && hasProtocolTokens(parsed.args)) {
    throw new Error('test bridge config v3 args must be launcher-only');
  }
  if (typeof parsed.displayName !== 'string' || parsed.displayName.length === 0) {
    throw new Error('test bridge config is missing displayName');
  }
  return {
    version: parsed.version,
    command: parsed.command,
    args: parsed.args,
    displayName: parsed.displayName,
    outputChannel: `${parsed.displayName} Test Bridge`,
    env: parsed.env
  };
}

export async function discoverTests(workspaceRoot: string): Promise<DiscoveryDocument> {
  const adapter = adapterConfig(workspaceRoot);
  const { stdout } = await execFile(adapter.command, canonicalTestsArgs(adapter, 'discover', ['--format', 'json']), {
    cwd: workspaceRoot,
    env: adapterEnv(adapter),
    maxBuffer: 16 * 1024 * 1024
  });
  const parsed = JSON.parse(stdout) as DiscoveryDocument;
  if (parsed.version !== 1 || !Array.isArray(parsed.items)) {
    throw new Error(`${adapter.displayName} adapter returned an unsupported VS Code discovery document`);
  }
  return parsed;
}

export async function planTests(workspaceRoot: string, ids: string[]): Promise<SetupPlan> {
  const adapter = adapterConfig(workspaceRoot);
  const args = canonicalTestsArgs(adapter, 'desired-state', ['--format', 'json']);
  for (const id of ids) {
    args.push('--id', id);
  }
  const { stdout } = await execFile(adapter.command, args, {
    cwd: workspaceRoot,
    env: adapterEnv(adapter),
    maxBuffer: 16 * 1024 * 1024
  });
  const parsed = JSON.parse(stdout) as SetupPlan;
  if (parsed.version !== 1 || !Array.isArray(parsed.items)) {
    throw new Error(`${adapter.displayName} adapter returned an unsupported VS Code setup plan`);
  }
  return parsed;
}

// DHF-REQ: keel/requirement-42
export function runTests(workspaceRoot: string, ids: string[]): cp.ChildProcessWithoutNullStreams {
  const adapter = adapterConfig(workspaceRoot);
  const args = canonicalTestsArgs(adapter, 'run');
  for (const id of ids) {
    args.push('--id', id);
  }
  // `detached: true` opens a new process group on POSIX so when the
  // adapter shells out to pnpm → vitest workers → playwright browsers,
  // a single process.kill(-child.pid, …) reaches the whole tree. Without
  // this, the cancel path SIGTERMs the immediate child only, leaving
  // grandchildren holding ports and CPU. Cancellation in cancelActiveRun
  // signals -child.pid; the negative-PID form is invalid on Windows so
  // the fallback path still uses child.kill(signal) for non-POSIX. See
  // Story 27.24 AC5.
  return cp.spawn(adapter.command, args, {
    cwd: workspaceRoot,
    env: adapterEnv(adapter),
    stdio: ['pipe', 'pipe', 'pipe'],
    detached: process.platform !== 'win32'
  });
}

// DHF-REQ: keel/requirement-41
export async function readDemoBlockStatus(workspaceRoot: string): Promise<DemoBlockStatus> {
  const adapter = adapterConfig(workspaceRoot);
  const { stdout } = await execFile(adapter.command, adapterDemoArgs(adapter, 'status'), {
    cwd: workspaceRoot,
    env: adapterEnv(adapter),
    maxBuffer: 1024 * 1024
  });
  return JSON.parse(stdout) as DemoBlockStatus;
}

// DHF-REQ: keel/requirement-41
export async function setDemoBlock(workspaceRoot: string, laneID: string | undefined): Promise<void> {
  const adapter = adapterConfig(workspaceRoot);
  const args = laneID ? adapterDemoArgs(adapter, 'block', laneID) : adapterDemoArgs(adapter, 'unblock');
  await execFile(adapter.command, args, {
    cwd: workspaceRoot,
    env: adapterEnv(adapter),
    maxBuffer: 1024 * 1024
  });
}

// DHF-REQ: keel/requirement-59
export async function upgradeConfig(workspaceRoot: string): Promise<{ stdout: string; stderr: string }> {
  const raw = readAdapterConfig(workspaceRoot);
  const adapter: BridgeAdapterConfig = {
    ...raw,
    command: resolveAdapterCommand(workspaceRoot, raw.command),
    args: launcherArgsForConfigUpgrade(raw)
  };
  const { stdout, stderr } = await execFile(adapter.command, [...adapter.args, 'test-bridge', 'config', 'upgrade'], {
    cwd: workspaceRoot,
    env: adapterEnv(adapter),
    maxBuffer: 1024 * 1024
  });
  return { stdout, stderr };
}

function defaultAdapterCommand(workspaceRoot: string): string {
  return workspaceRoot ? path.join(workspaceRoot, 'bin', process.platform === 'win32' ? 'keel-dev.exe' : 'keel-dev') : 'bin/keel-dev';
}

function resolveAdapterCommand(workspaceRoot: string, command: string): string {
  if (path.isAbsolute(command)) {
    return command;
  }
  return path.join(workspaceRoot, command);
}

function adapterEnv(adapter: BridgeAdapterConfig): NodeJS.ProcessEnv {
  return adapter.env ? { ...process.env, ...adapter.env } : process.env;
}

// DHF-REQ: keel/requirement-59
function canonicalTestsArgs(adapter: BridgeAdapterConfig, verb: 'discover' | 'desired-state' | 'run', extra: string[] = []): string[] {
  return [...adapter.args, 'test-bridge', 'tests', verb, ...extra];
}

function adapterDemoArgs(adapter: BridgeAdapterConfig, verb: string, laneID?: string): string[] {
  const args = trimLegacyVSCodeTestsPrefix(adapter.args);
  args.push('vscode', 'demo');
  args.push(verb);
  if (laneID) {
    args.push(laneID);
  }
  return args;
}

function launcherArgsForConfigUpgrade(config: BridgeAdapterConfig): string[] {
  if (config.version < currentConfigVersion) {
    return trimLegacyVSCodeTestsPrefix(config.args);
  }
  return [...config.args];
}

function trimLegacyVSCodeTestsPrefix(args: readonly string[]): string[] {
  const out = [...args];
  if (out.length >= 2 && out[out.length - 2] === 'vscode' && out[out.length - 1] === 'tests') {
    return out.slice(0, -2);
  }
  return out;
}

function hasProtocolTokens(args: readonly string[]): boolean {
  return args.includes('test-bridge') || args.some((arg, index) => arg === 'vscode' && (args[index + 1] === 'tests' || args[index + 1] === 'config'));
}
