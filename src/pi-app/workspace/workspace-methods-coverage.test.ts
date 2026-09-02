// @ts-nocheck
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanupPiAppFixture, connectPiApp, installPiAppFixture } from "../test-helper";

const pickerStyles = readFileSync(resolve(process.cwd(), "src/styles/parts/workspace-picker.css"), "utf8");
const responsiveStyles = readFileSync(resolve(process.cwd(), "src/styles/parts/responsive-themes.css"), "utf8");

function okJson(body) {
  return { ok: true, status: 200, statusText: "OK", json: async () => body };
}

function failJson(message = "boom") {
  return { ok: false, status: 500, statusText: "ERR", json: async () => ({ error: message }) };
}

describe("workspace folder/render/bootstrap coverage", () => {
  beforeEach(() => {
    installPiAppFixture();
    globalThis.PI_WEB_API_BASE = "http://backend.test";
  });
  afterEach(cleanupPiAppFixture);

  it("renders recent workspaces, sidebar groups, and folder rows", async () => {
    const app = await connectPiApp();
    const recent = document.createElement("div");
    const count = document.createElement("span");
    const path = app.querySelector("[data-folder-path]");
    const list = app.querySelector("[data-folder-list]");
    recent.dataset.recentWorkspaces = "";
    count.dataset.workspaceCount = "";
    app.append(recent, count);
    app.dataset.activeWorkspaceId = "w2";

    const workspaces = [
      { id: "w1", name: "one", path: "/one", sessionCount: 2, lastUsed: "1m", sessions: [] },
      { id: "w2", name: "two", path: "/two", sessionCount: 1, live: true, sessions: [
        { id: "s2", title: "run", lastUsed: "now", live: true },
      ] },
    ];
    app.renderWorkspaces(workspaces);
    app.renderWorkspaces(workspaces);
    app.renderFolderListing({ path: "/tmp", displayPath: "tmp", folders: [
      { name: "child", path: "/tmp/child", displayPath: "~/child" },
    ] });

    expect(count.textContent).toBe("2 个工作区");
    expect(recent.querySelectorAll(".recent-row")).toHaveLength(2);
    expect(recent.querySelector(".recent-row").getAttribute("aria-label")).toBe("打开 one");
    expect(recent.querySelector(".open-cta").textContent).toContain("打开");
    expect(recent.querySelector(".ws-stat").innerHTML).toContain("1m");
    expect(recent.querySelectorAll(".ws-stat")[1].innerHTML).toContain("live");
    expect(app.querySelector("[data-workspace-group='w2'] .sessions").hidden).toBe(false);
    expect(app.querySelector("[data-workspace-group='w2'] .ws-name .dot").classList.contains("live")).toBe(true);
    expect(path.textContent).toBe("tmp");
    expect(list.querySelector("[data-action='folder-select']").dataset.path).toBe("/tmp");
    expect(list.querySelector("[data-action='folder-select'][data-path='/tmp/child'] .folder-path").textContent).toBe("~/child");

    app.renderFolderListing({ path: "/empty", folders: [] });
    expect(list.querySelector(".folder-empty").textContent).toBe("暂无文件夹");
  });

  it("separates folder expansion from folder selection", async () => {
    const app = await connectPiApp();
    const list = app.querySelector("[data-folder-list]");
    app.apiConnected = true;
    globalThis.fetch = vi.fn(async (url) => String(url).includes("path=%2Froot%2Fsrc")
      ? okJson({ path: "/root/src", parent: "/root", folders: [{ name: "lib", path: "/root/src/lib", displayPath: "~/project/src/lib" }] })
      : okJson({ path: "/root", parent: "/", folders: [{ name: "src", path: "/root/src", displayPath: "~/project/src" }] }));

    app.renderFolderListing({
      path: "/root",
      displayPath: "~/project",
      folders: [{ name: "src", path: "/root/src", displayPath: "~/project/src" }],
    });

    const srcToggle = list.querySelector("[data-action='folder-toggle'][data-path='/root/src']");
    const srcLabel = list.querySelector("[data-action='folder-select'][data-path='/root/src']");
    expect(srcToggle.getAttribute("aria-expanded")).toBe("false");
    srcLabel.click();
    expect(app.currentFolder).toBe("/root/src");
    expect(app.querySelector("[data-folder-path]").textContent).toBe("~/project/src");
    expect(list.querySelector("[data-action='folder-select'][data-path='/root/src']").closest("[data-folder-tree-node]").getAttribute("aria-selected")).toBe("true");

    list.querySelector("[data-action='folder-toggle'][data-path='/root/src']").click();
    await vi.waitFor(() => expect(globalThis.fetch).toHaveBeenCalledWith(
      expect.stringContaining("path=%2Froot%2Fsrc&purpose=workspace"),
      expect.any(Object),
    ));
    await vi.waitFor(() => expect(list.querySelector("[data-path='/root/src/lib']")).not.toBeNull());
    expect(app.currentFolder).toBe("/root/src");
    expect(app.folderListingValid).toBe(true);
    expect(list.querySelectorAll("[aria-selected='true']")).toHaveLength(1);
    expect(list.querySelector("[data-action='folder-toggle'][data-path='/root/src']").getAttribute("aria-expanded")).toBe("true");
  });

  it("starts restricted folder selection at allowed roots without a synthetic home", async () => {
    const app = await connectPiApp();
    const list = app.querySelector("[data-folder-list]");
    const confirm = app.querySelector("[data-action='folder-open-current']");
    app.renderFolderListing({
      path: "/home/alice",
      displayPath: "~",
      rootListing: true,
      folders: [{ name: "projects", path: "/workspaces/projects", displayPath: "~/workspaces/projects" }],
    });

    expect(list.querySelector("[data-folder-tree-node][data-path='/home/alice']")).toBeNull();
    expect(list.querySelector("[data-folder-tree-node][data-path='/workspaces/projects']")).not.toBeNull();
    expect(app.folderListingValid).toBe(false);
    expect(confirm.disabled).toBe(true);
    expect(app.querySelector("[data-action='folder-up']").disabled).toBe(true);

    list.querySelector("[data-action='folder-select'][data-path='/workspaces/projects']").click();
    expect(app.currentFolder).toBe("/workspaces/projects");
    expect(app.folderListingValid).toBe(true);
    expect(confirm.disabled).toBe(false);
    expect(list.querySelectorAll("[aria-selected='true']")).toHaveLength(1);
  });

  it("loads folders and opens workspaces through backend responses", async () => {
    const app = await connectPiApp();
    app.apiConnected = true;
    app.renderFolderListing = vi.fn();
    app.renderWorkspaces = vi.fn();
    app.openWorkspace = vi.fn();
    app.setConnection = vi.fn();
    globalThis.fetch = vi.fn(async (url, options = {}) => {
      const value = String(url);
      if (value.includes("/system/folders")) {
        return okJson({ path: "/root", folders: [] });
      }
      if (value.endsWith("/workspaces/open")) return okJson({ id: "w1" });
      if (value.endsWith("/workspaces")) {
        return okJson({ workspaces: [{ id: "w1", path: "/root" }] });
      }
      return okJson({});
    });

    const browser = app.querySelector("[data-folder-browser]");
    browser.hidden = true;
    app.currentFolder = "";
    await app.browseFolder();
    await app.loadFolder("");
    await app.openWorkspacePath("/root");
    await app.openWorkspacePath("");

    expect(app.currentFolder).toBe("/root");
    expect(app.currentFolderParent).toBe("/root");
    expect(app.renderFolderListing).toHaveBeenCalledWith(expect.objectContaining({ path: "/root" }));
    expect(browser.hidden).toBe(false);
    expect(app.renderWorkspaces).toHaveBeenCalledWith([{ id: "w1", path: "/root" }]);
    expect(app.openWorkspace).toHaveBeenCalledWith("w1");
    expect(app.setConnection).not.toHaveBeenCalledWith("err");
  });

  it("ignores stale workspace opening after deferred workspace responses", async () => {
    const app = await connectPiApp();
    app.apiConnected = true;
    let resolveOpenResponse;
    let resolveWorkspacesResponse;
    const openResponse = new Promise((resolve) => { resolveOpenResponse = resolve; });
    const workspacesResponse = new Promise((resolve) => { resolveWorkspacesResponse = resolve; });
    globalThis.fetch = vi.fn((url) => {
      const value = String(url);
      if (value.endsWith("/workspaces/open")) return openResponse;
      if (value.endsWith("/workspaces")) return workspacesResponse;
      return Promise.resolve(okJson({}));
    });
    const renderFilteredWorkspaces = vi.spyOn(app, "renderFilteredWorkspaces");
    const openWorkspace = vi.spyOn(app, "openWorkspace");
    const activateWorkspaceForSession = vi.spyOn(app, "activateWorkspaceForSession");
    const route = vi.spyOn(app, "route");
    const openingToken = Symbol("folder-opening");
    app.folderOpeningToken = openingToken;

    const opening = app.openWorkspacePath("/stale", openingToken);
    resolveOpenResponse(okJson({ id: "old" }));
    await vi.waitFor(() => expect(
      globalThis.fetch.mock.calls.some(([url]) => String(url).endsWith("/api/workspaces")),
    ).toBe(true));

    app.folderOpeningToken = Symbol("new-folder-opening");
    resolveWorkspacesResponse(okJson({ workspaces: [{ id: "old", path: "/stale" }] }));
    await opening;

    expect(renderFilteredWorkspaces).not.toHaveBeenCalled();
    expect(openWorkspace).not.toHaveBeenCalled();
    expect(activateWorkspaceForSession).not.toHaveBeenCalled();
    expect(route).not.toHaveBeenCalledWith("workspace");
  });

  it("uses the restricted purpose for ordinary folder browsing", async () => {
    const app = await connectPiApp();
    app.apiConnected = true;
    globalThis.fetch = vi.fn(async () => okJson({ path: "/allowed", folders: [] }));

    await app.loadFolder("/allowed");

    expect(globalThis.fetch).toHaveBeenCalledWith(
      expect.stringContaining("purpose=workspace"),
      expect.any(Object),
    );
  });

  it("does not throw when opening the picker without an API connection", async () => {
    const app = await connectPiApp();
    app.setConnection = vi.fn();

    await expect(app.openFolderPopover()).resolves.toBeUndefined();

    expect(app.setConnection).toHaveBeenCalledWith("err");
    expect(app.querySelector("[data-folder-list] .folder-error").textContent).toBe("后端连接已断开");
    expect(app.querySelector("[data-action='folder-open-current']").disabled).toBe(true);
  });

  it("opens the folder picker popover, navigates folders, and selects the current folder", async () => {
    const app = await connectPiApp();
    const trigger = app.querySelector("[data-action='route-picker']");
    const popover = app.querySelector("[data-folder-popover]");
    app.apiConnected = true;
    globalThis.fetch = vi.fn(async (url) => String(url).includes("path=%2Fallowed%2Fchild")
      ? okJson({ path: "/allowed/child", parent: "/allowed", folders: [] })
      : okJson({ path: "/allowed", parent: "/", folders: [{ name: "child", path: "/allowed/child" }] }));

    await app.openFolderPopover();

    expect(popover.hidden).toBe(false);
    expect(app.folderBrowsePurpose).toBe("workspace");
    expect(globalThis.fetch).toHaveBeenCalledWith(expect.stringContaining("path=~&purpose=workspace"), expect.any(Object));
    expect(popover.querySelector("[data-folder-path]").textContent).toBe("/allowed");
    expect(popover.querySelector("[data-folder-loading]").hidden).toBe(true);
    expect(trigger.disabled).toBe(false);
    expect(popover.querySelector("[data-action='folder-open-current']").disabled).toBe(false);

    const childLabel = popover.querySelector("[data-action='folder-select'][data-path='/allowed/child']");
    childLabel.click();
    expect(app.currentFolder).toBe("/allowed/child");

    const confirm = popover.querySelector("[data-action='folder-open-current']");
    expect(app.folderListingValid).toBe(true);
    expect(app.folderLoadPending).toBe(false);
    expect(app.folderOpeningPending).toBeFalsy();
    expect(confirm.disabled).toBe(false);
    expect(app.folderBrowsePurpose).toBe("workspace");
    app.openWorkspacePath = vi.fn();
    confirm.click();
    expect(app.openWorkspacePath).toHaveBeenCalledWith("/allowed/child", expect.any(Symbol));
  });

  it("positions the folder popover inside the viewport and uses trigger bounds for keyboard events", async () => {
    const app = await connectPiApp();
    const trigger = app.querySelector("[data-action='route-picker']");
    const popover = app.querySelector("[data-folder-popover]");
    Object.defineProperty(window, "innerWidth", { configurable: true, value: 1024 });
    Object.defineProperty(window, "innerHeight", { configurable: true, value: 768 });
    Object.defineProperty(popover, "offsetWidth", { configurable: true, value: 300 });
    Object.defineProperty(popover, "offsetHeight", { configurable: true, value: 200 });
    trigger.getBoundingClientRect = () => ({ left: 40, top: 80, right: 120, bottom: 112, width: 80, height: 32 });

    app.positionFolderPopover(new MouseEvent("click", { clientX: 1000, clientY: 760 }), trigger);
    expect(popover.style.left).toBe("708px");
    expect(popover.style.top).toBe("552px");

    app.positionFolderPopover(new KeyboardEvent("keydown", { key: "Enter" }), trigger);
    expect(popover.style.left).toBe("40px");
    expect(popover.style.top).toBe("120px");

    app.positionFolderPopover({ clientX: 0, clientY: 200 }, trigger);
    expect(popover.style.left).toBe("40px");
    expect(popover.style.top).toBe("208px");

    app.positionFolderPopover(new MouseEvent("click"), trigger);
    expect(popover.style.left).toBe("40px");
    expect(popover.style.top).toBe("120px");

    app.positionFolderPopover({ clientX: Number.POSITIVE_INFINITY, clientY: Number.NaN }, trigger);
    expect(popover.style.left).toBe("40px");
    expect(popover.style.top).toBe("120px");

    Object.defineProperty(popover, "offsetWidth", { configurable: true, value: 0 });
    Object.defineProperty(popover, "offsetHeight", { configurable: true, value: 0 });
    Object.defineProperty(window, "innerWidth", { configurable: true, value: 500 });
    Object.defineProperty(window, "innerHeight", { configurable: true, value: 400 });
    app.positionFolderPopover({ clientX: 100, clientY: 100 }, trigger);
    expect(popover.style.left).toBe("64px");
    expect(popover.style.top).toBe("24px");
  });

  it("repositions after folder listing changes popover height", async () => {
    const app = await connectPiApp();
    const trigger = app.querySelector("[data-action='route-picker']");
    const popover = app.querySelector("[data-folder-popover]");
    app.apiConnected = true;
    Object.defineProperty(window, "innerWidth", { configurable: true, value: 1024 });
    Object.defineProperty(window, "innerHeight", { configurable: true, value: 768 });
    Object.defineProperty(popover, "offsetWidth", { configurable: true, value: 300 });
    let popoverHeight = 100;
    Object.defineProperty(popover, "offsetHeight", { configurable: true, get: () => popoverHeight });
    trigger.getBoundingClientRect = () => ({ left: 40, top: 80, right: 120, bottom: 112, width: 80, height: 32 });
    let resolveListing;
    globalThis.fetch = vi.fn(() => new Promise((resolve) => { resolveListing = resolve; }));

    const opening = app.openFolderPopover("workspace", new MouseEvent("click", { clientX: 100, clientY: 700 }));
    expect(popover.style.top).toBe("652px");
    popoverHeight = 600;
    resolveListing(okJson({ path: "/current", parent: "/", folders: [] }));
    await opening;

    expect(popover.style.top).toBe("152px");
  });

  it("toggles an open workspace popover without reloading or losing navigation", async () => {
    const app = await connectPiApp();
    const trigger = app.querySelector("[data-action='route-picker']");
    const popover = app.querySelector("[data-folder-popover]");
    app.apiConnected = true;
    globalThis.fetch = vi.fn(async () => okJson({ path: "/allowed", parent: "/", folders: [] }));

    await app.openFolderPopover();
    app.currentFolder = "/allowed/child";
    trigger.click();
    await Promise.resolve();

    expect(popover.hidden).toBe(true);
    expect(trigger.getAttribute("aria-expanded")).toBe("false");
    expect(app.currentFolder).toBe("/allowed/child");
    expect(globalThis.fetch).toHaveBeenCalledTimes(1);
  });

  it("renders a safe offline folder error and disables confirmation", async () => {
    const app = await connectPiApp();
    const list = app.querySelector("[data-folder-list]");
    const confirm = app.querySelector("[data-action='folder-open-current']");
    app.currentFolder = "/stale";
    app.apiConnected = false;
    app.setConnection = vi.fn();

    await app.loadFolder("~");

    expect(app.setConnection).toHaveBeenCalledWith("err");
    expect(list.querySelector(".folder-error").textContent).toBe("后端连接已断开");
    expect(confirm.disabled).toBe(true);
  });

  it("keeps folder loading feedback and ignores stale responses after closing", async () => {
    const app = await connectPiApp();
    const trigger = app.querySelector("[data-action='route-picker']");
    const popover = app.querySelector("[data-folder-popover]");
    const loading = app.querySelector("[data-folder-loading]");
    const up = app.querySelector("[data-action='folder-up']");
    const confirm = app.querySelector("[data-action='folder-open-current']");
    app.dataset.route = "workspace";
    app.apiConnected = true;
    let resolveListing;
    globalThis.fetch = vi.fn(() => new Promise((resolve) => { resolveListing = resolve; }));

    const opening = app.openFolderPopover();
    expect(popover.hidden).toBe(false);
    expect(loading.hidden).toBe(false);
    expect(trigger.disabled).toBe(true);
    expect(up.disabled).toBe(true);
    expect(confirm.disabled).toBe(true);

    app.closeFolderPopover();
    expect(popover.hidden).toBe(true);
    resolveListing(okJson({ path: "/stale", parent: "/", folders: [{ name: "stale", path: "/stale" }] }));
    await opening;

    expect(trigger.disabled).toBe(false);
    expect(app.currentFolder).toBe("~");
    expect(document.activeElement).toBe(trigger);
    expect(popover.querySelector("[data-folder-list] .folder-row")).toBeNull();
    expect(loading.hidden).toBe(true);
  });

  it("focuses the current folder popover after the latest request only", async () => {
    const app = await connectPiApp();
    const popover = app.querySelector("[data-folder-popover]");
    app.dataset.route = "workspace";
    app.apiConnected = true;
    const responses = [];
    globalThis.fetch = vi.fn(() => new Promise((resolve) => responses.push(resolve)));

    const first = app.openFolderPopover();
    app.closeFolderPopover();
    const second = app.openFolderPopover();
    await vi.waitFor(() => expect(responses).toHaveLength(2));
    responses[0](okJson({ path: "/stale", parent: "/", folders: [{ name: "stale", path: "/stale" }] }));
    await first;
    expect(app.currentFolder).toBe("~");
    expect(document.activeElement).not.toBe(popover.querySelector("[data-folder-list] .folder-row"));

    responses[1](okJson({ path: "/current", parent: "/", folders: [{ name: "current", path: "/current" }] }));
    await second;
    expect(app.currentFolder).toBe("/current");
    expect(document.activeElement).toBe(popover.querySelector("[data-folder-list] .folder-row"));
  });

  it("does not refocus a popover when opening it again during loading", async () => {
    const app = await connectPiApp();
    const popover = app.querySelector("[data-folder-popover]");
    const confirm = app.querySelector("[data-action='folder-open-current']");
    app.dataset.route = "workspace";
    app.apiConnected = true;
    let resolveListing;
    globalThis.fetch = vi.fn(() => new Promise((resolve) => { resolveListing = resolve; }));

    const first = app.openFolderPopover();
    await app.openFolderPopover();

    expect(globalThis.fetch).toHaveBeenCalledTimes(1);
    expect(document.activeElement).not.toBe(confirm);
    resolveListing(okJson({ path: "/current", parent: "/", folders: [] }));
    await first;
    expect(popover.hidden).toBe(false);

    expect(document.activeElement).toBe(popover.querySelector("[data-action='folder-select']"));
  });

  it("renders server-controlled folder values as text without HTML nodes", async () => {
    const app = await connectPiApp();
    const path = app.querySelector("[data-folder-path]");
    const list = app.querySelector("[data-folder-list]");

    const maliciousPath = '"><img src=x onerror="alert(1)">/allowed';
    const maliciousName = '<img src=x onerror="alert(1)">child';
    const maliciousError = '<svg onload="alert(1)">forbidden</svg>';
    app.renderFolderListing({
      path: maliciousPath,
      displayPath: maliciousPath,
      folders: [{ name: maliciousName, path: "/allowed/child", displayPath: maliciousPath }],
    });

    expect(path.textContent).toBe(maliciousPath);
    expect(path.querySelector("*")).toBeNull();
    const row = list.querySelector("[data-action='folder-select'][data-path='/allowed/child']");
    expect(row.querySelector(".folder-name").textContent).toBe(maliciousName);
    expect(row.querySelector(".folder-name").querySelector("*")).toBeNull();
    expect(row.querySelector(".folder-path").textContent).toBe(maliciousPath);
    expect(row.querySelector(".folder-path").querySelector("*")).toBeNull();

    app.renderFolderError(new Error(maliciousError));
    const error = list.querySelector(".folder-error");
    expect(error.textContent).toBe(maliciousError);
    expect(error.querySelector("*")).toBeNull();
  });

  it("uses the allowed-root purpose for configuration root browsing", async () => {
    const app = await connectPiApp();
    app.apiConnected = true;
    globalThis.fetch = vi.fn(async () => okJson({ path: "/allowed", folders: [] }));

    await app.loadFolder("/allowed", "allowed-root");

    expect(globalThis.fetch).toHaveBeenCalledWith(
      expect.stringContaining("purpose=allowed-root"),
      expect.any(Object),
    );
  });

  it("shows the backend error when folder browsing is forbidden", async () => {
    const app = await connectPiApp();
    app.apiConnected = true;
    const list = app.querySelector("[data-folder-list]");
    globalThis.fetch = vi.fn(async () => ({
      ok: false,
      status: 403,
      statusText: "Forbidden",
      json: async () => ({ error: "outside allowed roots" }),
    }));

    await app.loadFolder("/forbidden");

    const error = list.querySelector(".folder-error");
    expect(error.textContent).toBe("outside allowed roots");
  });

  it("uses a fixed viewport popover without legacy full-screen picker styles", () => {
    expect(pickerStyles).not.toMatch(/\.input-row\b/);
    expect(pickerStyles).not.toMatch(/\.clone-row\b/);
    expect(pickerStyles).not.toMatch(/\.picker-(?:shell|inner|tag|card)\b/);
    expect(responsiveStyles).not.toContain(".picker-card .card-head");
    expect(pickerStyles).not.toContain(".folder-picker-anchor");
    expect(pickerStyles).toMatch(/\.folder-popover\s*\{[^}]*position:\s*fixed;/s);
    expect(pickerStyles).toMatch(/\.folder-popover\s*\{[^}]*width:\s*min\(420px, calc\(100vw - 32px\)\);/s);
    expect(pickerStyles).toMatch(/\.folder-popover\s*\{[^}]*max-height:\s*min\(480px, calc\(100vh - 32px\)\);/s);
    expect(pickerStyles).toMatch(/\.folder-popover\s*\{[^}]*overflow:\s*hidden;/s);
    expect(pickerStyles).not.toMatch(/\.folder-popover\s*\{[^}]*top:\s*16px;/s);
    expect(pickerStyles).toContain("@media (max-width: 640px)");
    expect(pickerStyles).toMatch(/@media \(max-width: 640px\)[\s\S]*\.folder-popover\s*\{[^}]*max-width:\s*calc\(100vw - 32px\);/s);
    expect(pickerStyles).toMatch(/\.folder-list\s*\{[^}]*max-height:\s*300px;[^}]*overflow:\s*auto;/s);
    expect(pickerStyles).toMatch(/\.folder-browser\s*\{[^}]*display:\s*flex;[^}]*flex-direction:\s*column;/s);
    expect(pickerStyles).toMatch(/\.folder-list\s*\{[^}]*min-height:\s*0;[^}]*flex:/s);
    expect(pickerStyles).toMatch(/\.folder-confirm\s*\{[^}]*flex:\s*0 0 auto;/s);
  });

  it("keeps workspace picker chrome aligned with the terminal shell", () => {
    expect(pickerStyles).toMatch(/\.folder-popover-head\s*\{[^}]*padding:\s*12px 14px;/s);
    expect(pickerStyles).toMatch(/\.folder-popover-head\s*\{[^}]*border-bottom:\s*1px solid var\(--border\);/s);
    expect(pickerStyles).toMatch(/\.folder-popover-head button\s*\{[^}]*width:\s*28px;[^}]*height:\s*28px;/s);
    expect(pickerStyles).toMatch(/\.folder-browser-head\s*\{[^}]*background:\s*var\(--bg-2\);/s);
    expect(pickerStyles).toMatch(/\.folder-tree-row\s*\{[^}]*grid-template-columns:\s*28px minmax\(0, 1fr\);/s);
    expect(pickerStyles).toMatch(/\.folder-row\s*\{[^}]*width:\s*100%;[^}]*min-height:\s*36px;/s);
    expect(pickerStyles).toMatch(/\.folder-tree-node\[aria-selected="true"\]\s*>\s*\.folder-tree-row\s*>\s*\.folder-row/);
    expect(pickerStyles).toMatch(/\.folder-tree-node\[aria-selected="true"\]\s*>\s*\.folder-tree-row\s*>\s*\.folder-row \.folder-name/);
    expect(pickerStyles).toMatch(/\.folder-confirm\s*\{[^}]*color:\s*var\(--ansi-white\);/s);
    expect(pickerStyles).toMatch(/\.folder-confirm\s*\{[^}]*min-height:\s*40px;[^}]*margin:\s*10px 14px 14px;/s);
  });

  it("handles folder and delete failure branches", async () => {
    const app = await connectPiApp();
    app.apiConnected = false;
    app.setConnection = vi.fn();
    await app.loadFolder("~");
    expect(app.setConnection).toHaveBeenCalledWith("err");

    app.apiConnected = true;
    globalThis.fetch = vi.fn(async () => failJson("nope"));
    await app.loadFolder("~");
    expect(app.setConnection).toHaveBeenCalledWith("err");

    app.renderFolderListing({ path: "/none", folders: [{ name: "x", path: "/x" }] });

    vi.spyOn(window, "confirm").mockReturnValueOnce(false).mockReturnValueOnce(true);
    await app.deleteWorkspace("w1");
    globalThis.fetch = vi.fn(async () => okJson({ workspaces: [] }));
    app.renderWorkspaces = vi.fn();
    await app.deleteWorkspace("w1");
    expect(app.renderWorkspaces).toHaveBeenCalledWith([]);
    globalThis.fetch = vi.fn(async () => failJson("delete failed"));
    window.confirm.mockReturnValue(true);
    await app.deleteWorkspace("w1");
    expect(app.setConnection).toHaveBeenCalledWith("err");
    await app.deleteWorkspace("");
    delete app.openWorkspace;
    app.activateWorkspaceForSession = vi.fn();
    app.route = vi.fn();
    await app.openWorkspace("w1");
    expect(app.activateWorkspaceForSession).toHaveBeenCalledWith("w1", { loadContext: true, forceLoadContext: true });
  });

  it("boots an empty workspace view without browsing and refreshes the tree button", async () => {
    const app = document.querySelector("pi-app");
    await customElements.whenDefined("pi-app");
    const emptyMain = app.querySelector("[data-main='empty']");
    expect(emptyMain.hidden).toBe(true);
    app.dataset.route = "workspace";
    app.browseFolder = vi.fn();
    app.renderWorkspaces = vi.fn();
    app.loadWorkspaceMeta = vi.fn();
    app.setConnection = vi.fn();
    globalThis.fetch = vi.fn(async () => okJson({ workspaces: [] }));
    await app.bootstrapAPI();
    expect(app.browseFolder).not.toHaveBeenCalled();
    expect(globalThis.fetch).not.toHaveBeenCalledWith(expect.stringContaining("/system/folders"), expect.anything());
    expect(app.querySelector('[data-view="picker"]')).toBeNull();
    expect(emptyMain.hidden).toBe(false);

    const button = document.createElement("button");
    button.dataset.action = "refresh-tree";
    app.append(button);
    app.apiConnected = true;
    app.dataset.activeWorkspaceId = "w1";
    await app.refreshTree();
    expect(app.loadWorkspaceMeta).toHaveBeenCalledWith("w1");
    expect(button.disabled).toBe(false);
  });

  it("does not render workspaces outside configured allowed roots", async () => {
    const app = await connectPiApp();
    app.apiConnected = true;
    app.appConfigState = { config: { workspace: { allowedRoots: ["/allowed"] } } };
    globalThis.fetch = vi.fn(async () => okJson({ workspaces: [
      { id: "inside", name: "inside", path: "/allowed/project", sessions: [] },
      { id: "outside", name: "outside", path: "/other/project", sessions: [] },
    ] }));

    await app.refreshWorkspaces();

    expect(app.querySelector("[data-workspace-group='inside']")).not.toBeNull();
    expect(app.querySelector("[data-workspace-group='outside']")).toBeNull();
  });

  it("filters workspace lists after refresh and delete", async () => {
    const app = await connectPiApp();
    app.apiConnected = true;
    app.appConfigState = { config: { workspace: { allowedRoots: ["/allowed"] } } };
    app.renderWorkspaces = vi.fn();
    vi.spyOn(window, "confirm").mockReturnValue(true);
    const workspaces = [
      { id: "inside", name: "inside", path: "/allowed/project", sessions: [] },
      { id: "outside", name: "outside", path: "/outside/project", sessions: [] },
    ];
    globalThis.fetch = vi.fn(async () => okJson({ workspaces }));

    await app.refreshWorkspaces();
    expect(app.renderWorkspaces).toHaveBeenLastCalledWith([workspaces[0]]);

    await app.deleteWorkspace("outside");
    expect(app.renderWorkspaces).toHaveBeenLastCalledWith([workspaces[0]]);
  });

  it("covers workspace bootstrap, refresh guards, and folder edge branches", async () => {
    const app = await connectPiApp();
    const browser = document.createElement("div");
    browser.dataset.folderBrowser = "";
    app.append(browser);
    app.dataset.route = "workspace";
    app.browseFolder = vi.fn();
    app.renderWorkspaces = vi.fn();
    app.setConnection = vi.fn();
    globalThis.fetch = vi.fn(async (url) => String(url).endsWith("/workspaces")
      ? okJson({ workspaces: [{ id: "w-empty", name: "empty", path: "/empty", sessions: [] }] })
      : okJson({}));

    await app.bootstrapAPI();
    expect(app.browseFolder).not.toHaveBeenCalled();
    expect(app.querySelector('[data-view="picker"]')).toBeNull();

    app.apiConnected = false;
    await app.refreshWorkspaces();
    await app.refreshTree();
    app.apiConnected = true;
    app.dataset.activeWorkspaceId = "";
    globalThis.fetch = vi.fn(async () => okJson({}));
    app.renderWorkspaces = vi.fn();
    await app.refreshWorkspaces();
    expect(app.renderWorkspaces).toHaveBeenCalledWith([]);

    app.apiConnected = true;
    app.dataset.activeWorkspaceId = "w-empty";
    const refresh = document.createElement("button");
    refresh.dataset.action = "refresh-tree";
    app.append(refresh);
    app.loadWorkspaceMeta = vi.fn();
    await app.refreshTree();
    expect(refresh.disabled).toBe(false);

    const list = app.querySelector("[data-folder-list]") || document.createElement("div");
    list.dataset.folderList = "";
    app.append(list);
    app.renderFolderListing({ path: "/fallback", folders: undefined });
    expect(list.querySelector(".folder-empty")).not.toBeNull();

    app.currentFolder = "";
    delete app.browseFolder;
    app.openFolderPopover = vi.fn();
    await app.browseFolder();
    expect(app.openFolderPopover).toHaveBeenCalledWith("workspace");

    const pathOnly = app.folderRow({ name: "plain", path: "/plain" });
    expect(pathOnly.querySelector(".folder-path").textContent).toBe("/plain");

    app.activateBootstrapSession({ id: "s-default", title: "default" });
    app.renderMessages = vi.fn();
    app.connectEvents = vi.fn();
    app.applyLoadedSession({ id: "s-default", title: "default" }, undefined, "");
  });

  it("renders git history as a commit-only list", async () => {
    const app = await connectPiApp();
    const panel = document.createElement("div");
    panel.dataset.gitPanel = "";
    app.append(panel);

    app.renderGitHistory([
      {
        hash: "abc123",
        shortHash: "abc123",
        subject: "initial",
        authorName: "pi",
        date: "2026-01-01T00:00:00Z",
        parents: ["p0", "p1"],
        files: [],
      },
    ]);

    expect(panel.querySelector("[data-git-graph-library]")).toBeNull();
    expect(panel.querySelector("[data-action='toggle-git-graph']")).toBeNull();
    expect(panel.querySelector("[data-git-detail]")).toBeNull();
    expect(panel.querySelector(".git-commit-row")?.textContent).toContain("initial");
  });

  it("opens and closes git commit detail in the right panel", async () => {
    const app = await connectPiApp();
    const historyPanel = document.createElement("div");
    const detailPanel = app.querySelector("[data-git-detail-panel]");
    const body = app.querySelector(".app-body");
    historyPanel.dataset.gitPanel = "";
    app.dataset.activeWorkspaceId = "w1";
    app.append(historyPanel);
    app.renderGitHistory([
      {
        hash: "abc123",
        shortHash: "abc123",
        subject: "initial",
        authorName: "pi",
        date: "2026-01-01T00:00:00Z",
        files: [],
      },
    ]);
    globalThis.fetch = vi.fn(async () => okJson({
      commit: { shortHash: "abc123", subject: "initial", authorName: "pi", date: "2026-01-01T00:00:00Z", files: [] },
      body: "body",
      diff: "diff",
    }));

    await app.selectGitCommit("abc123");

    expect(historyPanel.querySelector("[data-git-detail]")).toBeNull();
    expect(detailPanel.hidden).toBe(false);
    expect(detailPanel.querySelector("[data-git-detail]")?.textContent).toContain("initial");
    expect(detailPanel.querySelector("[data-action='close-git-detail']")).not.toBeNull();
    expect(body.style.gridTemplateColumns).toBe("280px 1fr 560px");

    app.closeGitDetail();
    expect(detailPanel.hidden).toBe(true);
    expect(body.style.gridTemplateColumns).toBe("280px 1fr");
    expect(historyPanel.querySelector(".git-commit-row")?.classList.contains("selected")).toBe(false);
  });

  it("switches the right panel between file and git details", async () => {
    const app = await connectPiApp();
    const historyPanel = document.createElement("div");
    const filePanel = app.querySelector("[data-file-preview]");
    const gitPanel = app.querySelector("[data-git-detail-panel]");
    historyPanel.dataset.gitPanel = "";
    app.dataset.activeWorkspaceId = "w1";
    app.append(historyPanel);
    app.renderGitHistory([{
      hash: "abc123",
      shortHash: "abc123",
      subject: "initial",
      authorName: "pi",
      date: "2026-01-01T00:00:00Z",
      files: [],
    }]);
    app.renderFilePreview({ path: "README.md", previewKind: "unsupported" });
    globalThis.fetch = vi.fn(async () => okJson({
      commit: { shortHash: "abc123", subject: "initial", authorName: "pi", date: "2026-01-01T00:00:00Z", files: [] },
      body: "body",
      diff: "diff",
    }));

    await app.selectGitCommit("abc123");

    expect(filePanel.hidden).toBe(true);
    expect(gitPanel.hidden).toBe(false);

    app.renderFilePreview({ path: "src/main.ts", previewKind: "unsupported" });

    expect(filePanel.hidden).toBe(false);
    expect(gitPanel.hidden).toBe(true);
    expect(historyPanel.querySelector(".git-commit-row")?.classList.contains("selected")).toBe(false);
  });

  it("keeps an unsaved file open when switching to git detail is canceled", async () => {
    const app = await connectPiApp();
    const historyPanel = document.createElement("div");
    const filePanel = app.querySelector("[data-file-preview]");
    const gitPanel = app.querySelector("[data-git-detail-panel]");
    historyPanel.dataset.gitPanel = "";
    app.dataset.activeWorkspaceId = "w1";
    app.append(historyPanel);
    app.renderGitHistory([{
      hash: "abc123",
      shortHash: "abc123",
      subject: "initial",
      authorName: "pi",
      date: "2026-01-01T00:00:00Z",
      files: [],
    }]);
    app.renderFilePreview({ path: "README.md", mime: "text/markdown", previewKind: "text", content: "saved" });
    app.filePreview.editor.view.dispatch({ changes: { from: 0, to: 5, insert: "changed" } });
    vi.spyOn(window, "confirm").mockReturnValue(false);
    globalThis.fetch = vi.fn(async () => okJson({
      commit: { shortHash: "abc123", subject: "initial", authorName: "pi", date: "2026-01-01T00:00:00Z", files: [] },
      body: "body",
      diff: "diff",
    }));

    await app.selectGitCommit("abc123");

    expect(window.confirm).toHaveBeenCalledWith("Discard unsaved file changes?");
    expect(filePanel.hidden).toBe(false);
    expect(gitPanel.hidden).toBe(true);
    expect(historyPanel.querySelector(".git-commit-row")?.classList.contains("selected")).toBe(false);
    expect(globalThis.fetch).not.toHaveBeenCalled();
  });

  it("ignores a stale git response after a file opens", async () => {
    const app = await connectPiApp();
    const historyPanel = document.createElement("div");
    const filePanel = app.querySelector("[data-file-preview]");
    const gitPanel = app.querySelector("[data-git-detail-panel]");
    let resolveGitResponse;
    const gitResponse = new Promise((resolve) => { resolveGitResponse = resolve; });
    historyPanel.dataset.gitPanel = "";
    app.apiConnected = true;
    app.dataset.activeWorkspaceId = "w1";
    app.append(historyPanel);
    app.renderGitHistory([{
      hash: "abc123",
      shortHash: "abc123",
      subject: "initial",
      authorName: "pi",
      date: "2026-01-01T00:00:00Z",
      files: [],
    }]);
    globalThis.fetch = vi.fn(async (url) => {
      if (String(url).includes("/git/commit")) return gitResponse;
      return okJson({ path: "README.md", mime: "text/markdown", previewKind: "text", content: "file" });
    });

    const selecting = app.selectGitCommit("abc123");
    await app.openFilePath("README.md");
    resolveGitResponse(okJson({
      commit: { shortHash: "abc123", subject: "initial", authorName: "pi", date: "2026-01-01T00:00:00Z", files: [] },
      body: "body",
      diff: "diff",
    }));
    await selecting;

    expect(filePanel.hidden).toBe(false);
    expect(gitPanel.hidden).toBe(true);
    expect(historyPanel.querySelector(".git-commit-row")?.classList.contains("selected")).toBe(false);
  });

  it("ignores a stale file response after git detail opens", async () => {
    const app = await connectPiApp();
    const historyPanel = document.createElement("div");
    const filePanel = app.querySelector("[data-file-preview]");
    const gitPanel = app.querySelector("[data-git-detail-panel]");
    let resolveFileResponse;
    const fileResponse = new Promise((resolve) => { resolveFileResponse = resolve; });
    historyPanel.dataset.gitPanel = "";
    app.apiConnected = true;
    app.dataset.activeWorkspaceId = "w1";
    app.append(historyPanel);
    app.renderGitHistory([{
      hash: "abc123",
      shortHash: "abc123",
      subject: "initial",
      authorName: "pi",
      date: "2026-01-01T00:00:00Z",
      files: [],
    }]);
    globalThis.fetch = vi.fn(async (url) => {
      if (String(url).includes("/files/read")) return fileResponse;
      return okJson({
        commit: { shortHash: "abc123", subject: "initial", authorName: "pi", date: "2026-01-01T00:00:00Z", files: [] },
        body: "body",
        diff: "diff",
      });
    });

    const openingFile = app.openFilePath("README.md");
    await app.selectGitCommit("abc123");
    resolveFileResponse(okJson({ path: "README.md", mime: "text/markdown", previewKind: "text", content: "file" }));
    await openingFile;

    expect(filePanel.hidden).toBe(true);
    expect(gitPanel.hidden).toBe(false);
    expect(gitPanel.querySelector("[data-git-detail]")?.textContent).toContain("initial");
  });

  it("invalidates pending inspector requests when the workspace changes", async () => {
    const app = await connectPiApp();
    const historyPanel = document.createElement("div");
    const gitPanel = app.querySelector("[data-git-detail-panel]");
    let resolveGitResponse;
    const gitResponse = new Promise((resolve) => { resolveGitResponse = resolve; });
    historyPanel.dataset.gitPanel = "";
    app.dataset.activeWorkspaceId = "w1";
    app.append(historyPanel);
    app.renderGitHistory([{
      hash: "abc123",
      shortHash: "abc123",
      subject: "initial",
      authorName: "pi",
      date: "2026-01-01T00:00:00Z",
      files: [],
    }]);
    globalThis.fetch = vi.fn(async () => gitResponse);

    const selecting = app.selectGitCommit("abc123");
    app.activateWorkspaceForSession("w2");
    app.activateWorkspaceForSession("w1");
    resolveGitResponse(okJson({
      commit: { shortHash: "abc123", subject: "initial", authorName: "pi", date: "2026-01-01T00:00:00Z", files: [] },
      body: "body",
      diff: "diff",
    }));
    await selecting;

    expect(gitPanel.hidden).toBe(true);
    expect(gitPanel.querySelector("[data-git-detail]")).toBeNull();
  });

  it("loads git history in 30 commit pages", async () => {
    const app = await connectPiApp();
    const panel = document.createElement("div");
    panel.dataset.gitPanel = "";
    app.dataset.activeWorkspaceId = "w1";
    app.gitHistoryLimit = 30;
    app.gitHistoryHasMore = true;
    app.append(panel);
    const commits = Array.from({ length: 60 }, (_, index) => ({
      hash: `hash${index}`,
      shortHash: `h${index}`,
      subject: `commit ${index}`,
      authorName: "pi",
      date: "2026-01-01T00:00:00Z",
      parents: index === 59 ? [] : [`h${index + 1}`],
      files: [],
    }));
    app.renderGitHistory(commits.slice(0, 30));
    panel.querySelector("[data-git-commit-scroll]").scrollTop = 320;
    globalThis.fetch = vi.fn(async () => okJson({ commits }));

    await app.loadMoreGitHistory();

    expect(fetch).toHaveBeenCalledWith(expect.stringContaining("limit=60"), expect.anything());
    expect(panel.querySelectorAll(".git-commit-row")).toHaveLength(60);
    expect(panel.querySelector("[data-git-commit-scroll]").scrollTop).toBe(320);
    expect(panel.querySelector("[data-action='load-more-git-history']")?.textContent).toBe("再加载 30 条");
  });
});
