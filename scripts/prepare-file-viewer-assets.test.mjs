import assert from 'node:assert/strict';
import { execFile } from 'node:child_process';
import { mkdtemp, readFile, rm } from 'node:fs/promises';
import { promisify } from 'node:util';
import { exists } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const execFileAsync = promisify(execFile);
const projectRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const scriptPath = join(projectRoot, 'scripts/prepare-file-viewer-assets.mjs');

const assetExists = (path) => new Promise((resolveExists, reject) => {
  exists(path, (result) => resolveExists(result));
});

test('stages only the Office renderer asset set', async () => {
  const target = await mkdtemp(join(tmpdir(), 'pi-web-file-viewer-'));

  try {
    await execFileAsync(process.execPath, [scriptPath, '--target', target], {
      cwd: projectRoot,
    });

    assert.equal(await assetExists(join(target, 'vendor/docx/docx.worker.js')), true);
    assert.equal(await assetExists(join(target, 'vendor/xlsx/sheet.worker.js')), true);
    assert.equal(await assetExists(join(target, 'vendor/pptx/pptx.worker.js')), true);
    assert.equal(await assetExists(join(target, 'vendor/pdf/pdf.worker.mjs')), true);
    assert.equal(await assetExists(join(target, 'wasm/cad/libredwg-web.wasm')), false);
    assert.equal(await assetExists(join(target, 'vendor/3d')), false);
    assert.equal(await assetExists(join(target, 'vendor/typst')), false);

    const manifest = JSON.parse(await readFile(join(target, 'manifest.json'), 'utf8'));
    assert.deepEqual(manifest.packages, {
      '@file-viewer/assets-standard': '3.1.2',
      '@file-viewer/preset-office': '3.1.2',
      '@file-viewer/web': '3.1.2',
    });
    assert.ok(manifest.files.length > 0);
    assert.ok(manifest.files.every(({ sha256, path }) => sha256 && path));
  } finally {
    await rm(target, { recursive: true, force: true });
  }
});
