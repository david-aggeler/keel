#!/usr/bin/env node

const args = process.argv.slice(2);
const command = args.slice(0, 4).join(' ');
const now = () => new Date().toISOString();

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
      clear_results_test_ids: ['openbrain::maintenance::clear-results'],
      clear_state_test_ids: ['openbrain::maintenance::clear-state']
    },
    items: [
      { id: 'openbrain::agents', label: 'Agents', kind: 'root', framework: 'openbrain', runner: 'python-test', runner_label: 'Python test', runnable: true, profiles: ['run'] },
      { id: 'openbrain::file::agents/test_memory.py', parent_id: 'openbrain::agents', label: 'test_memory.py', kind: 'file', framework: 'openbrain', runner: 'python-test', runner_label: 'Python test', uri: 'agents/test_memory.py', runnable: true, profiles: ['run'], required_resources: ['python'] },
      { id: 'openbrain::test::agents/test_memory.py::test_recall', parent_id: 'openbrain::file::agents/test_memory.py', label: 'test_recall', kind: 'test', framework: 'openbrain', runner: 'python-test', runner_label: 'Python test', uri: 'agents/test_memory.py', runnable: true, profiles: ['run'], required_resources: ['python'] },
      { id: 'openbrain::test::agents/test_memory.py::test_store', parent_id: 'openbrain::file::agents/test_memory.py', label: 'test_store', kind: 'test', framework: 'openbrain', runner: 'python-test', runner_label: 'Python test', uri: 'agents/test_memory.py', runnable: true, profiles: ['run'], required_resources: ['python'] },
      { id: 'openbrain::lane::smoke', label: 'Smoke', kind: 'lane', framework: 'openbrain', runner: 'openbrain-dev', runner_label: 'OpenBrain devtool', runnable: true, profiles: ['run'] },
      { id: 'alias::openbrain::lane::smoke::openbrain::test::agents/test_memory.py::test_recall', parent_id: 'openbrain::lane::smoke', label: 'test_recall', kind: 'test', framework: 'openbrain', runner: 'python-test', runner_label: 'Python test', canonical_id: 'openbrain::test::agents/test_memory.py::test_recall', runnable: true, profiles: ['run'] },
      { id: 'openbrain::maintenance::clear-results', label: 'clear OpenBrain test results', kind: 'maintenance', framework: 'openbrain', runner: 'openbrain-dev', runner_label: 'OpenBrain devtool', runnable: true, profiles: ['run'] },
      { id: 'openbrain::maintenance::clear-state', label: 'clear OpenBrain local state', kind: 'maintenance', framework: 'openbrain', runner: 'openbrain-dev', runner_label: 'OpenBrain devtool', runnable: true, profiles: ['run'] }
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

if (args.slice(0, 4).join(' ') === 'vscode tests run --format') {
  const ids = [];
  for (let i = 5; i < args.length; i += 1) {
    if (args[i] === '--id' && args[i + 1]) {
      ids.push(args[i + 1]);
      i += 1;
    }
  }
  const emit = (event) => process.stdout.write(`${JSON.stringify({ version: 1, time: now(), run_id: 'fake-run', ...event })}\n`);
  emit({ event: 'run_started', message: 'OpenBrain fake test run started' });
  const selected = ids[0] ?? 'openbrain::test::agents/test_memory.py::test_recall';
  emit({ event: 'test_started', test_id: selected });
  if (selected === 'openbrain::maintenance::clear-state' || selected === 'openbrain::maintenance::clear-results') {
    emit({ event: 'output', test_id: selected, message: `completed ${selected}` });
    emit({ event: 'passed', test_id: selected, duration_ms: 1 });
    emit({ event: 'run_finished', exit_code: 0 });
    process.exit(0);
  }
  emit({ event: 'artifact', test_id: selected, artifact: { name: 'fake log', uri: '/tmp/openbrain-fake.log', kind: 'log' } });
  emit({ event: 'passed', test_id: 'openbrain::test::agents/test_memory.py::test_recall', duration_ms: 12 });
  emit({ event: 'run_finished', exit_code: 0 });
  process.exit(0);
}

process.stderr.write(`unsupported fake adapter command: ${args.join(' ')}\n`);
process.exit(2);
