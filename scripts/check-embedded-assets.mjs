import { readdir, readFile } from "node:fs/promises";
import path from "node:path";

const ignoredNames = new Set(["pi-web", "README.txt", "file-viewer"]);

async function listFiles(root, directory = "") {
  const entries = await readdir(path.join(root, directory), { withFileTypes: true });
  const files = [];
  for (const entry of entries) {
    if (ignoredNames.has(entry.name)) continue;
    const relativePath = path.join(directory, entry.name);
    if (entry.isDirectory()) files.push(...await listFiles(root, relativePath));
    else if (entry.isFile()) files.push(relativePath);
  }
  return files.sort();
}

async function compareDirectories(expectedRoot, actualRoot) {
  const [expectedFiles, actualFiles] = await Promise.all([listFiles(expectedRoot), listFiles(actualRoot)]);
  const mismatches = [];
  const allFiles = new Set([...expectedFiles, ...actualFiles]);
  for (const relativePath of [...allFiles].sort()) {
    if (!expectedFiles.includes(relativePath)) {
      mismatches.push(`missing from ${expectedRoot}: ${relativePath}`);
      continue;
    }
    if (!actualFiles.includes(relativePath)) {
      mismatches.push(`missing from ${actualRoot}: ${relativePath}`);
      continue;
    }
    const [expected, actual] = await Promise.all([
      readFile(path.join(expectedRoot, relativePath)),
      readFile(path.join(actualRoot, relativePath)),
    ]);
    if (!expected.equals(actual)) mismatches.push(`content differs: ${relativePath}`);
  }
  return mismatches;
}

const mismatches = await compareDirectories("static", "dist");
if (mismatches.length > 0) {
  console.error("Embedded UI assets are stale. Run `bun run embed:assets` after building:");
  for (const mismatch of mismatches) console.error(`- ${mismatch}`);
  process.exitCode = 1;
}
