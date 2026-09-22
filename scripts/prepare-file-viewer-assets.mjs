import { createHash } from 'node:crypto';
import { execFile } from 'node:child_process';
import { copyFile, lstat, mkdir, mkdtemp, readFile, readdir, realpath, rename, rm, writeFile } from 'node:fs/promises';
import { dirname, join, relative, resolve, sep } from 'node:path';
import { tmpdir } from 'node:os';
import { promisify } from 'node:util';
import { fileURLToPath } from 'node:url';

const execFileAsync = promisify(execFile);
const scriptPath = fileURLToPath(import.meta.url);
const projectRoot = resolve(dirname(scriptPath), '..');
const staticRoot = resolve(projectRoot, 'static');
const defaultTarget = resolve(staticRoot, 'file-viewer');
const tempRoot = resolve(tmpdir());
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
const forbiddenResourceSegments = new Set(['cad', '3d', 'typst']);
const renameExchangeScript = `
import ctypes
import os
import platform
import sys

numbers = {'x86_64': 316, 'amd64': 316, 'aarch64': 276, 'arm64': 276, 'ppc64le': 357}
number = numbers.get(platform.machine())
if number is None:
    raise SystemExit('renameat2 is unsupported on this architecture')
libc = ctypes.CDLL(None, use_errno=True)
libc.syscall.restype = ctypes.c_long
result = libc.syscall(
    ctypes.c_long(number),
    ctypes.c_int(-100), ctypes.c_char_p(os.fsencode(sys.argv[1])),
    ctypes.c_int(-100), ctypes.c_char_p(os.fsencode(sys.argv[2])),
    ctypes.c_uint(2),
)
if result != 0:
    error = ctypes.get_errno()
    raise OSError(error, os.strerror(error))
`;

const sha256 = (content) => createHash('sha256').update(content).digest('hex');
const isWithin = (parent, candidate) => candidate !== parent && candidate.startsWith(`${parent}${sep}`);

function safeRelativePath(root, candidate) {
  if (typeof candidate !== 'string' || candidate.includes('\0')) {
    throw new Error(`Unsafe asset path: ${candidate}`);
  }
  const normalized = candidate.replaceAll('\\', '/');
  const segments = normalized.split('/');
  if (!normalized || normalized.startsWith('/') || /^[A-Za-z]:\//.test(normalized)
    || segments.some((segment) => !segment || segment === '.' || segment === '..')) {
    throw new Error(`Unsafe asset path: ${candidate}`);
  }
  const absolute = resolve(root, normalized);
  const rootWithSeparator = `${resolve(root)}${sep}`;
  if (!absolute.startsWith(rootWithSeparator)) {
    throw new Error(`Asset path escapes staging root: ${candidate}`);
  }
  return normalized;
}

function validateManifestPath(candidate, label) {
  if (typeof candidate !== 'string' || candidate.length === 0 || candidate.includes('\0')) {
    throw new Error(`Invalid manifest defaultPath at ${label}`);
  }
  if (candidate.includes('\\')) {
    throw new Error(`Invalid manifest defaultPath at ${label}`);
  }
  const segments = candidate.split('/');
  const pathSegments = candidate.endsWith('/') ? segments.slice(0, -1) : segments;
  if (candidate.startsWith('/') || /^[A-Za-z]:\//.test(candidate)
    || pathSegments.length === 0
    || pathSegments.some((segment) => !segment || segment === '.' || segment === '..')) {
    throw new Error(`Invalid manifest defaultPath at ${label}`);
  }
  return candidate;
}

function validateManifest(manifest) {
  if (!manifest || !Array.isArray(manifest.rendererAssetManifests)) {
    throw new Error('The standard asset manifest has no renderer asset manifests.');
  }
  for (const [rendererIndex, renderer] of manifest.rendererAssetManifests.entries()) {
    if (!renderer || typeof renderer.rendererId !== 'string' || !Array.isArray(renderer.assets)) {
      throw new Error(`Invalid renderer asset manifest at index ${rendererIndex}.`);
    }
    for (const [assetIndex, asset] of renderer.assets.entries()) {
      validateManifestPath(asset?.defaultPath, `${renderer.rendererId}[${assetIndex}]`);
    }
  }
  return manifest;
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
  validateManifest(manifest);
  const renderers = manifest.rendererAssetManifests.filter(({ rendererId }) => !rendererIds || rendererIds.has(rendererId));
  const paths = renderers.flatMap(({ assets }) => assets.map(({ defaultPath }) => defaultPath));
  return [...new Set(paths)];
}

function isForbiddenResourcePath(path) {
  return path.split('/').some((segment) => forbiddenResourceSegments.has(segment));
}

function getOfficeAssetPaths(manifest) {
  const paths = getAssetPaths(manifest, officeRendererIds);
  if (paths.length === 0 || ![...officeRendererIds].every((rendererId) =>
    manifest.rendererAssetManifests.some((item) => item.rendererId === rendererId))) {
    throw new Error('The standard asset manifest is missing an Office renderer group.');
  }
  const forbiddenPaths = paths.filter(isForbiddenResourcePath);
  if (forbiddenPaths.length) {
    throw new Error(`Refusing forbidden non-Office asset path: ${forbiddenPaths.join(', ')}`);
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
  validateManifest(sourceManifest);
  if (sourceManifest.packageName !== '@file-viewer/assets-standard'
    || sourceManifest.packageVersion !== versions['@file-viewer/assets-standard']) {
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

async function atomicExchange(left, right) {
  if (process.platform !== 'linux') {
    throw new Error('Atomic replacement requires Linux renameat2 support.');
  }
  try {
    await execFileAsync(process.env.PYTHON || 'python3', ['-c', renameExchangeScript, left, right]);
  } catch (error) {
    throw new Error(`Atomic asset replacement is unavailable: ${error.stderr || error.message}`, { cause: error });
  }
}

async function verifyStagingSupport(operationRoot) {
  if (process.platform !== 'linux') {
    throw new Error('Office asset staging requires Linux, Python 3, and renameat2 support.');
  }
  const left = join(operationRoot, 'rename-probe-left');
  const right = join(operationRoot, 'rename-probe-right');
  await mkdir(left);
  await mkdir(right);
  try {
    await atomicExchange(left, right);
  } catch (error) {
    throw new Error(`Office asset staging requires Linux, Python 3, and renameat2 support: ${error.message}`, {
      cause: error,
    });
  } finally {
    await rm(left, { recursive: true, force: true });
    await rm(right, { recursive: true, force: true });
  }
}

async function replaceTarget(stageDir, targetDir, { exchange = atomicExchange, remove = rm } = {}) {
  const parentDir = dirname(targetDir);
  await mkdir(parentDir, { recursive: true });
  let targetExists = false;
  try {
    const info = await lstat(targetDir);
    targetExists = true;
    if (info.isSymbolicLink() || !info.isDirectory()) {
      throw new Error(`Asset target must be a directory: ${targetDir}`);
    }
  } catch (error) {
    if (error.code !== 'ENOENT') throw error;
  }

  if (!targetExists) {
    await rename(stageDir, targetDir);
    return;
  }

  await exchange(stageDir, targetDir);
  try {
    await remove(stageDir, { recursive: true, force: true });
  } catch (error) {
    const details = error instanceof Error ? error.message : String(error);
    throw new Error(`Asset replacement succeeded, but cleanup of the previous target failed: ${details}`, {
      cause: error,
    });
  }
}

async function existingAncestor(path) {
  let current = path;
  while (true) {
    try {
      const info = await lstat(current);
      if (info.isSymbolicLink()) throw new Error(`Asset target path cannot contain a symbolic link: ${path}`);
      return current;
    } catch (error) {
      if (error.code !== 'ENOENT') throw error;
      const parent = dirname(current);
      if (parent === current) throw new Error(`Asset target parent does not exist: ${path}`);
      current = parent;
    }
  }
}

async function validateTarget(targetInput) {
  const target = resolve(targetInput || defaultTarget);
  const relativeToTemp = relative(tempRoot, target);
  const firstTempSegment = relativeToTemp.split(sep)[0];
  const isApprovedTempTarget = isWithin(tempRoot, target)
    && /^pi-web-file-viewer-[A-Za-z0-9._-]+$/.test(firstTempSegment);
  if (target !== defaultTarget && !isApprovedTempTarget) {
    throw new Error(`Unsafe asset target: ${target}. Use ${defaultTarget} or a dedicated pi-web-file-viewer-* temp directory.`);
  }

  const anchor = await existingAncestor(target);
  const canonicalAnchor = await realpath(anchor);
  if (target === defaultTarget) {
    if (!isWithin(projectRoot, canonicalAnchor) && canonicalAnchor !== projectRoot) {
      throw new Error(`Unsafe asset target: ${target}`);
    }
  } else if (!isWithin(tempRoot, canonicalAnchor) && canonicalAnchor !== tempRoot) {
    throw new Error(`Unsafe asset target: ${target}`);
  }
  return target;
}

function parseTargetArgument(args) {
  if (args.length === 0) return undefined;
  if (args.length === 2 && args[0] === '--target' && args[1]) return args[1];
  throw new Error('Usage: prepare-file-viewer-assets.mjs [--target <directory>]');
}

async function prepareAssets(targetInput) {
  const targetDir = await validateTarget(targetInput);
  const versions = await readPinnedVersions();
  await mkdir(dirname(targetDir), { recursive: true });
  const operationRoot = await mkdtemp(join(dirname(targetDir), '.pi-web-file-viewer-'));
  const sourceDir = join(operationRoot, 'source');
  const stageDir = join(operationRoot, 'staged');
  try {
    await verifyStagingSupport(operationRoot);
    await runAssetCli(sourceDir);
    await stageSelectedAssets(sourceDir, stageDir, versions);
    await replaceTarget(stageDir, targetDir);
    console.log(`Staged Office viewer assets in ${targetDir}`);
  } finally {
    await rm(operationRoot, { recursive: true, force: true });
  }
}

export {
  atomicExchange,
  getAssetPaths,
  getOfficeAssetPaths,
  replaceTarget,
  safeRelativePath,
  validateManifest,
  validateManifestPath,
  validateTarget,
};

if (process.argv[1] && resolve(process.argv[1]) === scriptPath) {
  try {
    await prepareAssets(parseTargetArgument(process.argv.slice(2)));
  } catch (error) {
    console.error(error instanceof Error ? error.message : error);
    process.exitCode = 1;
  }
}
