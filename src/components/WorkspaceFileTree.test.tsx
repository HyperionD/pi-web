import React, { act } from "react";
import { createRoot } from "react-dom/client";
import { renderToString } from "react-dom/server";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  createWorkspaceFile,
  deleteWorkspaceFile,
  downloadWorkspaceFile,
  getWorkspaceFileList,
  renameWorkspaceFile,
  searchWorkspaceFiles,
  uploadWorkspaceFile,
} from "../lib/api";
import WorkspaceFileTree from "./WorkspaceFileTree";

vi.mock("../lib/api", () => ({
  createWorkspaceFile: vi.fn().mockResolvedValue({}),
  deleteWorkspaceFile: vi.fn().mockResolvedValue({}),
  getWorkspaceFileList: vi.fn().mockResolvedValue({ entries: [] }),
  renameWorkspaceFile: vi.fn().mockResolvedValue({}),
  searchWorkspaceFiles: vi.fn().mockResolvedValue({ matches: [] }),
  uploadWorkspaceFile: vi.fn().mockResolvedValue({}),
  downloadWorkspaceFile: vi.fn().mockResolvedValue({ blob: new Blob(["content"]), filename: "download.txt" }),
}));

const renderedTrees = new Map<ReturnType<typeof createRoot>, HTMLElement>();

describe("WorkspaceFileTree", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.mocked(getWorkspaceFileList).mockReset().mockResolvedValue({ entries: [] });
    vi.mocked(searchWorkspaceFiles).mockReset().mockResolvedValue({ matches: [] });
    document.body.replaceChildren();
  });

  afterEach(async () => {
    for (const [root, host] of renderedTrees) await cleanup(root, host);
    vi.useRealTimers();
    vi.restoreAllMocks();
    vi.unstubAllGlobals();
  });

  it("renders on the server without reading document", () => {
    vi.stubGlobal("document", undefined);
    try {
      expect(renderToString(<WorkspaceFileTree />)).toContain("workspace-file-tree");
    } finally {
      vi.unstubAllGlobals();
    }
  });

  it("does not render the file operations hint", async () => {
    const { host, root } = await renderTree();

    expect(host.querySelector(".tree-tip")).toBeNull();
    expect(host.textContent).not.toContain("提示：右键点击");

    await cleanup(root, host);
  });

  it("renders files, status badges, expansion, and activation contract", async () => {
    const { host, root } = await renderTree();

    await act(async () => {
      root.render(<WorkspaceFileTree initialFiles={[]} />);
    });
    expect(host.textContent).toContain("文件树正在从后端加载");

    await updateTree();

    const folder = host.querySelector<HTMLButtonElement>(".tree-node.dir");
    expect(folder?.textContent).toContain("src");
    expect(folder?.querySelector("[aria-label='dirty']")?.textContent).toBe("•");
    expect(folder?.querySelector(".file-icon")?.getAttribute("data-icon-name")).toBe("folder-src");

    await act(async () => folder?.click());
    const file = [...host.querySelectorAll<HTMLButtonElement>(".tree-node.file")]
      .find((button) => button.dataset.filePath === "src/main.ts");
    expect(file?.dataset.action).toBe("open-file");
    expect(file?.querySelector(".file-icon")?.getAttribute("data-icon-name")).toBe("typescript");
    expect(file?.querySelector("[aria-label='modified']")?.textContent).toBe("M");

    const click = vi.fn();
    host.addEventListener("click", click);
    await act(async () => file?.click());
    expect(click).toHaveBeenCalled();

    await cleanup(root, host);
  });

  it("expands an empty .worktrees directory with visible feedback and can collapse it", async () => {
    const { host, root } = await renderTree();
    createActiveWorkspace();
    await setTreeFiles([{ type: "dir", name: ".worktrees", path: ".worktrees" }]);
    const folder = host.querySelector<HTMLElement>("[data-file-path='.worktrees']")!;

    await act(async () => folder.click());

    expect(folder.getAttribute("aria-expanded")).toBe("true");
    expect(folder.textContent).toContain("空文件夹");
    await act(async () => folder.click());
    expect(folder.getAttribute("aria-expanded")).toBe("false");
    expect(folder.textContent).not.toContain("空文件夹");
    await cleanup(root, host);
  });

  it("toggles a focused directory once for each Enter or Space key", async () => {
    const { host, root } = await renderTree();
    createActiveWorkspace();
    await setTreeFiles([{ type: "dir", name: ".worktrees", path: ".worktrees" }]);
    const folder = host.querySelector<HTMLElement>("[data-file-path='.worktrees']")!;
    await act(async () => folder.click());
    expect(folder.getAttribute("aria-expanded")).toBe("true");
    await act(async () => folder.dispatchEvent(new KeyboardEvent("keydown", {
      key: "Enter", bubbles: true, cancelable: true,
    })));
    expect(folder.getAttribute("aria-expanded")).toBe("false");

    await act(async () => folder.dispatchEvent(new KeyboardEvent("keydown", {
      key: " ", bubbles: true, cancelable: true,
    })));

    expect(folder.getAttribute("aria-expanded")).toBe("true");
    await cleanup(root, host);
  });

  it("shows loading feedback before an unloaded directory finishes loading", async () => {
    let resolveListing!: (listing: unknown) => void;
    vi.mocked(getWorkspaceFileList).mockReturnValueOnce(new Promise((resolve) => { resolveListing = resolve; }));
    const { host, root } = await renderTree();
    createActiveWorkspace();
    await setTreeFiles([{ type: "dir", name: "deep", path: "deep" }]);
    const folder = host.querySelector<HTMLElement>("[data-file-path='deep']")!;

    await act(async () => folder.click());

    expect(folder.getAttribute("aria-expanded")).toBe("true");
    expect(folder.getAttribute("aria-busy")).toBe("true");
    expect(folder.textContent).toContain("加载中");
    expect(folder.textContent).not.toContain("空文件夹");
    await act(async () => resolveListing({ entries: [{ type: "file", name: "child.txt", path: "deep/child.txt" }] }));
    expect(host.querySelector("[data-file-path='deep/child.txt']")).toBeTruthy();
    expect(folder.getAttribute("aria-busy")).not.toBe("true");
    await cleanup(root, host);
  });

  it.each([
    { rawPath: "./deep", path: "deep" },
    { rawPath: "deep\\nested", path: "deep/nested" },
  ])("loads directories using the displayed normalized path: $rawPath", async ({ rawPath, path }) => {
    vi.mocked(getWorkspaceFileList).mockResolvedValueOnce({ entries: [
      { type: "file", name: "child.txt", path: `${path}/child.txt` },
    ] });
    const { host, root } = await renderTree();
    createActiveWorkspace();
    await setTreeFiles([{ type: "dir", name: "deep", path: rawPath }]);

    await act(async () => host.querySelector<HTMLElement>(`[data-file-path='${path}']`)!.click());

    expect(host.querySelector(`[data-file-path='${path}/child.txt']`)).toBeTruthy();
    expect(getWorkspaceFileList).toHaveBeenCalledWith("workspace-1", path);
    await cleanup(root, host);
  });

  it("preserves lazy-loaded children and git statuses on selection-only updates", async () => {
    vi.mocked(getWorkspaceFileList).mockResolvedValueOnce({ entries: [
      { type: "file", name: "child.txt", path: "deep/child.txt" },
    ] });
    const { host, root } = await renderTree();
    createActiveWorkspace();
    await act(async () => window.dispatchEvent(new CustomEvent("pi-workspace-tree:update", { detail: {
      files: [{ type: "dir", name: "deep", path: "deep" }],
      statusMap: { "deep/child.txt": "modified" },
    } })));
    await act(async () => host.querySelector<HTMLElement>("[data-file-path='deep']")!.click());
    expect(host.querySelector("[data-file-path='deep/child.txt']")).toBeTruthy();

    await act(async () => window.dispatchEvent(new CustomEvent("pi-workspace-tree:update", { detail: {
      selectedPath: "deep/child.txt",
    } })));

    const child = host.querySelector<HTMLElement>("[data-file-path='deep/child.txt']");
    expect(child).toBeTruthy();
    expect(child?.getAttribute("aria-current")).toBe("true");
    expect(child?.querySelector("[aria-label='modified']")).toBeTruthy();
    expect(host.querySelector("[data-file-path='deep']")?.getAttribute("aria-expanded")).toBe("true");
    await cleanup(root, host);
  });

  it("filters the tree with the all-file search", async () => {
    const { host, root } = await renderTree();
    await updateTree();

    const search = host.querySelector<HTMLInputElement>(".tree-search input");
    expect(host.querySelector(".tree-search span")?.textContent).toBe("搜索");
    expect(search?.placeholder).toBe("搜索全部内容");
    await act(async () => {
      setInputValue(search!, "new");
      search!.dispatchEvent(new Event("input", { bubbles: true }));
    });

    expect(host.querySelector("[data-file-path='src/new.ts']")).toBeTruthy();
    expect(host.querySelector("[data-file-path='src/main.ts']")).toBeFalsy();

    await cleanup(root, host);
  });

  it("filters the tree with file contents from backend search", async () => {
    vi.useFakeTimers();
    vi.mocked(searchWorkspaceFiles).mockResolvedValueOnce({ matches: ["src/main.ts"] });
    const app = document.createElement("pi-app");
    app.dataset.activeWorkspaceId = "workspace-1";
    document.body.append(app);
    const { host, root } = await renderTree();
    await updateTree();

    const search = host.querySelector<HTMLInputElement>(".tree-search input");
    await act(async () => {
      setInputValue(search!, "answer");
      search!.dispatchEvent(new Event("input", { bubbles: true }));
    });
    await act(async () => {
      vi.advanceTimersByTime(250);
      await Promise.resolve();
    });

    expect(searchWorkspaceFiles).toHaveBeenCalledWith("workspace-1", "answer");
    expect(host.querySelector("[data-file-path='src/main.ts']")).toBeTruthy();
    expect(host.querySelector("[data-file-path='src/new.ts']")).toBeFalsy();

    await cleanup(root, host);
  });

  it("renders backend search hits beyond the loaded depth without caching partial search folders", async () => {
    vi.useFakeTimers();
    vi.mocked(searchWorkspaceFiles).mockResolvedValueOnce({ matches: ["src/deep/more/needle.txt"] });
    vi.mocked(getWorkspaceFileList).mockResolvedValueOnce({ entries: [
      { type: "file", name: "other.txt", path: "src/other.txt" },
    ] });
    const { host, root } = await renderTree();
    createActiveWorkspace();
    await setTreeFiles([{ type: "dir", name: "src", path: "src" }]);
    const search = host.querySelector<HTMLInputElement>(".tree-search input")!;
    await act(async () => {
      setInputValue(search, "needle");
      search.dispatchEvent(new Event("input", { bubbles: true }));
    });
    await act(async () => { await vi.advanceTimersByTimeAsync(250); });

    const hit = host.querySelector<HTMLElement>("[data-file-path='src/deep/more/needle.txt']");
    expect(hit?.dataset.action).toBe("open-file");
    expect(host.querySelector("[data-file-path='src/deep/more']")).toBeTruthy();

    await act(async () => {
      setInputValue(search, "");
      search.dispatchEvent(new Event("input", { bubbles: true }));
    });
    await act(async () => host.querySelector<HTMLElement>("[data-file-path='src']")!.click());
    expect(host.querySelector("[data-file-path='src/other.txt']")).toBeTruthy();
    expect(host.querySelector("[data-file-path='src/deep/more/needle.txt']")).toBeNull();
    await cleanup(root, host);
  });

  it("does not merge a stale directory response into a refreshed tree", async () => {
    let resolveOldListing!: (listing: unknown) => void;
    vi.mocked(getWorkspaceFileList).mockReturnValueOnce(new Promise((resolve) => { resolveOldListing = resolve; }));
    const { host, root } = await renderTree();
    createActiveWorkspace();
    await setTreeFiles([{ type: "dir", name: "deep", path: "deep" }]);
    await act(async () => host.querySelector<HTMLElement>("[data-file-path='deep']")!.click());
    await setTreeFiles([{ type: "dir", name: "deep", path: "deep" }]);
    expect(host.querySelector("[data-file-path='deep']")?.getAttribute("aria-expanded")).toBe("false");

    await act(async () => resolveOldListing({ entries: [
      { type: "file", name: "stale.txt", path: "deep/stale.txt" },
    ] }));

    expect(host.querySelector("[data-file-path='deep/stale.txt']")).toBeNull();
    vi.mocked(getWorkspaceFileList).mockResolvedValueOnce({ entries: [
      { type: "file", name: "current.txt", path: "deep/current.txt" },
    ] });
    await act(async () => host.querySelector<HTMLElement>("[data-file-path='deep']")!.click());
    expect(host.querySelector("[data-file-path='deep/current.txt']")).toBeTruthy();
    await cleanup(root, host);
  });

  it("clears old git statuses on a full tree update that omits statusMap", async () => {
    const { host, root } = await renderTree();
    createActiveWorkspace();
    await act(async () => window.dispatchEvent(new CustomEvent("pi-workspace-tree:update", { detail: {
      files: [{ type: "file", name: "file.txt", path: "file.txt" }], statusMap: { "file.txt": "modified" },
    } })));
    expect(host.querySelector("[aria-label='modified']")).toBeTruthy();

    await setTreeFiles([{ type: "file", name: "file.txt", path: "file.txt" }]);

    expect(host.querySelector("[aria-label='modified']")).toBeNull();
    await cleanup(root, host);
  });

  it("retries failed directory loads only when reopening the directory", async () => {
    vi.mocked(getWorkspaceFileList).mockRejectedValueOnce(new Error("directory permission denied"));
    const alert = vi.spyOn(window, "alert").mockImplementation(() => {});
    const { host, root } = await renderTree();
    createActiveWorkspace();
    await setTreeFiles([{ type: "dir", name: "deep", path: "deep" }]);
    const folder = host.querySelector<HTMLElement>("[data-file-path='deep']")!;
    await act(async () => folder.click());
    expect(alert).toHaveBeenCalledWith("directory permission denied");
    let resolveRetry!: (listing: unknown) => void;
    vi.mocked(getWorkspaceFileList).mockReturnValueOnce(new Promise((resolve) => { resolveRetry = resolve; }));

    await act(async () => folder.click());

    expect(folder.getAttribute("aria-expanded")).toBe("false");
    expect(folder.getAttribute("aria-busy")).not.toBe("true");
    await act(async () => folder.click());
    expect(folder.getAttribute("aria-busy")).toBe("true");
    await act(async () => resolveRetry({ entries: [{ type: "file", name: "child.txt", path: "deep/child.txt" }] }));
    expect(host.querySelector("[data-file-path='deep/child.txt']")).toBeTruthy();
    await cleanup(root, host);
  });

  it("resets expansion when switching between workspaces with the same directory paths", async () => {
    const { host, root } = await renderTree();
    const app = createActiveWorkspace();
    await setTreeFiles([{ type: "dir", name: "deep", path: "deep" }]);
    await act(async () => host.querySelector<HTMLElement>("[data-file-path='deep']")!.click());
    expect(host.querySelector("[data-file-path='deep']")?.getAttribute("aria-expanded")).toBe("true");
    app.dataset.activeWorkspaceId = "workspace-2";

    await setTreeFiles([{ type: "dir", name: "deep", path: "deep" }]);

    expect(host.querySelector("[data-file-path='deep']")?.getAttribute("aria-expanded")).toBe("false");
    await cleanup(root, host);
  });

  it("repeats the current search in a new workspace and ignores the old search response", async () => {
    vi.useFakeTimers();
    let resolveOldSearch!: (result: unknown) => void;
    vi.mocked(searchWorkspaceFiles).mockReturnValueOnce(new Promise((resolve) => { resolveOldSearch = resolve; }));
    const { host, root } = await renderTree();
    const app = createActiveWorkspace();
    await setTreeFiles([{ type: "file", name: "result.txt", path: "result.txt" }]);
    const search = host.querySelector<HTMLInputElement>(".tree-search input")!;
    await act(async () => {
      setInputValue(search, "secret");
      search.dispatchEvent(new Event("input", { bubbles: true }));
    });
    await act(async () => { await vi.advanceTimersByTimeAsync(250); });
    app.dataset.activeWorkspaceId = "workspace-2";
    await setTreeFiles([{ type: "file", name: "result.txt", path: "result.txt" }]);
    await act(async () => { await vi.advanceTimersByTimeAsync(250); });

    await act(async () => resolveOldSearch({ matches: ["result.txt"] }));

    expect(host.querySelector("[data-file-path='result.txt']")).toBeNull();
    expect(searchWorkspaceFiles).toHaveBeenLastCalledWith("workspace-2", "secret");
    await cleanup(root, host);
  });

  it("reports current search failures instead of silently clearing the results", async () => {
    vi.useFakeTimers();
    vi.mocked(searchWorkspaceFiles).mockRejectedValueOnce(new Error("search permission denied"));
    const alert = vi.spyOn(window, "alert").mockImplementation(() => {});
    const { host, root } = await renderTree();
    createActiveWorkspace();
    await setTreeFiles([{ type: "file", name: "result.txt", path: "result.txt" }]);
    const search = host.querySelector<HTMLInputElement>(".tree-search input")!;
    await act(async () => {
      setInputValue(search, "secret");
      search.dispatchEvent(new Event("input", { bubbles: true }));
    });
    await act(async () => { await vi.advanceTimersByTimeAsync(250); });

    expect(alert).toHaveBeenCalledWith("search permission denied");
    await cleanup(root, host);
  });

  it("creates files in the context menu target folder", async () => {
    const { host, root } = await renderTree();
    await updateTree();
    const app = document.createElement("pi-app");
    app.dataset.activeWorkspaceId = "workspace-1";
    document.body.append(app);
    vi.spyOn(window, "prompt").mockReturnValue("src/created.ts");
    const refresh = vi.fn();
    window.addEventListener("pi-workspace-tree:refresh", refresh);

    const folder = host.querySelector<HTMLButtonElement>(".tree-node.dir");
    await act(async () => {
      folder?.dispatchEvent(new MouseEvent("contextmenu", { bubbles: true, cancelable: true, clientX: 10, clientY: 20 }));
    });
    await act(async () => {
      [...host.querySelectorAll<HTMLButtonElement>(".tree-action-menu button")]
        .find((button) => button.textContent === "新建文件")?.click();
    });

    expect(createWorkspaceFile).toHaveBeenCalledWith("workspace-1", "src/created.ts", "file");
    expect(refresh).toHaveBeenCalled();
    await cleanup(root, host);
  });

  it("offers download only for file rows", async () => {
    const { host, root } = await renderTree();
    await updateTree();
    const app = document.createElement("pi-app");
    app.dataset.activeWorkspaceId = "workspace-1";
    document.body.append(app);

    const folder = host.querySelector<HTMLButtonElement>(".tree-node.dir");
    await act(async () => folder?.click());
    const file = [...host.querySelectorAll<HTMLButtonElement>(".tree-node.file")]
      .find((button) => button.dataset.filePath === "src/main.ts");
    await act(async () => {
      file?.dispatchEvent(new MouseEvent("contextmenu", { bubbles: true, cancelable: true, clientX: 10, clientY: 20 }));
    });
    expect([...host.querySelectorAll(".tree-action-menu button")].some((button) => button.textContent === "下载")).toBe(true);

    await act(async () => {
      folder?.dispatchEvent(new MouseEvent("contextmenu", { bubbles: true, cancelable: true, clientX: 10, clientY: 20 }));
    });
    expect([...host.querySelectorAll(".tree-action-menu button")].some((button) => button.textContent === "下载")).toBe(false);
    await cleanup(root, host);
  });

  it("shows known upload progress with the filename", async () => {
    let resolveUpload!: (value: unknown) => void;
    vi.mocked(uploadWorkspaceFile).mockImplementationOnce((_workspace, _path, file, _overwrite, onProgress) => {
      expect(file).toBeInstanceOf(File);
      onProgress?.(50, 100);
      return new Promise((resolve) => { resolveUpload = resolve; });
    });
    const { host, root } = await renderTree();
    await updateTree();
    const app = document.createElement("pi-app");
    app.dataset.activeWorkspaceId = "workspace-1";
    document.body.append(app);
    const folder = host.querySelector<HTMLButtonElement>(".tree-node.dir");
    await act(async () => folder?.dispatchEvent(new MouseEvent("contextmenu", { bubbles: true, cancelable: true })));
    await act(async () => {
      [...host.querySelectorAll<HTMLButtonElement>(".tree-action-menu button")]
        .find((button) => button.textContent === "上传到此处")?.click();
    });
    const input = host.querySelector<HTMLInputElement>("input[type=file]")!;
    const file = new File(["hello"], "progress.txt", { type: "text/plain" });
    Object.defineProperty(input, "files", { configurable: true, value: [file] });
    await act(async () => input.dispatchEvent(new Event("change", { bubbles: true })));

    const status = host.querySelector<HTMLElement>("[role=status]");
    expect(status?.textContent).toContain("progress.txt");
    expect(status?.textContent).toContain("50%");
    await act(async () => folder?.dispatchEvent(new MouseEvent("contextmenu", { bubbles: true, cancelable: true })));
    const uploadAction = [...host.querySelectorAll<HTMLButtonElement>(".tree-action-menu button")]
      .find((button) => button.textContent === "上传到此处");
    expect(uploadAction?.disabled).toBe(true);
    resolveUpload({ uploaded: true });
    await cleanup(root, host);
  });

  it("does not continue an upload after unmount", async () => {
    vi.useFakeTimers();
    let resolveUpload!: (value: unknown) => void;
    vi.mocked(uploadWorkspaceFile).mockImplementationOnce(() => new Promise((resolve) => {
      resolveUpload = resolve;
    }));
    const refresh = vi.fn();
    window.addEventListener("pi-workspace-tree:refresh", refresh);
    const { host, root } = await renderTree();
    await updateTree();
    const app = document.createElement("pi-app");
    app.dataset.activeWorkspaceId = "workspace-1";
    document.body.append(app);
    const folder = host.querySelector<HTMLButtonElement>(".tree-node.dir");
    await act(async () => folder?.dispatchEvent(new MouseEvent("contextmenu", { bubbles: true, cancelable: true })));
    await act(async () => {
      [...host.querySelectorAll<HTMLButtonElement>(".tree-action-menu button")]
        .find((button) => button.textContent === "上传到此处")?.click();
    });
    const input = host.querySelector<HTMLInputElement>("input[type=file]")!;
    Object.defineProperty(input, "files", { configurable: true, value: [new File(["x"], "pending.txt")] });
    await act(async () => input.dispatchEvent(new Event("change", { bubbles: true })));
    const setTimeoutSpy = vi.spyOn(window, "setTimeout");
    setTimeoutSpy.mockClear();

    await cleanup(root, host);
    setTimeoutSpy.mockClear();
    await act(async () => {
      resolveUpload({ uploaded: true });
      await Promise.resolve();
    });
    window.removeEventListener("pi-workspace-tree:refresh", refresh);

    expect(setTimeoutSpy).not.toHaveBeenCalled();
    expect(refresh).not.toHaveBeenCalled();
  });

  it("does not report an upload rejection after unmount", async () => {
    vi.useFakeTimers();
    let rejectUpload!: (reason: unknown) => void;
    vi.mocked(uploadWorkspaceFile).mockImplementationOnce(() => new Promise((_resolve, reject) => {
      rejectUpload = reject;
    }));
    const refresh = vi.fn();
    const alert = vi.spyOn(window, "alert").mockImplementation(() => {});
    window.addEventListener("pi-workspace-tree:refresh", refresh);
    const { host, root } = await renderTree();
    await updateTree();
    const app = document.createElement("pi-app");
    app.dataset.activeWorkspaceId = "workspace-1";
    document.body.append(app);
    const folder = host.querySelector<HTMLButtonElement>(".tree-node.dir");
    await act(async () => folder?.dispatchEvent(new MouseEvent("contextmenu", { bubbles: true, cancelable: true })));
    await act(async () => {
      [...host.querySelectorAll<HTMLButtonElement>(".tree-action-menu button")]
        .find((button) => button.textContent === "上传到此处")?.click();
    });
    const input = host.querySelector<HTMLInputElement>("input[type=file]")!;
    Object.defineProperty(input, "files", { configurable: true, value: [new File(["x"], "pending.txt")] });
    await act(async () => input.dispatchEvent(new Event("change", { bubbles: true })));
    const setTimeoutSpy = vi.spyOn(window, "setTimeout");
    setTimeoutSpy.mockClear();

    await cleanup(root, host);
    setTimeoutSpy.mockClear();
    await act(async () => {
      rejectUpload(new Error("upload failed"));
      await Promise.resolve();
    });
    window.removeEventListener("pi-workspace-tree:refresh", refresh);

    expect(setTimeoutSpy).not.toHaveBeenCalled();
    expect(refresh).not.toHaveBeenCalled();
    expect(alert).not.toHaveBeenCalled();
  });

  it("does not continue a download after unmount", async () => {
    vi.useFakeTimers();
    let resolveDownload!: (value: unknown) => void;
    vi.mocked(downloadWorkspaceFile).mockImplementationOnce(() => new Promise((resolve) => {
      resolveDownload = resolve;
    }));
    const refresh = vi.fn();
    window.addEventListener("pi-workspace-tree:refresh", refresh);
    const { host, root } = await renderTree();
    await updateTree();
    const app = document.createElement("pi-app");
    app.dataset.activeWorkspaceId = "workspace-1";
    document.body.append(app);
    const folder = host.querySelector<HTMLButtonElement>(".tree-node.dir");
    await act(async () => folder?.click());
    const file = [...host.querySelectorAll<HTMLButtonElement>(".tree-node.file")]
      .find((button) => button.dataset.filePath === "src/main.ts");
    await act(async () => file?.dispatchEvent(new MouseEvent("contextmenu", { bubbles: true, cancelable: true })));
    await act(async () => {
      [...host.querySelectorAll<HTMLButtonElement>(".tree-action-menu button")]
        .find((button) => button.textContent === "下载")?.click();
    });
    const setTimeoutSpy = vi.spyOn(window, "setTimeout");
    setTimeoutSpy.mockClear();

    await cleanup(root, host);
    setTimeoutSpy.mockClear();
    await act(async () => {
      resolveDownload({ blob: new Blob(["content"]), filename: "download.txt" });
      await Promise.resolve();
    });
    window.removeEventListener("pi-workspace-tree:refresh", refresh);

    expect(setTimeoutSpy).not.toHaveBeenCalled();
    expect(refresh).not.toHaveBeenCalled();
  });

  it("shows download progress with the filename and disables download actions during transfer", async () => {
    let resolveDownload!: (value: unknown) => void;
    vi.mocked(downloadWorkspaceFile).mockImplementationOnce((_workspace, _path, onProgress) => {
      onProgress?.(50, 100);
      return new Promise((resolve) => { resolveDownload = resolve; });
    });
    const { host, root } = await renderTree();
    await updateTree();
    const app = document.createElement("pi-app");
    app.dataset.activeWorkspaceId = "workspace-1";
    document.body.append(app);
    const folder = host.querySelector<HTMLButtonElement>(".tree-node.dir");
    await act(async () => folder?.click());
    const file = [...host.querySelectorAll<HTMLButtonElement>(".tree-node.file")]
      .find((button) => button.dataset.filePath === "src/main.ts");
    await act(async () => file?.dispatchEvent(new MouseEvent("contextmenu", { bubbles: true, cancelable: true })));
    await act(async () => {
      [...host.querySelectorAll<HTMLButtonElement>(".tree-action-menu button")]
        .find((button) => button.textContent === "下载")?.click();
    });

    const status = host.querySelector<HTMLElement>("[role=status]");
    expect(status?.textContent).toContain("main.ts");
    expect(status?.textContent).toContain("50%");
    expect(status?.getAttribute("aria-live")).toBe("polite");

    await act(async () => file?.dispatchEvent(new MouseEvent("contextmenu", { bubbles: true, cancelable: true })));
    const downloadAction = [...host.querySelectorAll<HTMLButtonElement>(".tree-action-menu button")]
      .find((button) => button.textContent === "下载");
    expect(downloadAction?.disabled).toBe(true);

    resolveDownload({ blob: new Blob(["content"]), filename: "main.ts" });
    await cleanup(root, host);
  });

  it("uploads multiple files sequentially", async () => {
    let resolveFirst!: (value: unknown) => void;
    const uploadedFiles: File[] = [];
    vi.mocked(uploadWorkspaceFile).mockImplementation((_workspace, _path, file) => {
      uploadedFiles.push(file);
      if (uploadedFiles.length === 1) return new Promise((resolve) => { resolveFirst = resolve; });
      return Promise.resolve({ uploaded: true });
    });
    const { host, root } = await renderTree();
    await updateTree();
    const app = document.createElement("pi-app");
    app.dataset.activeWorkspaceId = "workspace-1";
    document.body.append(app);
    const folder = host.querySelector<HTMLButtonElement>(".tree-node.dir");
    await act(async () => folder?.dispatchEvent(new MouseEvent("contextmenu", { bubbles: true, cancelable: true })));
    await act(async () => {
      [...host.querySelectorAll<HTMLButtonElement>(".tree-action-menu button")]
        .find((button) => button.textContent === "上传到此处")?.click();
    });
    const input = host.querySelector<HTMLInputElement>("input[type=file]")!;
    const first = new File(["one"], "one.txt");
    const second = new File(["two"], "two.txt");
    Object.defineProperty(input, "files", { configurable: true, value: [first, second] });
    await act(async () => input.dispatchEvent(new Event("change", { bubbles: true })));
    expect(uploadedFiles).toEqual([first]);

    resolveFirst({ uploaded: true });
    await act(async () => { await Promise.resolve(); });
    expect(uploadedFiles).toEqual([first, second]);
    await cleanup(root, host);
  });

  it("retries an existing upload with overwrite after confirmation", async () => {
    vi.mocked(uploadWorkspaceFile)
      .mockRejectedValueOnce(new Error("already exists"))
      .mockResolvedValueOnce({ uploaded: true });
    vi.spyOn(window, "confirm").mockReturnValue(true);
    const { host, root } = await renderTree();
    await updateTree();
    const app = document.createElement("pi-app");
    app.dataset.activeWorkspaceId = "workspace-1";
    document.body.append(app);
    const folder = host.querySelector<HTMLButtonElement>(".tree-node.dir");
    await act(async () => folder?.dispatchEvent(new MouseEvent("contextmenu", { bubbles: true, cancelable: true })));
    await act(async () => {
      [...host.querySelectorAll<HTMLButtonElement>(".tree-action-menu button")]
        .find((button) => button.textContent === "上传到此处")?.click();
    });
    const input = host.querySelector<HTMLInputElement>("input[type=file]")!;
    const file = new File(["replace"], "existing.txt");
    Object.defineProperty(input, "files", { configurable: true, value: [file] });
    await act(async () => input.dispatchEvent(new Event("change", { bubbles: true })));

    expect(uploadWorkspaceFile).toHaveBeenNthCalledWith(
      1, "workspace-1", "src/existing.txt", file, false, expect.any(Function),
    );
    expect(uploadWorkspaceFile).toHaveBeenNthCalledWith(
      2, "workspace-1", "src/existing.txt", file, true, expect.any(Function),
    );
    await cleanup(root, host);
  });

  it("returns a success status to idle after a short display", async () => {
    vi.useFakeTimers();
    const { host, root } = await renderTree();
    await updateTree();
    const app = document.createElement("pi-app");
    app.dataset.activeWorkspaceId = "workspace-1";
    document.body.append(app);
    const folder = host.querySelector<HTMLButtonElement>(".tree-node.dir");
    await act(async () => folder?.dispatchEvent(new MouseEvent("contextmenu", { bubbles: true, cancelable: true })));
    await act(async () => {
      [...host.querySelectorAll<HTMLButtonElement>(".tree-action-menu button")]
        .find((button) => button.textContent === "上传到此处")?.click();
    });
    const input = host.querySelector<HTMLInputElement>("input[type=file]")!;
    Object.defineProperty(input, "files", { configurable: true, value: [new File(["x"], "done.txt")] });
    await act(async () => input.dispatchEvent(new Event("change", { bubbles: true })));
    expect(host.querySelector("[data-status='success']")).toBeTruthy();

    await act(async () => { vi.advanceTimersByTime(999); });
    expect(host.querySelector("[data-status='success']")).toBeTruthy();
    await act(async () => { vi.advanceTimersByTime(1); });
    expect(host.querySelector("[data-status='success']")).toBeFalsy();

    await cleanup(root, host);
  });

  it("clears a pending success timer when the component unmounts", async () => {
    vi.useFakeTimers();
    const clearTimeout = vi.spyOn(window, "clearTimeout");
    const { host, root } = await renderTree();
    await updateTree();
    const app = document.createElement("pi-app");
    app.dataset.activeWorkspaceId = "workspace-1";
    document.body.append(app);
    const folder = host.querySelector<HTMLButtonElement>(".tree-node.dir");
    await act(async () => folder?.dispatchEvent(new MouseEvent("contextmenu", { bubbles: true, cancelable: true })));
    await act(async () => {
      [...host.querySelectorAll<HTMLButtonElement>(".tree-action-menu button")]
        .find((button) => button.textContent === "上传到此处")?.click();
    });
    const input = host.querySelector<HTMLInputElement>("input[type=file]")!;
    Object.defineProperty(input, "files", { configurable: true, value: [new File(["x"], "pending.txt")] });
    await act(async () => input.dispatchEvent(new Event("change", { bubbles: true })));

    await cleanup(root, host);
    expect(clearTimeout).toHaveBeenCalled();
  });

  it("clears a previous success timer before a new transfer", async () => {
    vi.useFakeTimers();
    let resolveSecond!: (value: unknown) => void;
    const { host, root } = await renderTree();
    await updateTree();
    const app = document.createElement("pi-app");
    app.dataset.activeWorkspaceId = "workspace-1";
    document.body.append(app);
    const folder = host.querySelector<HTMLButtonElement>(".tree-node.dir");
    const openUpload = async (file: File) => {
      await act(async () => folder?.dispatchEvent(new MouseEvent("contextmenu", { bubbles: true, cancelable: true })));
      await act(async () => {
        [...host.querySelectorAll<HTMLButtonElement>(".tree-action-menu button")]
          .find((button) => button.textContent === "上传到此处")?.click();
      });
      const input = host.querySelector<HTMLInputElement>("input[type=file]")!;
      Object.defineProperty(input, "files", { configurable: true, value: [file] });
      await act(async () => input.dispatchEvent(new Event("change", { bubbles: true })));
    };
    await openUpload(new File(["first"], "first.txt"));
    expect(host.querySelector("[data-status='success']")).toBeTruthy();
    vi.mocked(uploadWorkspaceFile).mockImplementationOnce(() => (
      new Promise((resolve) => { resolveSecond = resolve; })
    ));
    await openUpload(new File(["second"], "second.txt"));

    await act(async () => { vi.advanceTimersByTime(1000); });
    expect(host.querySelector("[data-status='progress']")?.textContent).toContain("second.txt");
    resolveSecond({ uploaded: true });
    await cleanup(root, host);
  });

  it("revokes a download object URL when anchor click throws", async () => {
    const createObjectURL = vi.fn().mockReturnValue("blob:throw");
    const revokeObjectURL = vi.fn();
    vi.stubGlobal("URL", { ...URL, createObjectURL, revokeObjectURL });
    vi.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(() => {
      throw new Error("click failed");
    });
    vi.spyOn(window, "alert").mockImplementation(() => {});
    const { host, root } = await renderTree();
    await updateTree();
    const app = document.createElement("pi-app");
    app.dataset.activeWorkspaceId = "workspace-1";
    document.body.append(app);
    const folder = host.querySelector<HTMLButtonElement>(".tree-node.dir");
    await act(async () => folder?.click());
    const file = [...host.querySelectorAll<HTMLButtonElement>(".tree-node.file")]
      .find((button) => button.dataset.filePath === "src/main.ts");
    await act(async () => file?.dispatchEvent(new MouseEvent("contextmenu", { bubbles: true, cancelable: true })));
    await act(async () => {
      [...host.querySelectorAll<HTMLButtonElement>(".tree-action-menu button")]
        .find((button) => button.textContent === "下载")?.click();
    });

    expect(host.querySelector("[data-status='error']")?.textContent).toContain("click failed");
    expect(revokeObjectURL).toHaveBeenCalledWith("blob:throw");
    expect(document.body.querySelector("a[hidden]")).toBeNull();
    await cleanup(root, host);
  });

  it("downloads a blob, clicks its filename, and revokes the object URL", async () => {
    const createObjectURL = vi.fn().mockReturnValue("blob:test");
    const revokeObjectURL = vi.fn();
    vi.stubGlobal("URL", { ...URL, createObjectURL, revokeObjectURL });
    let downloadedName = "";
    const click = vi.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(function () {
      downloadedName = this.download;
    });
    const { host, root } = await renderTree();
    await updateTree();
    const app = document.createElement("pi-app");
    app.dataset.activeWorkspaceId = "workspace-1";
    document.body.append(app);
    const folder = host.querySelector<HTMLButtonElement>(".tree-node.dir");
    await act(async () => folder?.click());
    const file = [...host.querySelectorAll<HTMLButtonElement>(".tree-node.file")]
      .find((button) => button.dataset.filePath === "src/main.ts");
    await act(async () => file?.dispatchEvent(new MouseEvent("contextmenu", { bubbles: true, cancelable: true })));
    await act(async () => {
      [...host.querySelectorAll<HTMLButtonElement>(".tree-action-menu button")]
        .find((button) => button.textContent === "下载")?.click();
    });

    expect(downloadWorkspaceFile).toHaveBeenCalledWith("workspace-1", "src/main.ts", expect.any(Function));
    expect(createObjectURL).toHaveBeenCalledWith(expect.any(Blob));
    expect(click).toHaveBeenCalled();
    expect(downloadedName).toBe("download.txt");
    expect(revokeObjectURL).toHaveBeenCalledWith("blob:test");
    await cleanup(root, host);
  });

  it("passes the selected File to multipart upload", async () => {
    const { host, root } = await renderTree();
    await updateTree();
    const app = document.createElement("pi-app");
    app.dataset.activeWorkspaceId = "workspace-1";
    document.body.append(app);
    const folder = host.querySelector<HTMLButtonElement>(".tree-node.dir");
    await act(async () => folder?.dispatchEvent(new MouseEvent("contextmenu", { bubbles: true, cancelable: true })));
    await act(async () => {
      [...host.querySelectorAll<HTMLButtonElement>(".tree-action-menu button")]
        .find((button) => button.textContent === "上传到此处")?.click();
    });
    const input = host.querySelector<HTMLInputElement>("input[type=file]")!;
    const file = new File(["binary"], "actual.bin");
    Object.defineProperty(input, "files", { configurable: true, value: [file] });
    await act(async () => input.dispatchEvent(new Event("change", { bubbles: true })));

    expect(uploadWorkspaceFile).toHaveBeenCalledWith("workspace-1", "src/actual.bin", file, false, expect.any(Function));
    expect(vi.mocked(uploadWorkspaceFile).mock.calls[0][2]).toBe(file);
    await cleanup(root, host);
  });

  it("reports upload errors and removes stale progress details", async () => {
    vi.mocked(uploadWorkspaceFile).mockImplementationOnce(async (_workspace, _path, _file, _overwrite, onProgress) => {
      onProgress?.(50, 100);
      throw new Error("上传失败：磁盘已满");
    });
    vi.spyOn(window, "alert").mockImplementation(() => {});
    const { host, root } = await renderTree();
    await updateTree();
    const app = document.createElement("pi-app");
    app.dataset.activeWorkspaceId = "workspace-1";
    document.body.append(app);
    const folder = host.querySelector<HTMLButtonElement>(".tree-node.dir");
    await act(async () => folder?.dispatchEvent(new MouseEvent("contextmenu", { bubbles: true, cancelable: true })));
    await act(async () => {
      [...host.querySelectorAll<HTMLButtonElement>(".tree-action-menu button")]
        .find((button) => button.textContent === "上传到此处")?.click();
    });
    const input = host.querySelector<HTMLInputElement>("input[type=file]")!;
    Object.defineProperty(input, "files", { configurable: true, value: [new File(["x"], "failed.txt")] });
    await act(async () => input.dispatchEvent(new Event("change", { bubbles: true })));

    const status = host.querySelector<HTMLElement>("[role=status]");
    expect(status?.textContent).toContain("上传失败：磁盘已满");
    expect(status?.textContent).not.toContain("50%");
    await cleanup(root, host);
  });

  it("reports download errors and clears download progress", async () => {
    vi.mocked(downloadWorkspaceFile).mockImplementationOnce(async (_workspace, _path, onProgress) => {
      onProgress?.(50, 100);
      throw new Error("下载失败：文件不可用");
    });
    vi.spyOn(window, "alert").mockImplementation(() => {});
    const { host, root } = await renderTree();
    await updateTree();
    const app = document.createElement("pi-app");
    app.dataset.activeWorkspaceId = "workspace-1";
    document.body.append(app);
    const folder = host.querySelector<HTMLButtonElement>(".tree-node.dir");
    await act(async () => folder?.click());
    const file = [...host.querySelectorAll<HTMLButtonElement>(".tree-node.file")]
      .find((button) => button.dataset.filePath === "src/main.ts");
    await act(async () => file?.dispatchEvent(new MouseEvent("contextmenu", { bubbles: true, cancelable: true })));
    await act(async () => {
      [...host.querySelectorAll<HTMLButtonElement>(".tree-action-menu button")]
        .find((button) => button.textContent === "下载")?.click();
    });

    const status = host.querySelector<HTMLElement>("[role=status]");
    expect(status?.textContent).toContain("下载失败：文件不可用");
    expect(status?.textContent).not.toContain("50%");
    await cleanup(root, host);
  });

  it("maps upload status 413 to workspace guidance", async () => {
    const error = Object.assign(new Error("413 Payload Too Large"), { status: 413 });
    vi.mocked(uploadWorkspaceFile).mockRejectedValueOnce(error);
    vi.spyOn(window, "alert").mockImplementation(() => {});
    const { host, root } = await renderTree();
    await updateTree();
    const app = document.createElement("pi-app");
    app.dataset.activeWorkspaceId = "workspace-1";
    document.body.append(app);
    const folder = host.querySelector<HTMLButtonElement>(".tree-node.dir");
    await act(async () => folder?.dispatchEvent(new MouseEvent("contextmenu", { bubbles: true, cancelable: true })));
    await act(async () => {
      [...host.querySelectorAll<HTMLButtonElement>(".tree-action-menu button")]
        .find((button) => button.textContent === "上传到此处")?.click();
    });
    const input = host.querySelector<HTMLInputElement>("input[type=file]")!;
    Object.defineProperty(input, "files", { configurable: true, value: [new File(["x"], "large.bin")] });
    await act(async () => input.dispatchEvent(new Event("change", { bubbles: true })));

    expect(host.querySelector("[data-status='error']")?.textContent).toContain("文件超过上传上限，请先上传到工作区");
    await cleanup(root, host);
  });

  it("maps upload status 501 to a clear platform guidance", async () => {
    const error = Object.assign(new Error("501 Not Implemented"), { status: 501 });
    vi.mocked(uploadWorkspaceFile).mockRejectedValueOnce(error);
    vi.spyOn(window, "alert").mockImplementation(() => {});
    const { host, root } = await renderTree();
    await updateTree();
    const app = document.createElement("pi-app");
    app.dataset.activeWorkspaceId = "workspace-1";
    document.body.append(app);
    const folder = host.querySelector<HTMLButtonElement>(".tree-node.dir");
    await act(async () => folder?.dispatchEvent(new MouseEvent("contextmenu", { bubbles: true, cancelable: true })));
    await act(async () => {
      [...host.querySelectorAll<HTMLButtonElement>(".tree-action-menu button")]
        .find((button) => button.textContent === "上传到此处")?.click();
    });
    const input = host.querySelector<HTMLInputElement>("input[type=file]")!;
    Object.defineProperty(input, "files", { configurable: true, value: [new File(["x"], "unsupported.bin")] });
    await act(async () => input.dispatchEvent(new Event("change", { bubbles: true })));

    expect(host.querySelector("[data-status='error']")?.textContent).toContain("当前平台不支持安全文件上传");
    await cleanup(root, host);
  });

  it("renames and deletes selected file targets from the action menu", async () => {
    const { host, root } = await renderTree();
    await updateTree();
    const app = document.createElement("pi-app");
    app.dataset.activeWorkspaceId = "workspace-1";
    document.body.append(app);
    vi.spyOn(window, "prompt").mockReturnValue("src/renamed.ts");
    vi.spyOn(window, "confirm").mockReturnValue(true);

    const folder = host.querySelector<HTMLButtonElement>(".tree-node.dir");
    await act(async () => folder?.click());
    const file = [...host.querySelectorAll<HTMLButtonElement>(".tree-node.file")]
      .find((button) => button.dataset.filePath === "src/main.ts");
    await act(async () => {
      file?.dispatchEvent(new MouseEvent("contextmenu", { bubbles: true, cancelable: true, clientX: 10, clientY: 20 }));
    });
    await act(async () => {
      [...host.querySelectorAll<HTMLButtonElement>(".tree-action-menu button")]
        .find((button) => button.textContent === "重命名")?.click();
    });
    expect(renameWorkspaceFile).toHaveBeenCalledWith("workspace-1", "src/main.ts", "src/renamed.ts");

    await act(async () => {
      file?.dispatchEvent(new MouseEvent("contextmenu", { bubbles: true, cancelable: true, clientX: 10, clientY: 20 }));
    });
    await act(async () => {
      [...host.querySelectorAll<HTMLButtonElement>(".tree-action-menu button")]
        .find((button) => button.textContent === "删除")?.click();
    });
    expect(deleteWorkspaceFile).toHaveBeenCalledWith("workspace-1", "src/main.ts");
    await cleanup(root, host);
  });
});

async function renderTree() {
  const host = document.createElement("div");
  host.className = "tree-list";
  document.body.append(host);
  const root = createRoot(host);
  renderedTrees.set(root, host);
  await act(async () => {
    root.render(<WorkspaceFileTree initialFiles={[]} />);
  });
  return { host, root };
}

function createActiveWorkspace(id = "workspace-1") {
  const app = document.createElement("pi-app");
  app.dataset.activeWorkspaceId = id;
  document.body.append(app);
  return app;
}

async function setTreeFiles(files: Array<{ type: string; name: string; path: string }>) {
  await act(async () => window.dispatchEvent(new CustomEvent("pi-workspace-tree:update", { detail: { files } })));
}

async function updateTree() {
  await act(async () => {
    window.dispatchEvent(new CustomEvent("pi-workspace-tree:update", {
      detail: {
        statusMap: { "src/main.ts": "modified", "src/new.ts": "untracked" },
        files: [{
          type: "dir",
          name: "src",
          path: "src",
          children: [
            { type: "file", name: "main.ts", path: "src/main.ts" },
            { type: "file", name: "new.ts", path: "src/new.ts" },
          ],
        }],
      },
    }));
  });
}

function setInputValue(input: HTMLInputElement, value: string) {
  Object.getOwnPropertyDescriptor(window.HTMLInputElement.prototype, "value")?.set?.call(input, value);
}

async function cleanup(root, host) {
  renderedTrees.delete(root);
  await act(async () => {
    root.unmount();
  });
  host.remove();
}
