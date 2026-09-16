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

  it("clears slash commands when the active workspace is removed", async () => {
    const app = await connectPiApp();
    app.dataset.activeWorkspaceId = "w1";
    app.renderSlashCommands([{ command: "/old", description: "old command" }]);

    app.clearActiveWorkspaceView();

    expect(app.slashCommands).toEqual([]);
    expect(app.querySelector(".slash-item")).toBeNull();
  });

  it("clears stale slash commands when command loading fails", async () => {
    const app = await connectPiApp();
    app.dataset.activeWorkspaceId = "w1";
    app.renderSlashCommands([{ command: "/old", description: "old command" }]);
    app.recoverForbiddenWorkspace = vi.fn();
    let resolveRequest;
    const request = new Promise((resolve) => { resolveRequest = resolve; });
    globalThis.fetch = vi.fn(() => request);

    const loading = app.loadWorkspaceCommands("w1");
    await vi.waitFor(() => expect(globalThis.fetch).toHaveBeenCalled());
    expect(app.querySelector("[data-slash='/old']")).toBeNull();
    resolveRequest(failJson("commands unavailable"));
    await loading;

    expect(app.querySelector("[data-slash='/old']")).toBeNull();
    expect(app.querySelector("[data-slash='/compact']")).not.toBeNull();
  });

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
    await app.closeWorkspace("w1");
    expect(app.renderWorkspaces).toHaveBeenCalledWith([]);
  });

  it("activates the workspace returned by the open request instead of the first listed workspace", async () => {
    const app = await connectPiApp();
    app.apiConnected = true;
    app.renderWorkspaces = vi.fn();
    app.openWorkspace = vi.fn();
    globalThis.fetch = vi.fn(async (url) => String(url).endsWith("/workspaces/open")
      ? okJson({ id: "target", name: "target", path: "/selected" })
      : okJson({ workspaces: [{ id: "other", path: "/other", sessions: [] }] }));

    await app.openWorkspacePath("/selected");

    expect(app.openWorkspace).toHaveBeenCalledWith("target");
  });

  it("ignores a stale bootstrap success after a newer generation starts", async () => {
    const app = await connectPiApp();
    app.loadAppConfigState = vi.fn(async () => undefined);
    app.renderWorkspaces = vi.fn();
    let resolveWorkspaces;
    let requestCount = 0;
    globalThis.fetch = vi.fn(() => {
      requestCount += 1;
      return requestCount === 1
        ? new Promise((resolve) => { resolveWorkspaces = resolve; })
        : Promise.resolve(okJson({ workspaces: [] }));
    });

    const bootstrap = app.bootstrapAPI();
    await vi.waitFor(() => expect(globalThis.fetch).toHaveBeenCalled());
    const newerBootstrap = app.bootstrapAPI();
    resolveWorkspaces(okJson({ workspaces: [{ id: "stale", path: "/stale", sessions: [] }] }));
    await Promise.all([bootstrap, newerBootstrap]);

    expect(app.renderWorkspaces).not.toHaveBeenCalledWith([
      expect.objectContaining({ id: "stale" }),
    ]);
  });

  it("does not reactivate a session after deferred forbidden context recovery", async () => {
    const app = await connectPiApp();
    app.loadAppConfigState = vi.fn(async () => undefined);
    app.loadWorkspaceCommands = vi.fn();
    app.loadRuntimeStatus = vi.fn();
    let resolveMeta;
    app.loadWorkspaceMeta = vi.fn(() => new Promise((resolve) => { resolveMeta = resolve; }));
    app.connectEvents = vi.fn();
    let rejectSettings;
    const settingsRequest = new Promise((_, reject) => { rejectSettings = reject; });
    globalThis.fetch = vi.fn(async (url) => {
      const value = String(url);
      if (value.endsWith("/workspaces")) {
        return okJson({ workspaces: [{ id: "w1", name: "one", path: "/one", sessions: [{ id: "s1", title: "old" }] }] });
      }
      if (value.endsWith("/settings")) return settingsRequest;
      return okJson({});
    });

    const bootstrap = app.bootstrapAPI();
    await vi.waitFor(() => expect(globalThis.fetch).toHaveBeenCalledWith(
      expect.stringContaining("/settings"),
      expect.any(Object),
    ));
    rejectSettings(Object.assign(new Error("outside allowed roots"), { status: 403 }));
    await vi.waitFor(() => expect(app.dataset.activeWorkspaceId).toBe(""));
    resolveMeta();
    await bootstrap;

    expect(globalThis.fetch).not.toHaveBeenCalledWith(
      expect.stringContaining("/sessions/s1"),
      expect.any(Object),
    );
    expect(app.dataset.activeWorkspaceId).toBe("");
    expect(app.dataset.activeSessionId).toBe("");
    expect(app.connectEvents).not.toHaveBeenCalled();
    expect(app.querySelector("[data-main='empty']").hidden).toBe(false);
  });

  it("ignores an older refresh response after the newest refresh", async () => {
    const app = await connectPiApp();
    app.apiConnected = true;
    app.dataset.activeWorkspaceId = "w1";
    app.dataset.activeSessionId = "s1";
    const clearView = vi.spyOn(app, "clearActiveWorkspaceView");
    const render = vi.spyOn(app, "renderFilteredWorkspaces");
    let resolveOld;
    let resolveNew;
    let requestCount = 0;
    globalThis.fetch = vi.fn(() => {
      requestCount += 1;
      return new Promise((resolve) => {
        if (requestCount === 1) resolveOld = resolve;
        else resolveNew = resolve;
      });
    });

    const oldRefresh = app.refreshWorkspaces();
    await vi.waitFor(() => expect(globalThis.fetch).toHaveBeenCalledTimes(1));
    const newRefresh = app.refreshWorkspaces();
    await vi.waitFor(() => expect(globalThis.fetch).toHaveBeenCalledTimes(2));
    resolveNew(okJson({ workspaces: [{ id: "w1", name: "one", path: "/one", sessions: [{ id: "s1" }] }] }));
    await newRefresh;
    resolveOld(okJson({ workspaces: [] }));
    await oldRefresh;

    expect(clearView).not.toHaveBeenCalled();
    expect(render).toHaveBeenCalledTimes(1);
    expect(app.dataset.activeWorkspaceId).toBe("w1");
    expect(app.querySelector("[data-workspace-group='w1']")).not.toBeNull();
  });

  it("clears active state when refresh filters out the active workspace", async () => {
    const app = await connectPiApp();
    app.apiConnected = true;
    app.appConfigState = { config: { workspace: { allowedRoots: ["/allowed"] } } };
    app.dataset.activeWorkspaceId = "outside";
    app.dataset.activeSessionId = "s-outside";
    localStorage.setItem("pi.activeSession", JSON.stringify({ workspaceId: "outside", sessionId: "s-outside" }));
    globalThis.fetch = vi.fn(async () => okJson({
      workspaces: [{ id: "inside", path: "/allowed/project", sessions: [{ id: "s-inside" }] }],
    }));

    await app.refreshWorkspaces();

    expect(app.dataset.activeWorkspaceId).toBe("");
    expect(app.dataset.activeSessionId).toBe("");
    expect(localStorage.getItem("pi.activeSession")).toBeNull();
    expect(app.querySelector("[data-main='empty']").hidden).toBe(false);
  });

  it("clears active state when the active session is not owned by refreshed workspace", async () => {
    const app = await connectPiApp();
    app.apiConnected = true;
    app.dataset.activeWorkspaceId = "w1";
    app.dataset.activeSessionId = "s-old";
    localStorage.setItem("pi.activeSession", JSON.stringify({ workspaceId: "w1", sessionId: "s-old" }));
    globalThis.fetch = vi.fn(async () => okJson({
      workspaces: [{ id: "w1", path: "/one", sessions: [{ id: "s-new" }] }],
    }));

    await app.refreshWorkspaces();

    expect(app.dataset.activeWorkspaceId).toBe("");
    expect(app.dataset.activeSessionId).toBe("");
    expect(localStorage.getItem("pi.activeSession")).toBeNull();
    expect(app.querySelector("[data-main='empty']").hidden).toBe(false);
  });

  it("does not recreate background watchers while clearing active workspace view", async () => {
    const app = await connectPiApp();
    app.apiConnected = true;
    app.dataset.activeWorkspaceId = "w1";
    app.dataset.activeSessionId = "s1";
    const row = document.createElement("div");
    row.className = "session-row active";
    row.dataset.session = "s2";
    row.dataset.workspace = "w1";
    app.append(row);
    const watch = vi.spyOn(app, "watchBackgroundSession");
    const clientStop = vi.fn();
    app.backgroundSessionWatches = new Map([["s2", { client: { stop: clientStop }, source: { close: vi.fn() } }]]);

    app.clearActiveWorkspaceView();

    expect(watch).not.toHaveBeenCalled();
    expect(clientStop).toHaveBeenCalled();
    expect(row.classList.contains("active")).toBe(false);
  });

  it("clears the active workspace when refresh returns no allowed workspace", async () => {
    const app = await connectPiApp();
    app.apiConnected = true;
    app.dataset.activeWorkspaceId = "outside";
    app.dataset.activeSessionId = "stale-session";
    const workspaceLabel = document.createElement("span");
    workspaceLabel.dataset.activeWorkspace = "";
    const sessionLabel = document.createElement("span");
    sessionLabel.dataset.activeSessionTitle = "";
    app.append(workspaceLabel, sessionLabel);
    const closeDurableStream = vi.fn();
    app.durableStream = { stop: closeDurableStream };
    app.workspaceFiles = [{ name: "stale.ts" }];
    const dispatchTreeUpdate = vi.spyOn(window, "dispatchEvent");
    localStorage.setItem("pi.activeSession", JSON.stringify({ workspaceId: "outside", sessionId: "stale-session" }));
    globalThis.fetch = vi.fn(async () => okJson({ workspaces: [] }));

    await app.refreshWorkspaces();

    expect(app.dataset.activeWorkspaceId).toBe("");
    expect(app.dataset.activeSessionId).toBe("");
    expect(app.querySelector("[data-main='empty']").hidden).toBe(false);
    expect(app.querySelector("[data-active-workspace]")?.textContent).toBe("工作区");
    expect(app.querySelector("[data-active-session-title]")?.textContent).toBe("无会话");
    expect(localStorage.getItem("pi.activeSession")).toBeNull();
    expect(closeDurableStream).toHaveBeenCalled();
    expect(app.workspaceFiles).toEqual([]);
    expect(dispatchTreeUpdate).toHaveBeenCalledWith(expect.objectContaining({
      type: "pi-workspace-tree:update",
      detail: { files: [], statusMap: {}, selectedPath: "" },
    }));
  });

  it("clears forbidden workspace labels before showing the empty view", async () => {
    const app = await connectPiApp();
    app.dataset.activeWorkspaceId = "forbidden";
    app.dataset.activeSessionId = "session";
    const workspaceLabel = document.createElement("span");
    workspaceLabel.dataset.activeWorkspace = "";
    workspaceLabel.textContent = "secret workspace";
    const sessionLabel = document.createElement("span");
    sessionLabel.dataset.activeSessionTitle = "";
    sessionLabel.textContent = "secret session";
    app.append(workspaceLabel, sessionLabel);

    app.recoverForbiddenWorkspace(Object.assign(new Error("outside allowed roots"), { status: 403 }));

    expect(workspaceLabel.textContent).toBe("工作区");
    expect(sessionLabel.textContent).toBe("无会话");
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

  it("shows the active session after bootstrapping from the initial empty view", async () => {
    const app = await connectPiApp();
    app.showEmptyMain();
    app.loadAppConfigState = vi.fn(async () => undefined);
    app.loadAdminPasswordState = vi.fn(async () => undefined);
    app.loadWorkspaceCommands = vi.fn();
    app.loadRuntimeStatus = vi.fn();
    app.loadWorkspaceSettingsState = vi.fn();
    app.loadWorkspaceMeta = vi.fn();
    app.loadSession = vi.fn();
    globalThis.fetch = vi.fn(async (url) => String(url).endsWith("/workspaces")
      ? okJson({ workspaces: [{ id: "w1", name: "one", path: "/one", sessions: [{ id: "s1", title: "one" }] }] })
      : okJson({}));

    await app.bootstrapAPI();

    expect(app.dataset.activeWorkspaceId).toBe("w1");
    expect(app.dataset.activeSessionId).toBe("s1");
    expect(app.dataset.session).toBe("active");
    expect(app.querySelector("[data-main='session']").hidden).toBe(false);
    expect(app.querySelector("[data-main='empty']").hidden).toBe(true);
  });

  it("keeps the workspace route without browsing during an empty bootstrap", async () => {
    const app = await connectPiApp();
    const emptyPrompt = document.createElement("div");
    emptyPrompt.dataset.emptyNoWorkspace = "";
    emptyPrompt.hidden = true;
    app.append(emptyPrompt);
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
    expect(emptyPrompt.hidden).toBe(false);
    app.dataset.activeWorkspaceId = "w1";
    app.syncEmptyWorkspaceState();
    expect(emptyPrompt.hidden).toBe(true);
    expect(app.querySelector('[data-view="picker"]')).toBeNull();
    expect(app.querySelector("[data-action='route-picker']")).not.toBeNull();
  });

  it("clears stale active state before showing an empty bootstrap", async () => {
    const app = await connectPiApp();
    app.dataset.activeWorkspaceId = "stale-workspace";
    app.dataset.activeSessionId = "stale-session";
    globalThis.fetch = vi.fn(async (url) => String(url).endsWith("/workspaces")
      ? okJson({ workspaces: [] })
      : okJson({}));

    await app.bootstrapAPI();

    expect(app.dataset.activeWorkspaceId).toBe("");
    expect(app.dataset.activeSessionId).toBe("");
    expect(app.querySelector("[data-main='empty']").hidden).toBe(false);
  });

  it("ignores a refresh response after opening another workspace", async () => {
    const app = await connectPiApp();
    app.apiConnected = true;
    app.dataset.activeWorkspaceId = "w-old";
    app.dataset.activeSessionId = "s-old";
    app.loadWorkspaceContext = vi.fn();
    let resolveRefresh;
    let workspaceRequestCount = 0;
    globalThis.fetch = vi.fn(async (url) => {
      const value = String(url);
      if (value.endsWith("/workspaces")) {
        workspaceRequestCount += 1;
        if (workspaceRequestCount === 1) return new Promise((resolve) => { resolveRefresh = resolve; });
        return okJson({ workspaces: [{ id: "w-new", name: "new", path: "/new", sessions: [] }] });
      }
      if (value.endsWith("/workspaces/open")) return okJson({ id: "w-new", name: "new", path: "/new" });
      return okJson({});
    });

    const refresh = app.refreshWorkspaces();
    await vi.waitFor(() => expect(globalThis.fetch).toHaveBeenCalledTimes(1));
    await app.openWorkspacePath("/new");
    resolveRefresh(okJson({ workspaces: [{ id: "w-old", name: "old", path: "/old", sessions: [{ id: "s-old" }] }] }));
    await refresh;

    expect(app.dataset.activeWorkspaceId).toBe("w-new");
    expect(app.querySelector("[data-workspace-group='w-old']")).toBeNull();
    expect(app.querySelector("[data-workspace-group='w-new']")).not.toBeNull();
  });

  it("ignores a refresh response after a newer bootstrap starts", async () => {
    const app = await connectPiApp();
    app.apiConnected = true;
    app.dataset.activeWorkspaceId = "w-old";
    app.dataset.activeSessionId = "s-old";
    let resolveRefresh;
    let workspaceRequestCount = 0;
    globalThis.fetch = vi.fn(async (url) => {
      const value = String(url);
      if (value.endsWith("/workspaces")) {
        workspaceRequestCount += 1;
        if (workspaceRequestCount === 1) return new Promise((resolve) => { resolveRefresh = resolve; });
        return okJson({ workspaces: [] });
      }
      return okJson({});
    });

    const refresh = app.refreshWorkspaces();
    await vi.waitFor(() => expect(globalThis.fetch).toHaveBeenCalledTimes(1));
    await app.bootstrapAPI();
    resolveRefresh(okJson({ workspaces: [{ id: "w-old", name: "old", path: "/old", sessions: [{ id: "s-old" }] }] }));
    await refresh;

    expect(app.querySelector("[data-main='empty']").hidden).toBe(false);
    expect(app.querySelector("[data-workspace-group='w-old']")).toBeNull();
  });

  it("ignores deferred runtime metadata after forbidden recovery", async () => {
    const app = await connectPiApp();
    app.apiConnected = true;
    app.dataset.activeWorkspaceId = "w-old";
    app.dataset.activeSessionId = "s-old";
    let resolveModel;
    globalThis.fetch = vi.fn(async (url) => String(url).includes("/runtime-model")
      ? new Promise((resolve) => { resolveModel = resolve; })
      : okJson({ contextUsage: null }));

    const loading = app.loadRuntimeStatus("w-old");
    await vi.waitFor(() => expect(globalThis.fetch).toHaveBeenCalledWith(
      expect.stringContaining("/runtime-model"),
      expect.any(Object),
    ));
    app.recoverForbiddenWorkspace(Object.assign(new Error("outside allowed roots"), { status: 403 }));
    resolveModel(okJson({ status: { model: "OLD" } }));
    await loading;

    expect(app.querySelector("[data-prompt-meta]").textContent).not.toContain("OLD");
  });

  it("ignores deferred runtime quota after forbidden recovery", async () => {
    const app = await connectPiApp();
    app.apiConnected = true;
    app.dataset.activeWorkspaceId = "w-old";
    let resolveQuota;
    globalThis.fetch = vi.fn(async (url) => {
      const value = String(url);
      if (value.endsWith("/runtime-quota?model=CURRENT")) {
        return new Promise((resolve) => { resolveQuota = resolve; });
      }
      if (value.includes("/runtime-model")) return okJson({ status: { model: "CURRENT" } });
      return okJson({ contextUsage: null });
    });

    const loading = app.loadRuntimeStatus("w-old");
    await vi.waitFor(() => expect(resolveQuota).toBeTypeOf("function"));
    app.recoverForbiddenWorkspace(Object.assign(new Error("outside allowed roots"), { status: 403 }));
    resolveQuota(okJson({ status: { fiveHourQuota: 99 } }));
    await loading;
    await Promise.resolve();

    expect(app.querySelector("[data-prompt-meta]").textContent).not.toContain("99%");
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
    const loadSettings = vi.spyOn(app, "loadWorkspaceSettingsState");
    const recoverForbiddenWorkspace = vi.spyOn(app, "recoverForbiddenWorkspace");
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
    expect(loadSettings).toHaveBeenCalledWith("w-old");
    const settingsHandling = loadSettings.mock.results.at(-1)?.value;
    app.dataset.activeWorkspaceId = "w-new";
    app.dataset.activeSessionId = "s-new";
    rejectSettings(new Error("403 Forbidden"));
    await expect(settingsHandling).resolves.toBeUndefined();
    await bootstrap;

    expect(recoverForbiddenWorkspace).not.toHaveBeenCalled();
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
    app.transcriptState = {};

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

  it("fails closed when the durable session snapshot is unavailable", async () => {
    const app = await connectPiApp();
    app.apiConnected = true;
    app.durableEvents = false;
    app.setConnection = vi.fn();
    app.applyLoadedSession = vi.fn();
    app.connectEvents = vi.fn();
    globalThis.fetch = vi.fn(async (url) => {
      if (String(url).includes("/state")) throw new Error("event store unavailable");
      return okJson({ session: { id: "s1", title: "legacy" }, messages: [], status: "idle" });
    });

    await app.loadSession("s1");

    expect(app.applyLoadedSession).not.toHaveBeenCalled();
    expect(app.connectEvents).not.toHaveBeenCalled();
    expect(app.setConnection).toHaveBeenCalledWith("err");
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
    app.connectDurableEvents = vi.fn();
    app.applyLoadedDurableSession({
      session: { id: "s-row", title: "row", workspaceId: "w-row" },
      metadata: { title: "row", workspaceId: "w-row" }, items: [], status: "idle", seq: "0",
      cursor: "", hasMore: false, limit: 120, projectionSchemaVersion: 1, projectionGeneration: "1",
    });
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
