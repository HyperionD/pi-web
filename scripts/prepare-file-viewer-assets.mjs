import { createHash } from 'node:crypto';
import { execFile } from 'node:child_process';
import { copyFile, lstat, mkdir, mkdtemp, readFile, readdir, rename, rm, writeFile } from 'node:fs/promises';
import { dirname, join, relative, resolve, sep } from 'node:path';
import { promisify } from 'node:util';
import { fileURLToPath } from 'node:url';

const execFileAsync = promisify(execFile);
const projectRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const assetCli = join(projectRoot, 'node_modules', '.bin', 'file-viewer-assets-standard');
const packageNames = [
  '@file-viewer/assets-standard',
  '@file-viewer/preset-office',
  '@file-viewer/web',
];
const officeRendererIds = new Set([
  'pdf',
  'office-word-openxml',
  'spreadsheet-openxml',
  'office-presentation',
]);
const generatedFiles = new Set([
  'file-viewer-assets.ledger.json',
  'file-viewer-assets-standard.receipt.json',
  'flyfish-viewer-assets.json',
  'flyfish-viewer-manifest.json',
]);

const sha256 = (content) => createHash('sha256').update(content).digest('hex');

function safeRelativePath(root, candidate) {
  const normalized = candidate.replaceAll(sep, '/');
  if (!normalized || normalized.startsWith('/') || normalized.split('/').includes('..')) {
    throw new Error(`Unsafe asset path: ${candidate}`);
  }
  const absolute = resolve(root, normalized);
  const rootWithSeparator = `${resolve(root)}${sep}`;
  if (!absolute.startsWith(rootWithSeparator)) {
    throw new Error(`Asset path escapes staging root: ${candidate}`);
  }
  return normalized;
}

async function listFiles(root, current = root) {
  const entries = await readdir(current, { withFileTypes: true });
  const files = [];
  for (const entry of entries) {
    const absolute = join(current, entry.name);
    const relativePath = safeRelativePath(root, relative(root, absolute));
    if (entry.isSymbolicLink()) {
      throw new Error(`Refusing symbolic link in asset output: ${relativePath}`);
    }
    if (entry.isDirectory()) {
      files.push(...await listFiles(root, absolute));
    } else if (entry.isFile()) {
      files.push(relativePath);
    } else {
      throw new Error(`Unexpected asset output entry: ${relativePath}`);
    }
  }
  return files.sort();
}

async function readPinnedVersions() {
  const packageJson = JSON.parse(await readFile(join(projectRoot, 'package.json'), 'utf8'));
  const dependencies = packageJson.dependencies || {};
  const versions = Object.fromEntries(packageNames.map((name) => [name, dependencies[name]]));
  const missing = packageNames.filter((name) => !/^\d+\.\d+\.\d+$/.test(versions[name] || ''));
  if (missing.length || new Set(Object.values(versions)).size !== 1) {
    throw new Error(`File Viewer packages must share one exact version: ${missing.join(', ') || 'version mismatch'}`);
  }
  return versions;
}

async function runAssetCli(sourceDir) {
  try {
    await execFileAsync(assetCli, [sourceDir], { cwd: projectRoot });
  } catch (error) {
    throw new Error(`File Viewer asset CLI failed: ${error.stderr || error.message}`, { cause: error });
  }
}

function getAssetPaths(manifest, rendererIds) {
  const renderers = manifest.rendererAssetManifests.filter(({ rendererId }) => !rendererIds || rendererIds.has(rendererId));
  const paths = renderers.flatMap(({ assets }) => assets.map(({ defaultPath }) => defaultPath));
  return [...new Set(paths)];
}

function getOfficeAssetPaths(manifest) {
  const paths = getAssetPaths(manifest, officeRendererIds);
  if (paths.length === 0 || ![...officeRendererIds].every((rendererId) =>
    manifest.rendererAssetManifests.some((item) => item.rendererId === rendererId))) {
    throw new Error('The standard asset manifest is missing an Office renderer group.');
  }
  return paths;
}

function matchesAssetPath(path, assetPaths) {
  return assetPaths.some((assetPath) => assetPath.endsWith('/')
    ? path.startsWith(assetPath)
    : path === assetPath);
}

function assertAssetGroupsPresent(files, assetPaths) {
  for (const assetPath of assetPaths) {
    const present = assetPath.endsWith('/')
      ? files.some((file) => file.startsWith(assetPath))
      : files.includes(assetPath);
    if (!present) {
      throw new Error(`Required Office asset is missing: ${assetPath}`);
    }
  }
}

async function stageSelectedAssets(sourceDir, stageDir, versions) {
  const sourceManifest = JSON.parse(await readFile(
    join(projectRoot, 'node_modules/@file-viewer/assets-standard/viewer/flyfish-viewer-assets.json'),
    'utf8',
  ));
  if (sourceManifest.packageName !== '@file-viewer/assets-standard' || sourceManifest.packageVersion !== versions['@file-viewer/assets-standard']) {
    throw new Error('Installed standard asset manifest does not match package.json.');
  }
  const assetPaths = getOfficeAssetPaths(sourceManifest);
  const knownAssetPaths = getAssetPaths(sourceManifest);
  const sourceFiles = await listFiles(sourceDir);
  const outputFiles = sourceFiles.filter((path) => !generatedFiles.has(path));
  const unexpected = outputFiles.filter((path) => !matchesAssetPath(path, knownAssetPaths));
  const selectedFiles = outputFiles.filter((path) => matchesAssetPath(path, assetPaths));
  if (unexpected.length) {
    throw new Error(`Unexpected non-Office asset output: ${unexpected.join(', ')}`);
  }
  assertAssetGroupsPresent(selectedFiles, assetPaths);

  const files = [];
  for (const path of selectedFiles) {
    const source = join(sourceDir, path);
    const destination = join(stageDir, path);
    const info = await lstat(source);
    if (!info.isFile()) {
      throw new Error(`Asset output is not a regular file: ${path}`);
    }
    const content = await readFile(source);
    await mkdir(dirname(destination), { recursive: true });
    await copyFile(source, destination);
    files.push({ path, size: content.byteLength, sha256: sha256(content) });
  }
  files.sort((left, right) => left.path.localeCompare(right.path));
  await writeFile(join(stageDir, 'manifest.json'), `${JSON.stringify({
    schemaVersion: 1,
    packages: versions,
    files,
  }, null, 2)}\n`);
}

async function replaceTarget(stageDir, targetDir) {
  const parentDir = dirname(targetDir);
  await mkdir(parentDir, { recursive: true });
  const backupDir = `${targetDir}.previous-${process.pid}`;
  let movedPrevious = false;
  try {
    try {
      const info = await lstat(targetDir);
      if (info.isSymbolicLink() || !info.isDirectory()) {
        throw new Error(`Asset target must be a directory: ${targetDir}`);
      }
      await rm(backupDir, { recursive: true, force: true });
      await rename(targetDir, backupDir);
      movedPrevious = true;
    } catch (error) {
      if (error.code !== 'ENOENT') throw error;
    }
    await rename(stageDir, targetDir);
  } catch (error) {
    if (movedPrevious) await rename(backupDir, targetDir);
    throw error;
  }
  if (movedPrevious) await rm(backupDir, { recursive: true, force: true });
}

function parseTargetArgument(args) {
  if (args.length === 0) return undefined;
  if (args.length === 2 && args[0] === '--target' && args[1]) return args[1];
  throw new Error('Usage: prepare-file-viewer-assets.mjs [--target <directory>]');
}

async function prepareAssets(targetInput) {
  const versions = await readPinnedVersions();
  const targetDir = resolve(targetInput || join(projectRoot, 'static/file-viewer'));
  await mkdir(dirname(targetDir), { recursive: true });
  const operationRoot = await mkdtemp(join(dirname(targetDir), '.pi-web-file-viewer-'));
  const sourceDir = join(operationRoot, 'source');
  const stageDir = join(operationRoot, 'staged');
  try {
    await runAssetCli(sourceDir);
    await stageSelectedAssets(sourceDir, stageDir, versions);
    await replaceTarget(stageDir, targetDir);
    console.log(`Staged Office viewer assets in ${targetDir}`);
  } finally {
    await rm(operationRoot, { recursive: true, force: true });
  }
}

await prepareAssets(parseTargetArgument(process.argv.slice(2)));
