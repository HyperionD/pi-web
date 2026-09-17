// @ts-nocheck
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanupPiAppFixture, connectPiApp, installPiAppFixture } from "../test-helper";

describe("pi-app workspace navigation", () => {
  beforeEach(installPiAppFixture);
  afterEach(cleanupPiAppFixture);

  it("creates a new session in the workspace selected from the sidebar", async () => {
    globalThis.PI_WEB_API_BASE = "http://backend.test";
    globalThis.fetch = vi.fn(async () => ({
      ok: true,
      status: 201,
      statusText: "Created",
      json: async () => ({ session: { id: "s2", title: "new session", lastUsed: "now" } }),
    }));
    const app = await connectPiApp();
    const sessionMain = app.querySelector("[data-main='session']");
    const activeWorkspace = document.createElement("span");
    activeWorkspace.dataset.activeWorkspace = "";
    app.append(activeWorkspace);
    app.apiConnected = true;
    app.dataset.activeWorkspaceId = "hahn";
    app.loadWorkspaceCommands = vi.fn();
    app.loadRuntimeStatus = vi.fn();
    app.loadWorkspaceMeta = vi.fn();
    app.connectEvents = vi.fn();
    app.renderWorkspaces([
      { id: "hahn", name: "hahn-monorepo", path: "/hahn", sessionCount: 0, sessions: [] },
      { id: "juun", name: "juun-ai", path: "/juun", sessionCount: 0, sessions: [] },
    ]);

    app.querySelector("[data-workspace='juun'].ws-row").click();
    expect(app.loadWorkspaceMeta).not.toHaveBeenCalled();
    await app.newSession("juun");

    expect(globalThis.fetch).toHaveBeenCalledWith(
      "http://backend.test/api/workspaces/juun/sessions",
      expect.objectContaining({ method: "POST" }),
    );
    expect(app.dataset.activeWorkspaceId).toBe("juun");
    expect(activeWorkspace.textContent).toBe("juun-ai");
    expect(app.querySelector("[data-workspace='juun'].ws-row").getAttribute("aria-current")).toBe("true");
    expect(app.querySelector("[data-session='s2']").classList.contains("selected")).toBe(true);
    expect(app.querySelector("[data-session='s2']").classList.contains("active")).toBe(false);
    expect(app.querySelector("[data-session='s2'] .meta").hidden).toBe(true);
  });

  it("keeps the sidebar workspace trigger and recent workspaces in the main route", async () => {
    const app = await connectPiApp();
    const recent = document.createElement("div");
    recent.dataset.recentWorkspaces = "";
    app.append(recent);
    app.openWorkspace = vi.fn();
    app.apiConnected = true;
    globalThis.fetch = vi.fn(async () => ({
      ok: true,
      status: 200,
      statusText: "OK",
      json: async () => ({ path: "/allowed", parent: "/", folders: [] }),
    }));
    app.dataset.route = "workspace";
    app.renderWorkspaces([
      { id: "w1", name: "one", path: "/one", sessionCount: 0, sessions: [] },
    ]);

    const trigger = app.querySelector("[data-action='route-picker']");
    const event = { target: trigger, clientX: 90, clientY: 112 };
    app.handleAppClick(event);

    await vi.waitFor(() => expect(app.querySelector("[data-folder-popover]").hidden).toBe(false));
    expect(app.dataset.route).toBe("workspace");
    expect(app.querySelector('[data-view="picker"]')).toBeNull();
    expect(globalThis.fetch).toHaveBeenCalledWith(
      expect.stringContaining("purpose=workspace"),
      expect.any(Object),
    );
    recent.querySelector(".recent-row").click();

    expect(app.openWorkspace).toHaveBeenCalledWith("w1");
  });

  it("shows the empty workspace when the active workspace becomes forbidden", async () => {
    globalThis.PI_WEB_API_BASE = "http://backend.test";
    const app = await connectPiApp();
    app.sendBrowserNotification = vi.fn();
    app.apiConnected = true;
    app.dataset.activeWorkspaceId = "w-forbidden";
    app.dataset.activeSessionId = "s-forbidden";
    const emptyMain = app.querySelector("[data-main='empty']");
    app.browseFolder = vi.fn();
    globalThis.fetch = vi.fn(async () => ({
      ok: false,
      status: 403,
      statusText: "Forbidden",
      json: async () => ({ error: "outside allowed roots" }),
    }));

    await expect(app.refreshWorkspaces()).resolves.toBeUndefined();

    expect(app.dataset.route).toBe("workspace");
    expect(emptyMain.hidden).toBe(false);
    expect(app.querySelector('[data-view="picker"]')).toBeNull();
    expect(app.querySelector("[data-folder-popover]").hidden).toBe(true);
    expect(app.querySelector("[data-action='route-picker']").getAttribute("aria-expanded")).toBe("false");
    expect(app.dataset.activeWorkspaceId).toBe("");
    expect(app.dataset.activeSessionId).toBe("");
    expect(app.sendBrowserNotification).toHaveBeenCalledWith(
      "工作区不可访问",
      "outside allowed roots",
      undefined,
    );
    expect(globalThis.fetch).not.toHaveBeenCalledWith(expect.anything(), expect.objectContaining({ method: "DELETE" }));
  });

  it("keeps workspace rendering no-ops safe without optional containers", async () => {
    const app = await connectPiApp();
    const bare = document.createElement("div");
    bare.dataset.workspaceGroup = "bare";
    bare.innerHTML = `<button class="ws-row"></button>`;
    app.append(bare);
    expect(() => app.toggleWorkspace("bare")).not.toThrow();
    app.querySelector(".sidebar .sb-section").remove();
    expect(() => app.renderSidebarWorkspaces([{ id: "w1", name: "one", path: "/one", sessions: [] }])).not.toThrow();
    expect(() => app.renderRecentWorkspaces([{ id: "w1", name: "one", path: "/one", sessions: [] }])).not.toThrow();
  });

  it("renders recent workspaces and marks workspaces that contain a waiting session", async () => {
    const app = await connectPiApp();
    const count = document.createElement("span");
    const recent = document.createElement("div");
    count.dataset.workspaceCount = "";
    recent.dataset.recentWorkspaces = "";
    recent.append(document.createElement("button"));
    recent.firstElementChild.className = "recent-row";
    app.prepend(count, recent);
    app.dataset.activeSessionId = "s2";

    app.renderWorkspaces([
      { id: "w1", name: "one", path: "/one", sessionCount: 1, lastUsed: "<old>", sessions: [{ id: "s1", title: "first" }] },
      { id: "w2", name: "two", path: "/two", sessionCount: 1, live: true, sessions: [{ id: "s2", title: "second", live: true }] },
      { id: "w3", name: "three", path: "/three", sessionCount: 0, sessions: [] },
      { id: "w4", name: "four", path: "/four", sessionCount: 0, sessions: [] },
      { id: "w5", name: "five", path: "/five", sessionCount: 0, sessions: [] },
    ]);

    const row = app.querySelector("[data-workspace='w2'].ws-row");
    expect(count.textContent).toBe("5 个工作区");
    expect(recent.querySelectorAll(".recent-row")).toHaveLength(4);
    expect(recent.querySelector(".recent-row").getAttribute("aria-label")).toBe("打开 one");
    expect(recent.querySelector(".ws-stat").innerHTML).toContain("&lt;old&gt;");
    expect(recent.querySelectorAll(".ws-stat")[1].textContent).toContain("● 在线");
    expect(row.classList.contains("has-active-session")).toBe(true);
    expect(row.querySelector(".ws-name .dot").classList.contains("live")).toBe(true);
    expect(app.querySelector("[data-session='s2']").classList.contains("active")).toBe(true);
    expect(app.querySelector("[data-session='s2']").getAttribute("aria-current")).toBe("true");
  });
});
