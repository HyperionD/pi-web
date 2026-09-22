import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import { execFile } from 'node:child_process';
import { lstat, mkdir, mkdtemp, readFile, rm, writeFile } from 'node:fs/promises';
import { promisify } from 'node:util';
import { tmpdir } from 'node:os';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import {
  replaceTarget,
  safeRelativePath,
  validateManifest,
} from './prepare-file-viewer-assets.mjs';

const execFileAsync = promisify(execFile);
const projectRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const scriptPath = join(projectRoot, 'scripts/prepare-file-viewer-assets.mjs');

const runScript = (target) => execFileAsync(process.execPath, [scriptPath, '--target', target], {
  cwd: projectRoot,
});

const digest = (content) => createHash('sha256').update(content).digest('hex');

async function assetExists(path) {
  try {
    await lstat(path);
    return true;
  } catch (error) {
    if (error.code === 'ENOENT') return false;
    throw error;
  }
}

test('stages only the Office renderer asset set and records verifiable hashes', async () => {
  const target = await mkdtemp(join(tmpdir(), 'pi-web-file-viewer-'));

  try {
    await runScript(target);

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
    for (const entry of manifest.files) {
      const content = await readFile(join(target, entry.path));
      assert.equal(entry.size, content.byteLength, entry.path);
      assert.equal(entry.sha256, digest(content), entry.path);
    }
  } finally {
    await rm(target, { recursive: true, force: true });
  }
});

test('rejects dangerous target roots before modifying them', async () => {
  const dangerousTargets = [projectRoot, join(projectRoot, 'static'), tmpdir(), '/'];

  for (const target of dangerousTargets) {
    await assert.rejects(runScript(target), (error) => {
      const output = `${error.stdout || ''}${error.stderr || ''}`;
      return error.code !== 0 && /Unsafe asset target/.test(output);
    }, target);
  }
});

test('rejects traversal and absolute paths in generated output', () => {
  const root = '/tmp/pi-web-file-viewer-test';
  for (const candidate of ['../outside', 'vendor/../../outside', '/absolute', 'C:/absolute', 'vendor\\\\..\\\\outside', '']) {
    assert.throws(() => safeRelativePath(root, candidate), /Unsafe asset path/);
  }
});

test('validates every renderer defaultPath before selecting Office assets', () => {
  const validManifest = {
    rendererAssetManifests: [{ rendererId: 'office', assets: [{ defaultPath: 'vendor/office/' }] }],
  };
  assert.doesNotThrow(() => validateManifest(validManifest));

  for (const defaultPath of ['../outside', '/absolute', 'C:/absolute', 'vendor\\\\..\\\\outside', '']) {
    assert.throws(() => validateManifest({
      rendererAssetManifests: [{ rendererId: 'unrelated', assets: [{ defaultPath }] }],
    }), /Invalid manifest defaultPath/);
  }
});

test('atomically replaces an existing target without leaving the old tree', async () => {
  const workspace = await mkdtemp(join(tmpdir(), 'pi-web-file-viewer-'));
  const target = join(workspace, 'target');
  const stage = join(workspace, 'stage');
  try {
    await mkdir(target);
    await writeFile(join(target, 'old.txt'), 'old');
    await mkdir(stage);
    await writeFile(join(stage, 'new.txt'), 'new');

    await replaceTarget(stage, target);

    assert.equal(await readFile(join(target, 'new.txt'), 'utf8'), 'new');
    await assert.rejects(readFile(join(target, 'old.txt'), 'utf8'), { code: 'ENOENT' });
  } finally {
    await rm(workspace, { recursive: true, force: true });
  }
});

test('preserves both trees when atomic replacement fails', async () => {
  const workspace = await mkdtemp(join(tmpdir(), 'pi-web-file-viewer-'));
  const target = join(workspace, 'target');
  const stage = join(workspace, 'stage');
  try {
    await mkdir(target);
    await writeFile(join(target, 'old.txt'), 'old');
    await mkdir(stage);
    await writeFile(join(stage, 'new.txt'), 'new');

    await assert.rejects(replaceTarget(stage, target, {
      exchange: async () => { throw new Error('injected exchange failure'); },
    }), /injected exchange failure/);

    assert.equal(await readFile(join(target, 'old.txt'), 'utf8'), 'old');
    assert.equal(await readFile(join(stage, 'new.txt'), 'utf8'), 'new');
  } finally {
    await rm(workspace, { recursive: true, force: true });
  }
});

test('rolls back the exchange when cleanup fails', async () => {
  const workspace = await mkdtemp(join(tmpdir(), 'pi-web-file-viewer-'));
  const target = join(workspace, 'target');
  const stage = join(workspace, 'stage');
  try {
    await mkdir(target);
    await writeFile(join(target, 'old.txt'), 'old');
    await mkdir(stage);
    await writeFile(join(stage, 'new.txt'), 'new');

    await assert.rejects(replaceTarget(stage, target, {
      remove: async () => { throw new Error('injected cleanup failure'); },
    }), /injected cleanup failure/);

    assert.equal(await readFile(join(target, 'old.txt'), 'utf8'), 'old');
    assert.equal(await readFile(join(stage, 'new.txt'), 'utf8'), 'new');
  } finally {
    await rm(workspace, { recursive: true, force: true });
  }
});
