import React, { act } from "react";
import { createRoot } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  createWorkspaceFile,
  deleteWorkspaceFile,
  downloadWorkspaceFile,
  renameWorkspaceFile,
  searchWorkspaceFiles,
  uploadWorkspaceFile,
} from "../lib/api";
import WorkspaceFileTree from "./WorkspaceFileTree";

vi.mock("../lib/api", () => ({
  createWorkspaceFile: vi.fn().mockResolvedValue({}),
  deleteWorkspaceFile: vi.fn().mockResolvedValue({}),
  renameWorkspaceFile: vi.fn().mockResolvedValue({}),
  searchWorkspaceFiles: vi.fn().mockResolvedValue({ matches: [] }),
  uploadWorkspaceFile: vi.fn().mockResolvedValue({}),
  downloadWorkspaceFile: vi.fn().mockResolvedValue({ blob: new Blob(["content"]), filename: "download.txt" }),
}));

describe("WorkspaceFileTree", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    document.body.replaceChildren();
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.restoreAllMocks();
    vi.unstubAllGlobals();
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
  await act(async () => {
    root.render(<WorkspaceFileTree initialFiles={[]} />);
  });
  return { host, root };
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
  await act(async () => {
    root.unmount();
  });
  host.remove();
}
