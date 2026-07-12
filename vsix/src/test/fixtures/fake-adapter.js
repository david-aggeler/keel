#!/usr/bin/env node

const args = process.argv.slice(2);
const command = args.slice(0, 4).join(' ');
const now = () => new Date().toISOString();
const fs = require('node:fs');
const path = require('node:path');

function logCall() {
  const dir = path.join(process.cwd(), '.devtools');
  fs.mkdirSync(dir, { recursive: true });
  fs.appendFileSync(path.join(dir, 'fake-adapter-calls.log'), `${args.join(' ')}\n`);
}

logCall();

function blockStatePath() {
  return path.join(process.cwd(), '.devtools', 'vscode-demo-block.json');
}

function readBlockedLane() {
  try {
    return JSON.parse(fs.readFileSync(blockStatePath(), 'utf8')).blocked_lane ?? '';
  } catch {
    return '';
  }
}

if (command === 'vscode tests discover --format') {
  const format = args[4];
  if (format !== 'json') {
    process.stderr.write(`unsupported format ${format}\n`);
    process.exit(2);
  }
  process.stdout.write(JSON.stringify({
    version: 1,
    workspace: process.cwd(),
    generated_at: now(),
    capabilities: {
      clear_results: true,
      refresh_invalidates_results: true,
      neutral_parent_rollups: true,
      clear_results_test_ids: ['keel::maintenance::clear-results'],
      clear_state_test_ids: ['keel::maintenance::clear-state']
    },
    items: [
      { id: 'keel::maintenance', label: 'a. Maintenance', sort_text: 'a', kind: 'group', runnable: false, profiles: [] },
      { id: 'keel::lanes', label: 'b. Lanes', sort_text: 'b', kind: 'group', runnable: false, profiles: [] },
      { id: 'keel::frameworks', label: 'd. Frameworks', sort_text: 'd', kind: 'group', runnable: false, profiles: [] },
      { id: 'keel::agents', parent_id: 'keel::frameworks', label: 'Agents', kind: 'root', framework: 'keel', runner: 'go-test', runner_label: 'Go test', runnable: true, profiles: ['run'] },
      { id: 'keel::file::agents/test_memory.go', parent_id: 'keel::agents', label: 'test_memory.go', kind: 'file', framework: 'keel', runner: 'go-test', runner_label: 'Go test', uri: 'agents/test_memory.go', runnable: true, profiles: ['run'] },
      { id: 'keel::test::agents/test_memory.go::TestRecall', parent_id: 'keel::file::agents/test_memory.go', label: 'TestRecall', kind: 'test', framework: 'keel', runner: 'go-test', runner_label: 'Go test', uri: 'agents/test_memory.go', runnable: true, profiles: ['run'], required_resources: ['go'] },
      { id: 'keel::test::agents/test_memory.go::TestStore', parent_id: 'keel::file::agents/test_memory.go', label: 'TestStore', kind: 'test', framework: 'keel', runner: 'go-test', runner_label: 'Go test', uri: 'agents/test_memory.go', runnable: true, profiles: ['run'], required_resources: ['go'] },
      { id: 'go::root', parent_id: 'keel::frameworks', label: 'd.1 Go', sort_text: 'd.001', kind: 'root', framework: 'go', runner: 'go-test', runner_label: 'Go test', runnable: true, profiles: ['run'], required_resources: ['go-toolchain', 'keel-module-root'] },
      { id: 'go::pkg::log', parent_id: 'go::root', label: 'log', kind: 'package', framework: 'go', runner: 'go-test', runner_label: 'Go test', runnable: true, profiles: ['run'], required_resources: ['go-toolchain', 'keel-module-root'] },
      { id: 'go::test::log::TestLog', parent_id: 'go::pkg::log', label: 'TestLog', kind: 'test', framework: 'go', runner: 'go-test', runner_label: 'Go test', runnable: true, profiles: ['run'], required_resources: ['go-toolchain', 'keel-module-root'] },
      { id: 'go::test::log::TestMetrics', parent_id: 'go::pkg::log', label: 'TestMetrics', kind: 'test', framework: 'go', runner: 'go-test', runner_label: 'Go test', runnable: true, profiles: ['run'], required_resources: ['go-toolchain', 'keel-module-root'] },
      { id: 'keel::lane::smoke', parent_id: 'keel::lanes', label: 'Smoke', kind: 'lane', framework: 'keel', runner: 'keel-dev', runner_label: 'Keel devtool', runnable: true, profiles: ['run'] },
      { id: 'alias::keel::lane::smoke::keel::test::agents/test_memory.go::TestRecall', parent_id: 'keel::lane::smoke', label: 'TestRecall', kind: 'test', framework: 'keel', runner: 'go-test', runner_label: 'Go test', canonical_id: 'keel::test::agents/test_memory.go::TestRecall', runnable: true, profiles: ['run'] },
      { id: 'keel::lane::test-coverage', parent_id: 'keel::lanes', label: 'test-coverage', kind: 'lane', framework: 'keel', runner: 'keel-dev', runner_label: 'Keel devtool', runnable: true, profiles: ['coverage'] },
      { id: 'keel::lane::vsix-ci', parent_id: 'keel::lanes', label: 'b.10 vsix ci', sort_text: 'b.010', kind: 'lane', framework: 'keel', runner: 'keel-dev', runner_label: 'Keel devtool', runnable: true, profiles: ['run'], required_resources: ['go-toolchain', 'keel-module-root', 'stub-binaries', 'pnpm'] },
      { id: 'keel::lane::ci', parent_id: 'keel::lanes', label: 'b.30 ci', sort_text: 'b.030', kind: 'lane', framework: 'keel', runner: 'keel-dev', runner_label: 'Keel devtool', runnable: true, profiles: ['run'], required_resources: ['go-toolchain', 'keel-module-root', 'stub-binaries'] },
      { id: 'keel::maintenance::unlock', parent_id: 'keel::maintenance', label: 'a.2 unlock test bridge', sort_text: 'a.002', kind: 'maintenance', framework: 'keel', runner: 'keel-dev', runner_label: 'Keel devtool', runnable: true, profiles: ['run'] },
      { id: 'keel::maintenance::clear-results', parent_id: 'keel::maintenance', label: 'a.3 clear test results', sort_text: 'a.003', kind: 'maintenance', framework: 'keel', runner: 'keel-dev', runner_label: 'Keel devtool', runnable: true, profiles: ['run'] },
      { id: 'keel::maintenance::clear-state', parent_id: 'keel::maintenance', label: 'a.4 clear local test state', sort_text: 'a.004', kind: 'maintenance', framework: 'keel', runner: 'keel-dev', runner_label: 'Keel devtool', runnable: true, profiles: ['run'] }
    ]
  }, null, 2));
  process.stdout.write('\n');
  process.exit(0);
}

if (args.slice(0, 4).join(' ') === 'vscode tests plan --format') {
  const ids = [];
  for (let i = 5; i < args.length; i += 1) {
    if (args[i] === '--id' && args[i + 1]) {
      ids.push(args[i + 1]);
      i += 1;
    }
  }
  process.stdout.write(JSON.stringify({
    version: 1,
    workspace: process.cwd(),
    generated_at: now(),
    items: ids.map((id) => ({ id, runnable: true, framework: 'openbrain', runner: 'python-test', runner_label: 'Python test' })),
    required_resources: ['python'],
    desired_state: [{ resource: 'python', kind: 'tool', desired: 'available', current: 'available', status: 'satisfied', action: 'reuse', message: 'python available', reusable: true, owned: false }],
    checks: [{ id: 'python', ok: true, message: 'python available' }],
    actions: [{ resource: 'python', status: 'reuse', message: 'python available', reusable: true, owned: false }],
    teardown: { owned_temporary_resources: [], shared_reusable_resources: ['python'], policy: 'reuse fake python' }
  }, null, 2));
  process.stdout.write('\n');
  process.exit(0);
}

if (args.slice(0, 3).join(' ') === 'vscode tests run') {
  const ids = [];
  for (let i = 3; i < args.length; i += 1) {
    if (args[i] === '--id' && args[i + 1]) {
      ids.push(args[i + 1]);
      i += 1;
    }
  }
  const emit = (event) => process.stdout.write(`${JSON.stringify({ version: 1, time: now(), run_id: 'fake-run', ...event })}\n`);
  emit({ event: 'run_started', message: 'OpenBrain fake test run started' });
  const selected = ids[0] ?? 'keel::test::agents/test_memory.go::TestRecall';
  const blockedLane = readBlockedLane();
  if (blockedLane && selected === blockedLane) {
    emit({ event: 'failed', test_id: selected, message: `lane blocked: ${blockedLane}` });
    emit({ event: 'run_finished', exit_code: 1 });
    process.exit(1);
  }
  emit({ event: 'test_started', test_id: selected });
  if (selected === 'keel::maintenance::clear-state' || selected === 'keel::maintenance::clear-results' || selected === 'keel::maintenance::unlock') {
    emit({ event: 'output', test_id: selected, message: `completed ${selected}` });
    emit({ event: 'passed', test_id: selected, duration_ms: 1 });
    emit({ event: 'run_finished', exit_code: 0 });
    process.exit(0);
  }
  emit({ event: 'artifact', test_id: selected, artifact: { name: 'fake log', uri: '/tmp/openbrain-fake.log', kind: 'log' } });
  emit({ event: 'passed', test_id: 'keel::test::agents/test_memory.go::TestRecall', duration_ms: 12 });
  emit({ event: 'run_finished', exit_code: 0 });
  process.exit(0);
}

if (args.slice(0, 3).join(' ') === 'vscode demo status') {
  process.stdout.write(JSON.stringify({
    blocked_lane: readBlockedLane(),
    source: readBlockedLane() ? 'state' : 'none',
    path: blockStatePath()
  }));
  process.stdout.write('\n');
  process.exit(0);
}

if (args.slice(0, 3).join(' ') === 'vscode demo block') {
  if (!args[3]) {
    process.exit(2);
  }
  fs.mkdirSync(path.dirname(blockStatePath()), { recursive: true });
  fs.writeFileSync(blockStatePath(), JSON.stringify({ blocked_lane: args[3], updated_at: now() }) + '\n');
  process.exit(0);
}

if (args.slice(0, 3).join(' ') === 'vscode demo unblock') {
  fs.rmSync(blockStatePath(), { force: true });
  process.exit(0);
}

process.stderr.write(`unsupported fake adapter command: ${args.join(' ')}\n`);
process.exit(2);
