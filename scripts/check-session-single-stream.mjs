import { readdir, readFile } from "node:fs/promises";
import { join, relative } from "node:path";
import { fileURLToPath } from "node:url";

const root = fileURLToPath(new URL("..", import.meta.url));
const compat = process.argv.includes("--compat");
const sourceRoots = ["internal/piweb", "src/pi-app"];
const sourceExtensions = new Set([".go", ".ts", ".tsx", ".astro", ".js", ".mjs"]);

const rules = [
  { name: "broker publish", matches: (line) => /\b(?:Broker|broker|b)\.Publish\s*\(/.test(line) },
  { name: "legacy message append", matches: (line) => /appendMessageAndPublish\s*\(/.test(line) },
  { name: "legacy session subscription", matches: (line) => /\bsessionEvents\s*\(/.test(line) },
  {
    name: "legacy session events route",
    matches: (line) => line.includes("/api/sessions/") && line.includes("/events") && !line.includes("events-v2"),
  },
  { name: "legacy session event adapter", matches: (line) => line.includes("legacy-session-event-adapter") },
];

async function sourceFiles(directory) {
  const entries = await readdir(join(root, directory), { withFileTypes: true });
  const files = [];
  for (const entry of entries) {
    const path = join(directory, entry.name);
    if (entry.isDirectory()) {
      files.push(...await sourceFiles(path));
      continue;
    }
    const extension = entry.name.slice(entry.name.lastIndexOf("."));
    if (!sourceExtensions.has(extension) || isTestFile(entry.name)) continue;
    files.push(path);
  }
  return files;
}

function isTestFile(name) {
  return name.endsWith("_test.go") || name.endsWith(".test.ts") || name.endsWith(".test.tsx");
}

function isCompatFile(path) {
  return path.endsWith("src/pi-app/sessions/legacy-session-event-adapter.ts");
}

const files = (await Promise.all(sourceRoots.map(sourceFiles))).flat().sort();
const violations = [];
for (const file of files) {
  if (compat && isCompatFile(file)) continue;
  const contents = await readFile(join(root, file), "utf8");
  for (const [index, line] of contents.split("\n").entries()) {
    for (const rule of rules) {
      if (rule.matches(line)) {
        violations.push(`${relative(root, file)}:${index + 1}: ${rule.name}: ${line.trim()}`);
      }
    }
  }
}

if (violations.length > 0) {
  console.error("Session single-stream check failed:");
  for (const violation of violations) console.error(`- ${violation}`);
  process.exitCode = 1;
} else {
  console.log(`Session single-stream check passed (${compat ? "compat" : "strict"} mode).`);
}
