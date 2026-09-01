// @ts-nocheck
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanupPiAppFixture, connectPiApp, installPiAppFixture } from "../test-helper";

function okJson(body) {
  return { ok: true, status: 200, statusText: "OK", json: async () => body };
}

function failJson(message = "boom") {
  return { ok: false, status: 500, statusText: "ERR", json: async () => ({ error: message }) };
}

describe("workspace bootstrap coverage", () => {
  beforeEach(() => {
    installPiAppFixture();
    globalThis.PI_WEB_API_BASE = "http://backend.test";
  });
  afterEach(cleanupPiAppFixture);

  it("covers workspace API fallback branches", async () => {
    const app = await connectPiApp();
    app.apiConnected = true;
    app.renderWorkspaces = vi.fn();
    app.openWorkspace = vi.fn();
    globalThis.fetch = vi.fn(async (url) => String(url).endsWith("/workspaces/open")
      ? okJson({})
      : okJson({ workspaces: null }));
    await app.openWorkspacePath("/missing");
    expect(app.renderWorkspaces).toHaveBeenCalledWith([]);

    vi.spyOn(window, "confirm").mockReturnValue(true);
    globalThis.fetch = vi.fn(async () => okJson({ workspaces: null }));
    await app.deleteWorkspace("w1");
    expect(app.renderWorkspaces).toHaveBeenCalledWith([]);
  });

  it("shows app config load failures while continuing workspace bootstrap", async () => {
    const app = await connectPiApp();
    app.renderWorkspaces = vi.fn();
    app.loadAppConfigState = vi.fn(async () => {
      throw new Error("app config unavailable");
    });
    globalThis.fetch = vi.fn(async (url) => String(url).endsWith("/workspaces")
      ? okJson({ workspaces: [] })
      : okJson({}));

    await app.bootstrapAPI();

    expect(app.renderWorkspaces).toHaveBeenCalledWith([]);
    const toast = document.querySelector(".session-toast.error");
    expect(toast?.textContent).toContain("应用配置加载失败");
    expect(toast?.textContent).toContain("app config unavailable");
    expect(app.querySelector("[data-brand]").textContent).toBe("pi.dev / web");
  });

  it("filters the initial bootstrap workspace list", async () => {
    const app = await connectPiApp();
    app.dataset.route = "workspace";
    app.renderWorkspaces = vi.fn();
    app.loadWorkspaceCommands = vi.fn();
    app.loadRuntimeStatus = vi.fn();
    app.loadWorkspaceMeta = vi.fn();
    app.loadWorkspaceSettingsState = vi.fn();
    globalThis.fetch = vi.fn(async (url) => String(url).endsWith("/app-config")
      ? okJson({ config: { version: 1, brand: "x", workspace: { allowedRoots: ["/allowed"] } }, path: "/config" })
      : okJson({ workspaces: [
        { id: "inside", name: "inside", path: "/allowed/project", sessions: [] },
        { id: "outside", name: "outside", path: "/outside/project", sessions: [] },
      ] }));

    await app.bootstrapAPI();

    expect(app.renderWorkspaces).toHaveBeenCalledWith([
      expect.objectContaining({ id: "inside" }),
    ]);
    expect(app.renderWorkspaces.mock.calls.at(-1)[0]).toHaveLength(1);
  });

  it("keeps the workspace route without browsing during an empty bootstrap", async () => {
    const app = await connectPiApp();
    const emptyMain = app.querySelector("[data-main='empty']");
    expect(emptyMain.hidden).toBe(true);
    app.dataset.route = "workspace";
    app.browseFolder = vi.fn();
    globalThis.fetch = vi.fn(async (url) => String(url).endsWith("/workspaces")
      ? okJson({ workspaces: [] })
      : okJson({}));

    await app.bootstrapAPI();

    expect(app.browseFolder).not.toHaveBeenCalled();
    expect(app.dataset.route).toBe("workspace");
    expect(emptyMain.hidden).toBe(false);
    expect(app.querySelector('[data-view="picker"]')).toBeNull();
    expect(app.querySelector("[data-action='route-picker']")).not.toBeNull();
  });

  it("clears folder popover state before returning to the empty workspace view", async () => {
    const app = await connectPiApp();
    const trigger = app.querySelector("[data-action='route-picker']");
    const popover = app.querySelector("[data-folder-popover]");
    const emptyMain = app.querySelector("[data-main='empty']");
    app.folderOpeningPending = true;
    app.folderBrowsePurpose = "workspace";
    popover.hidden = false;
    trigger.setAttribute("aria-expanded", "true");

    expect(app.recoverForbiddenWorkspace(new Error("403 Forbidden"))).toBe(true);

    expect(app.dataset.route).toBe("workspace");
    expect(emptyMain.hidden).toBe(false);
    expect(app.querySelector('[data-view="picker"]')).toBeNull();
    expect(popover.hidden).toBe(true);
    expect(trigger.getAttribute("aria-expanded")).toBe("false");
    expect(app.folderOpeningPending).toBe(false);
  });

  it("recovers settings forbidden errors and clears the persisted active session", async () => {
    const app = await connectPiApp();
    app.apiConnected = true;
    app.dataset.activeWorkspaceId = "w-forbidden";
    app.dataset.activeSessionId = "s-forbidden";
    const emptyMain = app.querySelector("[data-main='empty']");
    localStorage.setItem("pi.activeSession", JSON.stringify({
      workspaceId: "w-forbidden",
      sessionId: "s-forbidden",
    }));
    app.browseFolder = vi.fn();
    globalThis.fetch = vi.fn(async () => ({
      ok: false,
      status: 403,
      statusText: "Forbidden",
      json: async () => ({ error: "outside allowed roots" }),
    }));

    await expect(app.loadWorkspaceSettingsState("w-forbidden")).resolves.toBeUndefined();

    expect(app.dataset.route).toBe("workspace");
    expect(emptyMain.hidden).toBe(false);
    expect(app.querySelector('[data-view="picker"]')).toBeNull();
    expect(app.querySelector("[data-folder-popover]").hidden).toBe(true);
    expect(app.querySelector("[data-action='route-picker']").getAttribute("aria-expanded")).toBe("false");
    expect(app.dataset.activeWorkspaceId).toBe("");
    expect(app.dataset.activeSessionId).toBe("");
    expect(localStorage.getItem("pi.activeSession")).toBeNull();
    expect([...document.querySelectorAll(".session-toast.error")].some((toast) =>
      toast.textContent.includes("outside allowed roots"))).toBe(true);
  });

  it("ignores a deferred forbidden settings response after switching workspaces", async () => {
    const app = await connectPiApp();
    app.loadAppConfigState = vi.fn(async () => undefined);
    app.loadWorkspaceCommands = vi.fn();
    app.loadRuntimeStatus = vi.fn();
    app.loadWorkspaceMeta = vi.fn();
    app.dataset.route = "workspace";
    let rejectSettings;
    const settingsRequest = new Promise((_, reject) => {
      rejectSettings = reject;
    });
    globalThis.fetch = vi.fn(async (url) => {
      const value = String(url);
      if (value.endsWith("/workspaces")) {
        return okJson({ workspaces: [{ id: "w-old", name: "old", path: "/old", sessions: [] }] });
      }
      if (value.includes("/settings")) return settingsRequest;
      return okJson({});
    });

    const bootstrap = app.bootstrapAPI();
    await vi.waitFor(() => expect(globalThis.fetch).toHaveBeenCalledWith(
      expect.stringContaining("/settings"),
      expect.any(Object),
    ));
    expect(app.dataset.activeWorkspaceId).toBe("w-old");
    app.dataset.activeWorkspaceId = "w-new";
    app.dataset.activeSessionId = "s-new";
    rejectSettings(new Error("403 Forbidden"));
    await bootstrap;
    await vi.waitFor(() => expect(app.dataset.activeWorkspaceId).toBe("w-new"));

    expect(app.dataset.activeWorkspaceId).toBe("w-new");
    expect(app.dataset.activeSessionId).toBe("s-new");
    expect(app.dataset.route).toBe("workspace");
    expect(app.querySelector('[data-view="picker"]')).toBeNull();
  });

  it("shows the empty main when a session becomes forbidden", async () => {
    const app = await connectPiApp();
    app.apiConnected = true;
    app.dataset.activeWorkspaceId = "w-forbidden";
    app.dataset.activeSessionId = "s-forbidden";
    const emptyMain = app.querySelector("[data-main='empty']");
    localStorage.setItem("pi.activeSession", JSON.stringify({
      workspaceId: "w-forbidden",
      sessionId: "s-forbidden",
    }));
    app.browseFolder = vi.fn();
    globalThis.fetch = vi.fn(async () => ({
      ok: false,
      status: 403,
      statusText: "Forbidden",
      json: async () => ({ error: "outside allowed roots" }),
    }));

    await app.loadSession("s-forbidden");

    expect(app.dataset.route).toBe("workspace");
    expect(emptyMain.hidden).toBe(false);
    expect(app.querySelector('[data-view="picker"]')).toBeNull();
    expect(app.querySelector("[data-folder-popover]").hidden).toBe(true);
    expect(app.querySelector("[data-action='route-picker']").getAttribute("aria-expanded")).toBe("false");
    expect(app.dataset.activeWorkspaceId).toBe("");
    expect(app.dataset.activeSessionId).toBe("");
    expect(localStorage.getItem("pi.activeSession")).toBeNull();
    expect([...document.querySelectorAll(".session-toast.error")].some((toast) =>
      toast.textContent.includes("outside allowed roots"))).toBe(true);
  });

  it("ignores delayed forbidden metadata after switching workspaces", async () => {
    const app = await connectPiApp();
    app.apiConnected = true;
    app.dataset.activeWorkspaceId = "w-old";
    app.dataset.activeSessionId = "s-old";
    app.dataset.route = "workspace";
    let rejectFiles;
    globalThis.fetch = vi.fn(async (url) => String(url).includes("/files")
      ? new Promise((_, reject) => { rejectFiles = reject; })
      : okJson({ branch: "main", files: {} }));

    const loading = app.loadWorkspaceMeta("w-old");
    await vi.waitFor(() => expect(globalThis.fetch).toHaveBeenCalledWith(
      expect.stringContaining("/files"),
      expect.any(Object),
    ));
    app.dataset.activeWorkspaceId = "w-new";
    app.dataset.activeSessionId = "s-new";
    rejectFiles(new Error("403 Forbidden"));
    await loading;

    expect(app.dataset.activeWorkspaceId).toBe("w-new");
    expect(app.dataset.activeSessionId).toBe("s-new");
    expect(app.dataset.route).toBe("workspace");
    expect(app.querySelector('[data-view="picker"]')).toBeNull();
  });

  it("ignores delayed forbidden commands after switching workspaces", async () => {
    const app = await connectPiApp();
    app.apiConnected = true;
    app.dataset.activeWorkspaceId = "w-old";
    app.dataset.activeSessionId = "s-old";
    app.dataset.route = "workspace";
    let rejectCommands;
    globalThis.fetch = vi.fn(async () => new Promise((_, reject) => { rejectCommands = reject; }));

    const loading = app.loadWorkspaceCommands("w-old");
    await vi.waitFor(() => expect(globalThis.fetch).toHaveBeenCalled());
    app.dataset.activeWorkspaceId = "w-new";
    app.dataset.activeSessionId = "s-new";
    rejectCommands(new Error("403 Forbidden"));
    await loading;

    expect(app.dataset.activeWorkspaceId).toBe("w-new");
    expect(app.dataset.activeSessionId).toBe("s-new");
    expect(app.dataset.route).toBe("workspace");
    expect(app.querySelector('[data-view="picker"]')).toBeNull();
  });

  it("ignores delayed forbidden history after switching sessions", async () => {
    const app = await connectPiApp();
    app.apiConnected = true;
    app.dataset.activeWorkspaceId = "w-old";
    app.dataset.activeSessionId = "s-old";
    app.dataset.route = "workspace";
    app.sessionHistoryCursor = "cursor";
    app.sessionHistoryHasMore = true;
    let rejectHistory;
    globalThis.fetch = vi.fn(async () => new Promise((_, reject) => { rejectHistory = reject; }));

    const loading = app.loadOlderSessionMessages();
    await vi.waitFor(() => expect(globalThis.fetch).toHaveBeenCalled());
    app.dataset.activeWorkspaceId = "w-new";
    app.dataset.activeSessionId = "s-new";
    rejectHistory(new Error("403 Forbidden"));
    await loading;

    expect(app.dataset.activeWorkspaceId).toBe("w-new");
    expect(app.dataset.activeSessionId).toBe("s-new");
    expect(app.dataset.route).toBe("workspace");
    expect(app.querySelector('[data-view="picker"]')).toBeNull();
  });

  it("ignores delayed forbidden settings after switching workspaces", async () => {
    const app = await connectPiApp();
    app.apiConnected = true;
    app.dataset.activeWorkspaceId = "w-old";
    app.dataset.activeSessionId = "s-old";
    app.dataset.route = "workspace";
    let rejectSettings;
    globalThis.fetch = vi.fn(async (url) => {
      const value = String(url);
      if (value.includes("/settings")) return new Promise((_, reject) => { rejectSettings = reject; });
      if (value.includes("/app-config")) return okJson({ config: { version: 1, brand: "x" }, path: "/config" });
      if (value.includes("/auth/")) return okJson({ providers: [] });
      return okJson({ providers: [] });
    });

    const loading = app.openSettingsModal();
    await vi.waitFor(() => expect(globalThis.fetch).toHaveBeenCalledWith(
      expect.stringContaining("/settings"),
      expect.any(Object),
    ));
    app.dataset.activeWorkspaceId = "w-new";
    app.dataset.activeSessionId = "s-new";
    rejectSettings(new Error("403 Forbidden"));
    await loading;

    expect(app.dataset.activeWorkspaceId).toBe("w-new");
    expect(app.dataset.activeSessionId).toBe("s-new");
    expect(app.dataset.route).toBe("workspace");
    expect(app.querySelector('[data-view="picker"]')).toBeNull();
  });

  it("covers bootstrap fallback branches", async () => {
    localStorage.setItem("pi.activeSession", JSON.stringify({ workspaceId: "w-missing", sessionId: "missing" }));
    const app = await connectPiApp();
    app.apiConnected = true;
    app.renderWorkspaces = vi.fn();
    app.openActiveWorkspaceGroup = vi.fn();
    app.setConnection = vi.fn();
    app.querySelector("[data-action='refresh-workspaces']")?.remove();
    globalThis.fetch = vi.fn(async (url) => String(url).endsWith("/workspaces")
      ? okJson({ workspaces: [{ id: "stored", name: "stored", path: "/stored", sessions: [{ id: "other", title: "other" }] }] })
      : okJson({}));
    await app.bootstrapAPI();
    globalThis.fetch = vi.fn(async (url) => String(url).endsWith("/workspaces")
      ? okJson({ workspaces: null })
      : okJson({}));
    await app.refreshWorkspaces();
    expect(app.renderWorkspaces).toHaveBeenCalledWith([]);

    app.sessionLoadToken = Symbol("old");
    globalThis.fetch = vi.fn(async () => { throw new Error("session fail"); });
    await app.loadSession("missing");
    expect(app.setConnection).toHaveBeenCalledWith("err");
    app.setConnection.mockClear();
    let rejectStale;
    globalThis.fetch = vi.fn(() => new Promise((_, reject) => { rejectStale = reject; }));
    const loading = app.loadSession("stale");
    app.sessionLoadToken = Symbol("newer");
    rejectStale(new Error("stale"));
    await loading;
    expect(app.setConnection).not.toHaveBeenCalled();

    const row = document.createElement("div");
    row.dataset.session = "s-row";
    row.dataset.workspace = "w-row";
    app.append(row);
    expect(app.findSessionRow("s-row")).toBe(row);
    app.renderMessages = vi.fn();
    app.connectEvents = vi.fn();
    app.applyLoadedSession({ id: "s-row", title: "row" }, null, "");
    expect(app.dataset.activeWorkspaceId).toBe("w-row");

    const group = document.createElement("div");
    group.dataset.workspaceGroup = "bare";
    app.append(group);
    expect(() => app.openActiveWorkspaceGroup("bare")).not.toThrow();
  });

  it("boots, refreshes, and applies workspace metadata", async () => {
    const app = document.querySelector("pi-app");
    await customElements.whenDefined("pi-app");
    const activeLabel = document.createElement("span");
    const activeTitle = document.createElement("span");
    const tree = document.createElement("div");
    const git = document.createElement("span");
    activeLabel.dataset.activeWorkspace = "";
    activeTitle.dataset.activeSessionTitle = "";
    tree.className = "tree-list";
    git.dataset.gitStatus = "";
    app.append(activeLabel, activeTitle, tree, git);
    app.renderSlashCommands = vi.fn();
    app.route = vi.fn();
    app.connectEvents = vi.fn();
    app.updatePromptMeta = vi.fn();
    globalThis.fetch = vi.fn(async (url) => {
      const value = String(url);
      if (value.endsWith("/workspaces-null")) return okJson({});
      if (value.endsWith("/workspaces")) return okJson({ workspaces: [{
        id: "w1", name: "one", path: "/one", sessions: [{ id: "s1", title: "first", lastUsed: "now" }],
      }] });
      if (value.includes("/files")) return okJson({ files: [{ type: "file", name: "a.ts", depth: 0 }] });
      if (value.includes("/git/status")) return okJson({ branch: "main", dirty: 2 });
      if (value.includes("/commands")) return okJson({ commands: [{ command: "/x" }] });
      if (value.includes("/runtime-status")) return okJson({});
      if (value.includes("/sessions/s1")) return okJson({ session: { id: "s1", title: "first", workspaceId: "w1" } });
      if (value.includes("/version")) return okJson({});
      return okJson({});
    });

    await app.bootstrapAPI();
    await app.loadWorkspaceCommands("w1");
    app.apiConnected = false;
    await app.refreshWorkspaces();
    app.apiConnected = true;
    app.dataset.activeWorkspaceId = "w1";
    await app.refreshWorkspaces();
    globalThis.fetch = vi.fn(async () => failJson("refresh failed"));
    await app.refreshWorkspaces();
    await app.refreshTree();
    app.dataset.activeWorkspaceId = "";
    await app.refreshTree();
    await app.loadSession("missing");
    app.renderGitStatus({ branch: "dev", dirty: 1 });
    app.renderWorkspaceTree(null);

    expect(app.apiConnected).toBe(true);
    expect(activeLabel.textContent).toBe("one");
    expect(activeTitle.textContent).toBe("first");
    expect(app.workspaceFiles?.[0]?.name).toBe("a.ts");
    expect(git.textContent).toBe("dev · 1 ✱");
    expect(app.renderSlashCommands).toHaveBeenCalledWith([{ command: "/x" }]);
    const row = document.createElement("button");
    row.dataset.session = "find-me";
    app.append(row);
    expect(app.findSessionRow("find-me")).toBe(row);
  });
});
