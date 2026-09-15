// @ts-nocheck
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanupPiAppFixture, connectPiApp, installPiAppFixture } from "./test-helper";

describe("pi-app core events coverage", () => {
  beforeEach(() => {
    installPiAppFixture();
  });
  afterEach(cleanupPiAppFixture);

  it("connects and disconnects with missing optional DOM", async () => {
    document.body.innerHTML = `<pi-app></pi-app>`;
    const app = document.querySelector("pi-app");
    await customElements.whenDefined("pi-app");
    app.bootstrapAPI = vi.fn();
    app.uninstallFilePreviewUnloadGuard = vi.fn();
    app.connectedCallback();
    app.connectedCallback();
    Object.defineProperty(window, "innerHeight", { configurable: true, value: 0 });
    app.installViewportSizing();
    app.applyEvent({ type: "unknown" });
    app.disconnectedCallback();
    expect(app.uninstallFilePreviewUnloadGuard).toHaveBeenCalled();
    expect(app.bound).toBe(true);
  });

  it("exercises bound send and slash keyboard listeners", async () => {
    const app = await connectPiApp();
    Element.prototype.scrollIntoView = vi.fn();
    app.submitPrompt = vi.fn();
    app.compactActiveSession = vi.fn();
    app.pickSlash = vi.fn();
    app.slashPopover.hidden = false;
    app.sendButton.disabled = false;
    app.sendButton.click();
    app.querySelector("[data-action='compact-session']").disabled = false;
    app.querySelector("[data-action='compact-session']").click();
    app.prompt.dispatchEvent(new KeyboardEvent("keydown", { key: "Enter", ctrlKey: true }));
    app.prompt.dispatchEvent(new KeyboardEvent("keydown", { key: "Enter" }));
    expect(app.submitPrompt).toHaveBeenCalledTimes(1);
    expect(app.pickSlash).toHaveBeenCalledWith("/model");

    app.slashPopover.hidden = true;
    app.prompt.dispatchEvent(new KeyboardEvent("keydown", { key: "Enter", ctrlKey: true, shiftKey: true }));
    expect(app.submitPrompt).toHaveBeenCalledTimes(2);
  });







  it("suppresses completion notifications after user cancellation", async () => {
    const app = await connectPiApp();
    app.dataset.activeSessionId = "s1";
    app.running = true;
    app.notifySessionCompleted = vi.fn();
    app.markSessionCancellationPending("");
    app.markSessionCancellationPending("s1");

    app.setMode("idle");

    expect(app.running).toBe(false);
    expect(app.notifySessionCompleted).not.toHaveBeenCalled();
    expect(app.isSessionCancellationPending("s1")).toBe(false);

    app.markSessionCancellationPending("s1");
    app.setMode("idle");
    expect(app.isSessionCancellationPending("s1")).toBe(false);
  });

  it("keeps transient event stream errors from disabling the API", async () => {
    const app = await connectPiApp();
    const indicator = document.createElement("span");
    indicator.className = "statusbtn";
    app.append(indicator);
    app.apiConnected = false;
    app.eventSource = { readyState: 0, close: vi.fn() };

    app.confirmConnection();
    expect(app.apiConnected).toBe(true);
    expect(indicator.title).toBe("已连接");

    app.deferEventStreamError();
    expect(indicator.title).toBe("正在重新连接事件流");
    expect(indicator.style.color).toBe("var(--fg-3)");
    expect(app.apiConnected).toBe(true);

    app.confirmConnection();
    expect(indicator.title).toBe("已连接");

    globalThis.EventSource = { CLOSED: 2 };
    app.eventSource = { readyState: 2, close: vi.fn() };
    app.deferEventStreamError();
    expect(indicator.title).toBe("后端连接已断开");
  });

  it("marks the backend disconnected when the event stream stays down", async () => {
    let timeoutCallback;
    vi.spyOn(window, "setTimeout").mockImplementation((callback) => {
      timeoutCallback = callback;
      return 1;
    });
    const app = await connectPiApp();
    const indicator = document.createElement("span");
    indicator.className = "statusbtn";
    app.append(indicator);
    app.eventSource = { readyState: 0, close: vi.fn() };
    app.connectionErrorTimer = 7;
    vi.spyOn(window, "clearTimeout").mockImplementation(() => undefined);
    app.deferEventStreamError();
    timeoutCallback();
    expect(window.clearTimeout).toHaveBeenCalledWith(7);
    expect(indicator.title).toBe("后端连接已断开");

    app.eventSource = { readyState: 0, close: vi.fn() };
    app.deferEventStreamError();
    app.eventSource = { readyState: 0, close: vi.fn() };
    expect(() => timeoutCallback()).not.toThrow();

    app.setConnection("unexpected");
    expect(indicator.title).toBe("后端连接已断开");
  });

  it("connects events, checks stream identity, mode, connection, and quota labels", async () => {
    const app = await connectPiApp();
    const source = { close: vi.fn() };
    app.eventSource = source;
    let handlers;
    globalThis.EventSource = undefined;
    const api = await import("../lib/api");
    vi.spyOn(api, "sessionEvents").mockImplementation((sessionId, options) => {
      handlers = options;
      return { close: vi.fn(), sessionId };
    });
    app.dataset.activeSessionId = "s1";
    app.applyEvent = vi.fn();
    app.connectEvents("s1", { replay: true });
    handlers.onOpen();
    handlers.onError();
    handlers.onEvent({ type: "heartbeat" });
    expect(source.close).toHaveBeenCalled();
    expect(app.applyEvent).toHaveBeenCalledWith({ type: "heartbeat" });
    expect(app.isActiveEventStream(app.eventStreamId, "s1")).toBe(true);
    expect(app.isActiveEventStream(Symbol("x"), "s1")).toBe(false);
    expect(app.isCurrentSessionEvent({ sessionId: "s1" })).toBe(true);
    expect(app.isCurrentSessionEvent({ sessionId: "s2" })).toBe(false);
    app.dataset.activeSessionId = "";
    expect(app.isCurrentSessionEvent({ sessionId: "s2" })).toBe(true);

    app.stopButton = document.createElement("button");
    app.sendButton = document.createElement("button");
    app.finishRunningTools = vi.fn();
    app.syncLoadingMessage = vi.fn();
    app.loadRuntimeStatus = vi.fn();
    app.syncCurrentSessionRunState = vi.fn();
    app.setMode("thinking");
    app.setMode("cancelled");
    app.setMode("idle");
    expect(app.syncCurrentSessionRunState).toHaveBeenCalledWith(true);
    expect(app.syncCurrentSessionRunState).toHaveBeenCalledWith(false);
    expect(app.finishRunningTools).toHaveBeenCalled();

    expect(app.quotaLabel("5h", 120)).toContain("prompt-meta-battery-full");
    expect(app.quotaLabel("5h", 120)).toContain("(100%)");
    expect(app.quotaLabel("5h", 42)).toContain("prompt-meta-battery-medium");
    expect(app.quotaLabel("5h", -1)).toContain("prompt-meta-battery-low");
    expect(app.quotaLabel("5h", -1)).toContain("(0%)");
    expect(app.quotaLabel("5h", Number.NaN)).toBeUndefined();
    expect(app.promptMetaIcon("missing")).toBe("");
    vi.resetModules();
    await import("./index");
  });
});
