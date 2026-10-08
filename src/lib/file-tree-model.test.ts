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

  it("keeps empty and unloaded directories expandable without making files expandable", () => {
    const [empty, unloaded, file] = decorateFileTree([
      { type: "dir", name: "empty", path: ".worktrees", children: [] },
      { type: "dir", name: "unloaded", path: "deep" },
      { type: "file", name: "file.txt", path: "file.txt" },
    ]);

    expect(empty.children).toEqual([]);
    expect(unloaded.children).toEqual([]);
    expect(file.children).toBeUndefined();
  });

  it("adds unloaded ancestors for backend search hits without mutating the loaded tree", () => {
    const raw = [{ type: "dir", name: "src", path: "src" }];
    const [src] = decorateFileTree(raw, { "src/deep/more/needle.txt": "modified" }, "", new Set(),
      new Set(["src/deep/more/needle.txt", "src/deep/other.txt"]));

    const deep = src.children?.[0];
    expect(deep?.path).toBe("src/deep");
    expect(deep?.children?.map((node) => node.path)).toEqual(["src/deep/more", "src/deep/other.txt"]);
    expect(deep?.children?.[0].children?.[0]).toMatchObject({
      path: "src/deep/more/needle.txt", kind: "file", gitStatus: "modified",
    });
    expect(src.dirtyDescendants).toBe(true);
    expect(raw).toEqual([{ type: "dir", name: "src", path: "src" }]);
    expect(decorateFileTree(raw)[0].children).toEqual([]);
  });

  it("keeps existing search hits unique and preserves their metadata", () => {
    const [docs] = decorateFileTree([{ type: "dir", name: "docs", path: "docs", children: [
      { type: "file", name: "guide.md", path: "docs/guide.md", status: "added" },
    ] }], {}, "docs/guide.md", new Set(), new Set(["docs/guide.md", "docs/nested/guide.md"]));

    expect(docs.children?.map((node) => node.path)).toEqual(["docs/nested", "docs/guide.md"]);
    expect(docs.children?.[1]).toMatchObject({ gitStatus: "added", selected: true });
  });

  it("ignores search hit paths that escape the workspace or contain empty segments", () => {
    const nodes = decorateFileTree([], {}, "", new Set(),
      new Set(["../secret.txt", "/tmp/secret.txt", "docs//secret.txt", "docs/../secret.txt",
        "C:/outside.txt", "C:\\outside.txt"]));

    expect(nodes).toEqual([]);
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
    expect(src).toMatchObject({ id: "src", path: "src", children: [], expanded: true, gitStatus: "clean" });
    expect(readme).toMatchObject({ id: "README.md", path: "README.md", gitStatus: "added" });
    for (const status of ["modified", "added", "untracked", "deleted", "renamed"]) {
      expect(isDirtyStatus(status)).toBe(true);
    }
    expect(isDirtyStatus("clean")).toBe(false);
    expect(isDirtyStatus("unknown")).toBe(false);
    expect(isDirtyStatus(undefined)).toBe(false);
  });
});
