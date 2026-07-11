const fs = require('node:fs');
const path = require('node:path');
const Mocha = require('mocha');

const {
  EVENT_RUN_BEGIN,
  EVENT_RUN_END,
  EVENT_TEST_BEGIN,
  EVENT_TEST_PASS,
  EVENT_TEST_FAIL,
  EVENT_TEST_PENDING
} = Mocha.Runner.constants;

class JsonlReporter {
  constructor(runner) {
    const output = process.env.KEEL_VSCODE_MOCHA_JSONL || path.join('.vscode-test', 'results.jsonl');
    fs.mkdirSync(path.dirname(output), { recursive: true });
    const stream = fs.createWriteStream(output, { flags: 'w' });
    const startedAt = Date.now();
    let passes = 0;
    let failures = 0;
    let pending = 0;

    const write = (event, fields = {}) => {
      stream.write(`${JSON.stringify({ version: 1, event, time: new Date().toISOString(), ...fields })}\n`);
    };

    runner.once(EVENT_RUN_BEGIN, () => {
      write('run_started');
    });
    runner.on(EVENT_TEST_BEGIN, (test) => {
      write('test_started', testFields(test));
    });
    runner.on(EVENT_TEST_PASS, (test) => {
      passes += 1;
      write('passed', testFields(test));
    });
    runner.on(EVENT_TEST_FAIL, (test, err) => {
      failures += 1;
      write('failed', {
        ...testFields(test),
        message: err && err.message ? err.message : String(err)
      });
    });
    runner.on(EVENT_TEST_PENDING, (test) => {
      pending += 1;
      write('skipped', testFields(test));
    });
    runner.once(EVENT_RUN_END, () => {
      write('run_finished', {
        passes,
        failures,
        pending,
        duration_ms: Date.now() - startedAt
      });
      stream.end();
      process.stdout.write(`Mocha JSONL results: ${output}\n`);
      process.stdout.write(`Mocha summary: ${passes} passed, ${failures} failed, ${pending} skipped\n`);
    });
  }
}

function testFields(test) {
  return {
    title: test.title,
    full_title: typeof test.fullTitle === 'function' ? test.fullTitle() : test.title,
    file: test.file,
    duration_ms: typeof test.duration === 'number' ? test.duration : 0
  };
}

module.exports = JsonlReporter;
