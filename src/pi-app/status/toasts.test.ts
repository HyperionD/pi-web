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

function setActiveToastContext(app) {
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

describe("pi-app toast notifications", () => {
  beforeEach(() => {
    installPiAppFixture();
    globalThis.PI_WEB_API_BASE = "http://backend.test";
    globalThis.fetch = vi.fn(async (url) => bootstrapOkResponse(url));
  });
  afterEach(cleanupPiAppFixture);

  it("shows and dismisses a green toast when a running response completes", async () => {
    const app = await connectPiApp();
    setActiveToastContext(app);

    app.applyEvent({ type: "session.status", payload: { status: "running" } });
    app.applyEvent({ type: "session.status", payload: { status: "idle" } });

    const toast = document.querySelector(".session-toast.success");
    expect(toast.textContent).toContain("回答完成");
    expect(toast.querySelector(".toast-workspace").textContent).toBe("demo");
    expect(toast.querySelector(".toast-session").textContent).toBe("chat");
    expect(toast.querySelector(".toast-prompt").textContent).toBe("explain the failure and suggest a fix");
  });

  it("shows a red failure toast and suppresses completion after a response error", async () => {
    const app = await connectPiApp();
    setActiveToastContext(app);

    app.applyEvent({ type: "session.status", payload: { status: "running" } });
    app.applyEvent({ type: "error", payload: { error: "boom" } });
    app.applyEvent({ type: "session.status", payload: { status: "idle" } });

    expect(document.querySelector(".session-toast.error").textContent).toContain("回答失败");
    expect(document.querySelector(".session-toast.error").querySelector(".toast-prompt").textContent).toBe("explain the failure and suggest a fix");
    expect(app.querySelector(".term-inner .msg.error .body").textContent).toBe("boom");
    expect(document.querySelector(".session-toast.error").querySelector(".toast-workspace").textContent).toBe("demo");
    expect(document.querySelector(".session-toast.success")).toBeNull();
  });

  it("keeps the feedback toast when a choice response becomes idle", async () => {
    const app = await connectPiApp();
    setActiveToastContext(app);

    app.applyEvent({ type: "session.status", payload: { status: "running" } });
    app.applyEvent({ type: "session.message", payload: { kind: "pi", text: fallbackChoiceJson } });
    app.applyEvent({ type: "session.status", payload: { status: "idle" } });

    expect(document.querySelector(".session-toast.choice").textContent).toContain("需要反馈");
    expect(document.querySelector(".session-toast.success")).toBeNull();
  });

  it("allows the next response to show completion after feedback is submitted", async () => {
    const app = await connectPiApp();
    setActiveToastContext(app);

    app.applyEvent({ type: "session.status", payload: { status: "running" } });
    app.applyEvent({ type: "session.message", payload: { kind: "pi", text: fallbackChoiceJson } });
    app.applyEvent({ type: "session.status", payload: { status: "idle" } });
    app.applyEvent({ type: "session.status", payload: { status: "running" } });
    app.applyEvent({ type: "session.message", payload: { kind: "pi", text: "follow-up answer" } });
    app.applyEvent({ type: "session.status", payload: { status: "idle" } });

    expect(document.querySelector(".session-toast.success").textContent).toContain("回答完成");
  });

  it("does not show completion before a delayed choice message is received", async () => {
    const app = await connectPiApp();
    setActiveToastContext(app);

    app.applyEvent({ type: "session.status", payload: { status: "running" } });
    app.applyEvent({ type: "session.delta", payload: { kind: "pi", delta: fallbackChoiceJson } });
    app.applyEvent({ type: "session.status", payload: { status: "idle" } });
    app.applyEvent({ type: "session.message", payload: { kind: "pi", text: fallbackChoiceJson } });

    expect(document.querySelector(".session-toast.choice").textContent).toContain("需要反馈");
    expect(document.querySelectorAll(".session-toast.choice")).toHaveLength(1);
    expect(document.querySelector(".session-toast.success")).toBeNull();
  });

  it("shows a backend connection toast instead of response failure for disconnects", async () => {
    const app = await connectPiApp();
    setActiveToastContext(app);
    app.setConnection = vi.fn();

    app.applyEvent({ type: "session.status", payload: { status: "running" } });
    app.notifyResponseFailure("TypeError: Failed to fetch");
    app.applyEvent({ type: "session.status", payload: { status: "idle" } });

    expect(document.querySelector(".session-toast.connection").textContent).toContain("后端连接已断开");
    expect(document.querySelector(".session-toast.error")).toBeNull();
    expect(document.querySelector(".session-toast.success")).toBeNull();
    expect(app.setConnection).toHaveBeenCalledWith("err");
  });

  it("shows auth warnings instead of generic response errors for credential failures", async () => {
    const app = await connectPiApp();
    setActiveToastContext(app);

    app.applyEvent({ type: "session.status", payload: { status: "running" } });
    app.notifyResponseFailure({ message: "No API key found for provider github-copilot" });
    app.applyEvent({ type: "session.status", payload: { status: "idle" } });

    expect(document.querySelector(".session-toast.warning").textContent).toContain("身份验证警告");
    expect(document.querySelector(".session-toast.error")).toBeNull();
    expect(document.querySelector(".session-toast.success")).toBeNull();
  });

  it("ignores toast body activation when the dismiss button is clicked", async () => {
    const app = await connectPiApp();
    setActiveToastContext(app);
    app.activateToastSession = vi.fn();
    app.dismissToast = vi.fn();

    const notification = app.showToast("success");
    notification.triggerEvent("click", { target: { closest: (selector) => selector === ".notyf__dismiss" } });

    expect(app.activateToastSession).not.toHaveBeenCalled();
    expect(app.dismissToast).not.toHaveBeenCalled();
  });

  it("shows a blue choice toast and can dismiss all visible toasts", async () => {
    const app = await connectPiApp();
    setActiveToastContext(app);
    app.renderMessages([]);

    app.appendMessage({ kind: "pi", text: fallbackChoiceJson });
    app.notifySessionCompleted();

    expect(document.querySelector(".session-toast.choice").textContent).toContain("需要反馈");
    expect(document.querySelector(".toast-dismiss-all").hidden).toBe(false);
    const visibleToasts = [...document.querySelectorAll(".notyf__toast:not(.notyf__toast--disappear)")];
    expect(visibleToasts.map((toast) => toast.style.getPropertyValue("--toast-stack-index"))).toEqual(["0", "1"]);
    expect(visibleToasts.map((toast) => toast.style.zIndex)).toEqual(["1", "2"]);
    document.querySelector(".toast-dismiss-all").click();
    expect(document.querySelectorAll(".notyf__toast--disappear")).toHaveLength(2);
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

    const toast = document.querySelector(".session-toast.success");
    const toastText = toast.textContent;
    expect(toastText).toContain("回答完成");
    expect(toast.querySelector(".toast-workspace").textContent).toBe("other");
    expect(toast.querySelector(".toast-session").textContent).toBe("background");
    expect(toast.querySelector(".toast-prompt").textContent).toBe("finish the background task");
    expect(sources[0].close).toHaveBeenCalled();
    expect(app.querySelector("[data-session='background-session']").classList.contains("unread-completed")).toBe(true);

    app.loadSession = vi.fn();
    toast.click();
    expect(app.dataset.activeSessionId).toBe("background-session");
    expect(app.loadSession).toHaveBeenCalledWith("background-session");
    expect(app.querySelector("[data-session='background-session']").classList.contains("unread-completed")).toBe(false);
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
    app.apiConnected = true;
    app.durableEvents = true;
    app.dataset.activeSessionId = "active-session";
    app.dataset.activeWorkspaceId = "active-workspace";
    app.notifySessionCompleted = vi.fn();
    globalThis.fetch = vi.fn(async (url) => String(url).includes("/state")
      ? { ok: true, status: 200, statusText: "OK", json: async () => ({ seq: "7", status: "running", projectionGeneration: "1", session: { id: "background-session" }, items: [] }) }
      : { ok: true, status: 200, statusText: "OK", json: async () => ({}) });

    app.renderWorkspaces([
      { id: "active-workspace", name: "active", sessionCount: 1, sessions: [{ id: "active-session", title: "now" }] },
      { id: "other-workspace", name: "other", sessionCount: 1, sessions: [{ id: "background-session", title: "background", active: true }] },
    ]);
    expect(app.backgroundWatchRows()).toHaveProperty("size", 1);
    await vi.waitFor(() => expect(sources).toHaveLength(1));

    expect(sources).toHaveLength(1);
    expect(sources[0].url).toContain("/events-v2?after=7");
    expect(globalThis.fetch.mock.calls.every(([url]) => !String(url).includes("/events"))).toBe(true);
    sources[0].emit("session.event", { seq: "8", sessionId: "background-session", type: "run.finished", payload: {} });
    expect(app.notifySessionCompleted).toHaveBeenCalled();
    expect(sources[0].close).toHaveBeenCalled();
  });

  it("drops unread completed sessions that are no longer visible", async () => {
    localStorage.setItem("piweb:unread-completed-sessions", JSON.stringify(["missing"]));
    const app = await connectPiApp();

    app.syncUnreadCompletedSessions();

    expect(localStorage.getItem("piweb:unread-completed-sessions")).toBe("[]");
  });

  it("restores unread completed session glow from local storage until the session is opened", async () => {
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
