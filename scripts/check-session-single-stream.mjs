import { readdir, readFile } from "node:fs/promises";
import { join, relative, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const root = fileURLToPath(new URL("..", import.meta.url));
const sourceRoots = ["internal/piweb", "src/pi-app"];
const sourceExtensions = new Set([".go", ".ts", ".tsx", ".astro", ".js", ".mjs"]);
const compatAdapter = "src/pi-app/sessions/legacy-session-event-adapter.ts";

const rules = [
  { name: "broker publish", matches: (line) => /\b(?:Broker|broker|b)\.Publish\s*\(/.test(line) },
  { name: "legacy message append", matches: (line) => /appendMessageAndPublish\s*\(/.test(line) },
  { name: "legacy session subscription", matches: (line) => /\bsessionEvents\s*\(/.test(line) },
  {
    name: "legacy session events route",
    matches: (line) => line.includes("/api/sessions/") && /\/events(?:[^a-zA-Z0-9_-]|$)/.test(line),
  },
  { name: "legacy session event adapter", matches: (line) => line.includes("legacy-session-event-adapter") },
];

async function sourceFiles(repositoryRoot, directory) {
  const entries = await readdir(join(repositoryRoot, directory), { withFileTypes: true });
  const files = [];
  for (const entry of entries) {
    const path = join(directory, entry.name);
    if (entry.isDirectory()) {
      files.push(...await sourceFiles(repositoryRoot, path));
      continue;
    }
    const extension = entry.name.slice(entry.name.lastIndexOf("."));
    if (!sourceExtensions.has(extension) || isTestFile(entry.name)) continue;
    files.push(path);
  }
  return files;
}

function isTestFile(name) {
  return name.endsWith("_test.go") || name.endsWith(".test.ts") || name.endsWith(".test.tsx") || name.endsWith(".test.mjs");
}

function isCompatFile(path) {
  return path.replaceAll("\\", "/") === compatAdapter;
}

export async function checkSessionSingleStream(repositoryRoot = root, options = {}) {
  const compat = options.compat === true;
  const files = (await Promise.all(sourceRoots.map((directory) => sourceFiles(repositoryRoot, directory)))).flat().sort();
  const violations = [];
  for (const file of files) {
    const contents = await readFile(join(repositoryRoot, file), "utf8");
    for (const [index, line] of contents.split("\n").entries()) {
      for (const rule of rules) {
        if (compat && isCompatFile(file) && rule.name !== "broker publish") continue;
        if (rule.matches(line)) {
          violations.push(`${relative(repositoryRoot, join(repositoryRoot, file))}:${index + 1}: ${rule.name}: ${line.trim()}`);
        }
      }
    }
  }
  return violations;
}

export async function main(args = process.argv.slice(2)) {
  const violations = await checkSessionSingleStream(root, { compat: args.includes("--compat") });
  if (violations.length > 0) {
    console.error("Session single-stream check failed:");
    for (const violation of violations) console.error(`- ${violation}`);
    process.exitCode = 1;
    return;
  }
  console.log(`Session single-stream check passed (${args.includes("--compat") ? "compat" : "strict"} mode).`);
}

if (process.argv[1] && resolve(process.argv[1]) === resolve(fileURLToPath(import.meta.url))) {
  await main();
}
