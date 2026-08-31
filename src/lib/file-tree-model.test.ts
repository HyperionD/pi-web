import { describe, expect, it } from "vitest";
import { decorateFileTree, isDirtyStatus } from "./file-tree-model";

const tree = [
  {
    type: "dir",
    name: "src",
    path: "src",
    children: [
      { type: "file", name: "main.ts", path: "src/main.ts" },
      { type: "file", name: "new.ts", path: "src/new.ts" },
      { type: "file", name: "gone.ts", path: "src/gone.ts" },
      { type: "file", name: "renamed.ts", path: "src/renamed.ts" },
      { type: "file", name: "clean.ts", path: "src/clean.ts" },
    ],
  },
  { type: "file", name: "README.md", path: "README.md" },
];

describe("decorateFileTree", () => {
  it("sorts folders before files and sorts each group by name recursively", () => {
    const [assets, docs, alpha, zeta] = decorateFileTree([
      { type: "file", name: "zeta.txt", path: "zeta.txt" },
      {
        type: "dir",
        name: "docs",
        path: "docs",
        children: [
          { type: "file", name: "zeta.md", path: "docs/zeta.md" },
          { type: "dir", name: "B-guides", path: "docs/B-guides" },
          { type: "file", name: "alpha.md", path: "docs/alpha.md" },
          { type: "dir", name: "a-guides", path: "docs/a-guides" },
        ],
      },
      { type: "dir", name: "assets", path: "assets" },
      { type: "file", name: "alpha.txt", path: "alpha.txt" },
    ]);

    expect([assets.name, docs.name, alpha.name, zeta.name]).toEqual(["assets", "docs", "alpha.txt", "zeta.txt"]);
    expect(docs.children?.map((child) => child.name)).toEqual(["a-guides", "B-guides", "alpha.md", "zeta.md"]);
  });

  it("adds git status, selected state, and dirty folder ancestors", () => {
    const [src, readme] = decorateFileTree(tree, {
      "src/main.ts": "modified",
      "src/new.ts": "untracked",
      "src/gone.ts": "deleted",
      "src/renamed.ts": "renamed",
      "README.md": "added",
    }, "src/main.ts", new Set(["src"]));

    expect(src.kind).toBe("dir");
    expect(src.expanded).toBe(true);
    expect(src.dirtyDescendants).toBe(true);
    expect(src.children?.map((child) => [child.path, child.gitStatus, child.selected])).toEqual([
      ["src/clean.ts", "clean", false],
      ["src/gone.ts", "deleted", false],
      ["src/main.ts", "modified", true],
      ["src/new.ts", "untracked", false],
      ["src/renamed.ts", "renamed", false],
    ]);
    expect(readme.gitStatus).toBe("added");
  });

  it("treats unknown statuses as clean", () => {
    const [node] = decorateFileTree([{ type: "file", name: "x", path: "x" }], { x: "ignored" });
    expect(node.gitStatus).toBe("clean");
    expect(node.dirtyDescendants).toBe(false);
  });

  it("normalizes paths, empty input, open folders, status overrides, and dirty statuses", () => {
    expect(decorateFileTree()).toEqual([]);
    const [src, readme] = decorateFileTree([
      { type: "dir", name: "src", path: "./src", open: true, status: "modified", children: [] },
      { type: "file", name: "README.md" },
    ], { src: "clean", "README.md": "added" });
    expect(src).toMatchObject({ id: "src", path: "src", children: undefined, expanded: true, gitStatus: "clean" });
    expect(readme).toMatchObject({ id: "README.md", path: "README.md", gitStatus: "added" });
    for (const status of ["modified", "added", "untracked", "deleted", "renamed"]) {
      expect(isDirtyStatus(status)).toBe(true);
    }
    expect(isDirtyStatus("clean")).toBe(false);
    expect(isDirtyStatus("unknown")).toBe(false);
    expect(isDirtyStatus(undefined)).toBe(false);
  });
});
