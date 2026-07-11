import * as assert from 'node:assert/strict';
import * as fs from 'node:fs';
import * as os from 'node:os';
import * as path from 'node:path';
import * as vscode from 'vscode';
import { currentAdapterConfig } from '../../extension';
import { configRelativePath, currentConfigVersion, defaultConfigTemplate, readAdapterConfig } from '../../bridgeAdapter';

suite('Keel Test Bridge config contract', () => {
  // DHF-TEST: keel/requirement-40
  test('manifest exposes Keel identity, command ids, and config-file activation only', () => {
    const manifestPath = path.resolve(__dirname, '../../../package.json');
    const manifest = JSON.parse(fs.readFileSync(manifestPath, 'utf8')) as {
      publisher: string;
      name: string;
      displayName: string;
      license: string;
      activationEvents: string[];
      contributes?: { configuration?: unknown; commands?: Array<{ command: string }> };
    };

    assert.equal(manifest.publisher, 'aggeler');
    assert.equal(manifest.name, 'keel-test-bridge');
    assert.equal(manifest.displayName, 'Keel Test Bridge');
    assert.equal(manifest.license, 'Apache-2.0');
    assert.deepEqual(
      manifest.activationEvents.filter((event) => event.startsWith('workspaceContains:')),
      ['workspaceContains:.vscode/test-bridge.json']
    );
    assert.equal(manifest.contributes?.configuration, undefined);
    const commands = new Set(manifest.contributes?.commands?.map((command) => command.command));
    assert.ok(commands.has('keel.tests.refresh'));
    assert.ok(commands.has('keel.tests.initConfig'));
    assert.ok(!commands.has('vela.tests.refresh'));
  });

  // DHF-TEST: keel/requirement-40
  test('default template is the embedded current Keel config', () => {
    const parsed = JSON.parse(defaultConfigTemplate()) as { version: number; command: string; args: string[]; displayName: string };
    assert.equal(parsed.version, currentConfigVersion);
    assert.equal(parsed.command, 'bin/keel-dev');
    assert.deepEqual(parsed.args, ['vscode', 'tests']);
    assert.equal(parsed.displayName, 'Keel');
  });

  // DHF-TEST: keel/requirement-40
  test('file-backed config tolerantly reads newer versions and ignores unknown fields', () => {
    const root = fs.mkdtempSync(path.join(os.tmpdir(), 'keel-test-bridge-'));
    fs.mkdirSync(path.join(root, '.vscode'));
    fs.writeFileSync(
      path.join(root, configRelativePath),
      JSON.stringify({
        version: currentConfigVersion + 1,
        command: 'bin/future-dev',
        args: ['vscode', 'tests'],
        displayName: 'Future',
        extraFutureField: true
      })
    );

    const cfg = readAdapterConfig(root);
    assert.equal(cfg.version, currentConfigVersion + 1);
    assert.equal(cfg.command, 'bin/future-dev');
    assert.equal(cfg.displayName, 'Future');
  });

  // DHF-TEST: keel/requirement-40
  test('extension activates and registers Keel commands', async () => {
    const extension = vscode.extensions.getExtension('aggeler.keel-test-bridge');
    assert.ok(extension, 'extension should be discoverable');
    await extension.activate();

    const commands = await vscode.commands.getCommands(true);
    assert.ok(commands.includes('keel.tests.refresh'));
    assert.ok(commands.includes('keel.tests.initConfig'));
    assert.ok(commands.includes('keel.tests.clearTestResults'));
    assert.equal(currentAdapterConfig().displayName, 'Keel');
  });
});
