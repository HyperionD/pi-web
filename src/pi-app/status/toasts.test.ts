// @ts-nocheck
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanupPiAppFixture, connectPiApp, installPiAppFixture } from "../test-helper";

const fallbackChoiceJson = [
  "```json",
  JSON.stringify({
    type: "piweb_choice",
    id: "runtime",
    question: "Runtime?",
    options: [{ label: "Go", value: "go" }],
    allowCustom: false,
  }),
  "```",
].join("\n");

function bootstrapOkResponse(url) {
  const body = String(url).endsWith("/app-config")
    ? { config: { version: 1, brand: "pi.dev / web" }, path: "/config" }
    : { workspaces: [] };
  return { ok: true, status: 200, statusText: "OK", json: async () => body };
}

function setActiveNotificationContext(app) {
  app.dataset.activeWorkspaceId = "w1";
  app.dataset.activeSessionId = "s1";
  const workspace = document.createElement("span");
  const session = document.createElement("span");
  workspace.dataset.activeWorkspace = "";
  session.dataset.activeSessionTitle = "";
  workspace.textContent = "demo";
  session.textContent = "chat";
  app.append(workspace, session);
  app.writeLastSessionPrompt?.("s1", "explain the failure and suggest a fix");
}

function installNotificationEnvironment(app, permission = "granted") {
  const notifications = [];
  const environment = {
    getPermission: () => permission,
    requestPermission: vi.fn(async () => permission),
    createNotification: vi.fn((title, options) => {
      const notification = { title, options, onclick: null, close: vi.fn() };
      notifications.push(notification);
      return notification;
    }),
  };
  app.browserNotificationEnvironment = environment;
  return { environment, notifications };
}

async function flushNotifications() {
  await Promise.resolve();
  await Promise.resolve();
}

describe("pi-app browser notifications", () => {
  beforeEach(() => {
    installPiAppFixture();
    globalThis.PI_WEB_API_BASE = "http://backend.test";
    globalThis.fetch = vi.fn(async (url) => bootstrapOkResponse(url));
  });
  afterEach(cleanupPiAppFixture);

  it("shows a system notification when a running response completes", async () => {
    const app = await connectPiApp();
    const { notifications } = installNotificationEnvironment(app);
    setActiveNotificationContext(app);

    app.applyEvent({ type: "session.status", payload: { status: "running" } });
    app.applyEvent({ type: "session.status", payload: { status: "idle" } });
    await flushNotifications();

    expect(notifications).toHaveLength(1);
    expect(notifications[0].title).toBe("回答完成");
    expect(notifications[0].options.body).toContain("demo · chat");
    expect(notifications[0].options.body).toContain("explain the failure and suggest a fix");
    expect(document.querySelector(".notyf")).toBeNull();
  });

  it("shows a failure notification and suppresses completion after a response error", async () => {
    const app = await connectPiApp();
    const { notifications } = installNotificationEnvironment(app);
    setActiveNotificationContext(app);

    app.applyEvent({ type: "session.status", payload: { status: "running" } });
    app.applyEvent({ type: "error", payload: { error: "boom" } });
    app.applyEvent({ type: "session.status", payload: { status: "idle" } });
    await flushNotifications();

    expect(notifications).toHaveLength(1);
    expect(notifications[0].title).toBe("回答失败");
    expect(notifications[0].options.body).toContain("explain the failure and suggest a fix");
    expect(app.querySelector(".term-inner .msg.error .body").textContent).toBe("boom");
  });

  it("shows a feedback notification instead of completion for a choice response", async () => {
    const app = await connectPiApp();
    const { notifications } = installNotificationEnvironment(app);
    setActiveNotificationContext(app);

    app.applyEvent({ type: "session.status", payload: { status: "running" } });
    app.applyEvent({ type: "session.message", payload: { kind: "pi", text: fallbackChoiceJson } });
    app.applyEvent({ type: "session.status", payload: { status: "idle" } });
    await flushNotifications();

    expect(notifications).toHaveLength(1);
    expect(notifications[0].title).toBe("需要反馈");
  });

  it("allows the next response to show completion after feedback is submitted", async () => {
    const app = await connectPiApp();
    const { notifications } = installNotificationEnvironment(app);
    setActiveNotificationContext(app);

    app.applyEvent({ type: "session.status", payload: { status: "running" } });
    app.applyEvent({ type: "session.message", payload: { kind: "pi", text: fallbackChoiceJson } });
    app.applyEvent({ type: "session.status", payload: { status: "idle" } });
    app.applyEvent({ type: "session.status", payload: { status: "running" } });
    app.applyEvent({ type: "session.message", payload: { kind: "pi", text: "follow-up answer" } });
    app.applyEvent({ type: "session.status", payload: { status: "idle" } });
    await flushNotifications();

    expect(notifications.map(({ title }) => title)).toEqual(["需要反馈", "回答完成"]);
  });

  it("does not show completion before a delayed choice message is received", async () => {
    const app = await connectPiApp();
    const { notifications } = installNotificationEnvironment(app);
    setActiveNotificationContext(app);

    app.applyEvent({ type: "session.status", payload: { status: "running" } });
    app.applyEvent({ type: "session.delta", payload: { kind: "pi", delta: fallbackChoiceJson } });
    app.applyEvent({ type: "session.status", payload: { status: "idle" } });
    app.applyEvent({ type: "session.message", payload: { kind: "pi", text: fallbackChoiceJson } });
    await flushNotifications();

    expect(notifications).toHaveLength(1);
    expect(notifications[0].title).toBe("需要反馈");
  });

  it("shows a connection notification instead of a generic failure", async () => {
    const app = await connectPiApp();
    const { notifications } = installNotificationEnvironment(app);
    setActiveNotificationContext(app);
    app.setConnection = vi.fn();

    app.applyEvent({ type: "session.status", payload: { status: "running" } });
    app.notifyResponseFailure("TypeError: Failed to fetch");
    app.applyEvent({ type: "session.status", payload: { status: "idle" } });
    await flushNotifications();

    expect(notifications).toHaveLength(1);
    expect(notifications[0].title).toBe("后端连接已断开");
    expect(app.setConnection).toHaveBeenCalledWith("err");
  });

  it("shows an auth warning notification for credential failures", async () => {
    const app = await connectPiApp();
    const { notifications } = installNotificationEnvironment(app);
    setActiveNotificationContext(app);

    app.applyEvent({ type: "session.status", payload: { status: "running" } });
    app.notifyResponseFailure({ message: "No API key found for provider github-copilot" });
    app.applyEvent({ type: "session.status", payload: { status: "idle" } });
    await flushNotifications();

    expect(notifications).toHaveLength(1);
    expect(notifications[0].title).toBe("身份验证警告");
    expect(notifications[0].options.body).toContain("请在设置中重新登录");
  });

  it("keeps the session target on an auth warning from a background session", async () => {
    const app = await connectPiApp();
    app.sendBrowserNotification = vi.fn();

    app.notifyRuntimeWarning("OAuth token expired", { sessionId: "background-session" });

    expect(app.sendBrowserNotification).toHaveBeenCalledWith(
      "身份验证警告",
      expect.stringContaining("OAuth token expired"),
      "background-session",
    );
  });

  it("uses the active session as the default auth warning target", async () => {
    const app = await connectPiApp();
    app.sendBrowserNotification = vi.fn();
    setActiveNotificationContext(app);

    app.notifyRuntimeWarning("OAuth token expired");

    expect(app.sendBrowserNotification).toHaveBeenCalledWith(
      "身份验证警告",
      expect.stringContaining("OAuth token expired"),
      "s1",
    );
  });

  it("shows a system notification when the durable event stream disconnects", async () => {
    const sources = [];
    class FakeEventSource {
      constructor() {
        this.listeners = {};
        this.close = vi.fn();
        sources.push(this);
      }
      addEventListener(type, callback) { this.listeners[type] = callback; }
    }
    vi.stubGlobal("EventSource", FakeEventSource);
    const app = await connectPiApp();
    app.sendBrowserNotification = vi.fn();

    app.connectDurableEvents("s1", "0");
    sources[0].listeners.error();

    expect(app.sendBrowserNotification).toHaveBeenCalledWith(
      "后端连接已断开",
      expect.stringContaining("事件流连接已断开"),
      "s1",
    );
  });

  it("shows a system notification when a background event stream disconnects", async () => {
    const sources = [];
    class FakeEventSource {
      constructor() {
        this.listeners = {};
        this.close = vi.fn();
        sources.push(this);
      }
      addEventListener(type, callback) { this.listeners[type] = callback; }
    }
    vi.stubGlobal("EventSource", FakeEventSource);
    const app = await connectPiApp();
    app.sendBrowserNotification = vi.fn();
    app.apiConnected = true;
    app.dataset.activeSessionId = "active-session";
    app.dataset.activeWorkspaceId = "active-workspace";
    globalThis.fetch = vi.fn(async (url) => String(url).includes("/state")
      ? { ok: true, status: 200, json: async () => ({ seq: "1", status: "running" }) }
      : bootstrapOkResponse(url));
    app.renderWorkspaces([
      { id: "active-workspace", name: "active", sessionCount: 1, sessions: [{ id: "active-session", title: "now" }] },
      { id: "other-workspace", name: "other", sessionCount: 1, sessions: [{ id: "background-session", title: "background", active: true }] },
    ]);

    await vi.waitFor(() => expect(sources).toHaveLength(1));
    sources[0].listeners.error();

    expect(app.sendBrowserNotification).toHaveBeenCalledWith(
      "后端连接已断开",
      expect.stringContaining("事件流连接已断开"),
      "background-session",
    );
  });

  it("does not complete from an idle snapshot that still waits for feedback", async () => {
    const app = await connectPiApp();
    app.notifySessionCompleted = vi.fn();
    const row = document.createElement("div");
    row.dataset.session = "background-session";
    row.dataset.workspace = "other-workspace";
    const watch = {
      failed: false,
      wasRunning: true,
      stopped: false,
      row,
      client: { stop: vi.fn(), start: vi.fn() },
    };
    app.backgroundSessionWatches = new Map([["background-session", watch]]);
    globalThis.fetch = vi.fn(async () => ({
      ok: true,
      status: 200,
      json: async () => ({
        seq: "2",
        status: "idle",
        items: [{ kind: "pi", text: fallbackChoiceJson }],
      }),
    }));

    await app.startDurableBackgroundWatch(watch);

    expect(app.notifySessionCompleted).not.toHaveBeenCalled();
  });

  it("restarts a background stream when its state snapshot fails", async () => {
    const app = await connectPiApp();
    const row = document.createElement("div");
    row.dataset.session = "background-session";
    row.dataset.workspace = "other-workspace";
    const watch = {
      failed: false,
      wasRunning: true,
      stopped: false,
      row,
      client: { stop: vi.fn(), start: vi.fn() },
    };
    app.backgroundSessionWatches = new Map([["background-session", watch]]);
    globalThis.fetch = vi.fn(async () => { throw new Error("Failed to fetch"); });

    await app.startDurableBackgroundWatch(watch);

    expect(watch.client.start).toHaveBeenCalledOnce();
  });

  it("does not send a completion notification after a background choice response", async () => {
    const app = await connectPiApp();
    app.notifyChoiceRequested = vi.fn();
    app.notifySessionCompleted = vi.fn();
    app.toastContextForSessionRow = vi.fn(() => ({ sessionId: "background-session" }));
    const row = document.createElement("div");
    row.dataset.session = "background-session";
    row.dataset.workspace = "other-workspace";
    const watch = {
      failed: false,
      wasRunning: true,
      stopped: false,
      row,
      client: { stop: vi.fn() },
    };
    app.backgroundSessionWatches = new Map([["background-session", watch]]);

    app.handleDurableBackgroundSessionEvent({
      type: "assistant.segment.committed",
      sessionId: "background-session",
      payload: { text: fallbackChoiceJson },
    }, watch);
    app.handleDurableBackgroundSessionEvent({
      type: "run.finished",
      sessionId: "background-session",
      payload: {},
    }, watch);

    expect(app.notifyChoiceRequested).toHaveBeenCalledOnce();
    expect(app.notifySessionCompleted).not.toHaveBeenCalled();
  });

  it("focuses a session when a system notification is clicked", async () => {
    const app = await connectPiApp();
    const { notifications } = installNotificationEnvironment(app);
    setActiveNotificationContext(app);
    app.activateToastSession = vi.fn();

    vi.spyOn(window, "focus").mockImplementation(() => {});
    app.showToast("success");
    await flushNotifications();
    notifications[0].onclick();

    expect(notifications[0].close).toHaveBeenCalledOnce();
    expect(app.activateToastSession).toHaveBeenCalledWith("s1");
  });

  it("updates the settings status after notification permission is requested", async () => {
    const app = await connectPiApp();
    const { environment } = installNotificationEnvironment(app, "granted");
    const status = document.createElement("span");
    status.dataset.notificationStatus = "";
    app.append(status);

    await app.enableBrowserNotifications();

    expect(environment.requestPermission).not.toHaveBeenCalled();
    expect(status.textContent).toBe("系统通知已启用。");
    expect(status.classList.contains("err")).toBe(false);
  });

  it("does not show a page notification when browser permission is denied", async () => {
    const app = await connectPiApp();
    const { notifications } = installNotificationEnvironment(app, "denied");
    setActiveNotificationContext(app);

    app.notifySessionCompleted();
    await flushNotifications();

    expect(notifications).toHaveLength(0);
    expect(document.querySelector(".notyf")).toBeNull();
  });

  it("notifies when an inactive watched session completes", async () => {
    const sources = [];
    class FakeEventSource {
      constructor(url) {
        this.url = url;
        this.listeners = {};
        this.close = vi.fn();
        sources.push(this);
      }
      addEventListener(type, callback) { this.listeners[type] = callback; }
    }
    vi.stubGlobal("EventSource", FakeEventSource);
    globalThis.fetch = vi.fn(async (url) => {
      if (String(url).includes("/state")) {
        return { ok: true, status: 200, json: async () => ({ seq: "1", status: "running" }) };
      }
      return bootstrapOkResponse(url);
    });
    const app = await connectPiApp();
    const { notifications } = installNotificationEnvironment(app);
    app.apiConnected = true;
    app.dataset.activeSessionId = "active-session";
    app.dataset.activeWorkspaceId = "active-workspace";

    app.writeLastSessionPrompt("background-session", "finish the background task");
    app.renderWorkspaces([
      { id: "active-workspace", name: "active", sessionCount: 1, sessions: [{ id: "active-session", title: "now" }] },
      { id: "other-workspace", name: "other", sessionCount: 1, sessions: [
        { id: "background-session", title: "background", active: true },
      ] },
    ]);

    await new Promise((resolve) => setTimeout(resolve, 0));
    expect(sources).toHaveLength(1);
    sources[0].listeners["session.event"]({
      data: JSON.stringify({
        seq: "2",
        type: "run.finished",
        sessionId: "background-session",
        payload: {},
      }),
    });
    await flushNotifications();

    expect(notifications[0].title).toBe("回答完成");
    expect(notifications[0].options.body).toContain("other · background");
    expect(notifications[0].options.body).toContain("finish the background task");
    expect(sources[0].close).toHaveBeenCalled();
    expect(app.querySelector("[data-session='background-session']").classList.contains("unread-completed")).toBe(true);
  });

  it("uses durable background streams instead of legacy events", async () => {
    const sources = [];
    class FakeEventSource {
      constructor(url) {
        this.url = url;
        this.listeners = {};
        this.close = vi.fn();
        sources.push(this);
      }
      addEventListener(type, callback) { this.listeners[type] = callback; }
      emit(type, payload) { this.listeners[type]?.({ data: JSON.stringify(payload) }); }
    }
    vi.stubGlobal("EventSource", FakeEventSource);
    const app = await connectPiApp();
    const { notifications } = installNotificationEnvironment(app);
    app.apiConnected = true;
    app.durableEvents = true;
    app.dataset.activeSessionId = "active-session";
    app.dataset.activeWorkspaceId = "active-workspace";
    globalThis.fetch = vi.fn(async (url) => String(url).includes("/state")
      ? { ok: true, status: 200, statusText: "OK", json: async () => ({ seq: "7", status: "running", projectionGeneration: "1", session: { id: "background-session" }, items: [] }) }
      : { ok: true, status: 200, statusText: "OK", json: async () => ({}) });

    app.renderWorkspaces([
      { id: "active-workspace", name: "active", sessionCount: 1, sessions: [{ id: "active-session", title: "now" }] },
      { id: "other-workspace", name: "other", sessionCount: 1, sessions: [{ id: "background-session", title: "background", active: true }] },
    ]);
    await vi.waitFor(() => expect(sources).toHaveLength(1));

    expect(sources[0].url).toContain("/events-v2?after=7");
    expect(globalThis.fetch.mock.calls.every(([url]) => !String(url).includes("/events"))).toBe(true);
    sources[0].emit("session.event", { seq: "8", sessionId: "background-session", type: "run.finished", payload: {} });
    await flushNotifications();
    expect(notifications).toHaveLength(1);
    expect(notifications[0].title).toBe("回答完成");
    expect(sources[0].close).toHaveBeenCalled();
  });

  it("drops unread completed sessions that are no longer visible", async () => {
    localStorage.setItem("piweb:unread-completed-sessions", JSON.stringify(["missing"]));
    const app = await connectPiApp();

    app.syncUnreadCompletedSessions();

    expect(localStorage.getItem("piweb:unread-completed-sessions")).toBe("[]");
  });

  it("restores unread completed session glow until the session is opened", async () => {
    localStorage.setItem("piweb:unread-completed-sessions", JSON.stringify(["s2"]));
    const app = await connectPiApp();
    app.apiConnected = true;
    app.dataset.activeWorkspaceId = "w1";
    app.dataset.activeSessionId = "s1";
    app.renderWorkspaces([
      { id: "w1", name: "demo", sessionCount: 2, sessions: [
        { id: "s1", title: "current" },
        { id: "s2", title: "done" },
      ] },
    ]);

    const row = app.querySelector("[data-session='s2']");
    expect(row.classList.contains("unread-completed")).toBe(true);

    app.loadSession = vi.fn();
    await app.pickSession(row);

    expect(row.classList.contains("unread-completed")).toBe(false);
    expect(localStorage.getItem("piweb:unread-completed-sessions")).toBe("[]");
  });
});
