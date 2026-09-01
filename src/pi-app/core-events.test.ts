// @ts-nocheck
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanupPiAppFixture, connectPiApp, installPiAppFixture } from "./test-helper";

const appTemplatePath = "../App.astro";
const appTemplate = readFileSync(fileURLToPath(new URL(appTemplatePath, import.meta.url)), "utf8");

describe("pi-app core events", () => {
  beforeEach(installPiAppFixture);
  afterEach(cleanupPiAppFixture);

  it("starts timers idempotently and cleans up resources on disconnect", async () => {
    vi.useFakeTimers();
    const app = await connectPiApp();
    app.style.setProperty = vi.fn();
    Object.defineProperty(window, "innerHeight", { configurable: true, value: 0 });
    Object.defineProperty(window, "visualViewport", {
      configurable: true,
      value: { height: 0, addEventListener: vi.fn(), removeEventListener: vi.fn() },
    });
    expect(() => app.installViewportSizing()).not.toThrow();
    if (app.runtimeStatusTimer) clearInterval(app.runtimeStatusTimer);
    app.runtimeStatusTimer = undefined;
    app.loadRuntimeStatus = vi.fn();
    app.startRuntimeStatusPolling();
    const runtimeTimer = app.runtimeStatusTimer;
    app.startRuntimeStatusPolling();
    expect(app.runtimeStatusTimer).toBe(runtimeTimer);
    vi.advanceTimersByTime(15000);
    expect(app.loadRuntimeStatus).toHaveBeenCalled();

    app.startSpinners();
    const spinnerTimer = app.spinnerTimer;
    app.startSpinners();
    expect(app.spinnerTimer).toBe(spinnerTimer);
    const spinner = document.createElement("span");
    spinner.className = "spinner";
    app.append(spinner);
    app.tickSpinners();
    expect(spinner.dataset.frame).toBe("1");

    const backgroundSource = { close: vi.fn() };
    app.eventSource = { close: vi.fn() };
    app.backgroundSessionWatches = new Map([["s2", { source: backgroundSource }]]);
    app.streamingRenderFrame = 1;
    app.scrollFrame = 2;
    app.uninstallViewportSizing = vi.fn();
    vi.spyOn(window, "cancelAnimationFrame").mockImplementation(() => undefined);
    app.disconnectedCallback();
    expect(app.eventSource.close).toHaveBeenCalled();
    expect(backgroundSource.close).toHaveBeenCalled();
    expect(app.uninstallViewportSizing).toHaveBeenCalled();
    expect(app.streamingRenderFrame).toBeUndefined();
    expect(app.scrollFrame).toBeUndefined();
    app.eventSource = null;
    app.spinnerTimer = undefined;
    app.runtimeStatusTimer = undefined;
    app.streamingRenderFrame = undefined;
    app.scrollFrame = undefined;
    expect(() => app.disconnectedCallback()).not.toThrow();
    vi.useRealTimers();
  });

  it("persists the sidebar width only after a resize gesture ends", async () => {
    const app = await connectPiApp();
    app.dataset.sidebarWidth = "280";
    app.applyGrid = vi.fn();
    localStorage.setItem.mockClear();

    app.startResize(new PointerEvent("pointerdown", { clientX: 100 }));
    window.dispatchEvent(new PointerEvent("pointermove", { clientX: 180 }));
    expect(app.dataset.sidebarWidth).toBe("360");
    expect(localStorage.setItem).not.toHaveBeenCalledWith("pi.sb.width", "360");

    window.dispatchEvent(new PointerEvent("pointerup", { clientX: 180 }));
    expect(localStorage.setItem).toHaveBeenCalledWith("pi.sb.width", "360");
    window.dispatchEvent(new PointerEvent("pointermove", { clientX: 220 }));
    expect(app.dataset.sidebarWidth).toBe("360");
  });

  it("resizes the inspector from its left edge and persists after release", async () => {
    const app = await connectPiApp();
    app.dataset.inspectorWidth = "560";
    app.applyGrid = vi.fn();
    localStorage.setItem.mockClear();

    app.startInspectorResize(new PointerEvent("pointerdown", { clientX: 900 }));
    window.dispatchEvent(new PointerEvent("pointermove", { clientX: 800 }));
    expect(app.dataset.inspectorWidth).toBe("660");
    expect(localStorage.setItem).not.toHaveBeenCalledWith("pi.inspector.width", "660");

    window.dispatchEvent(new PointerEvent("pointerup", { clientX: 800 }));
    expect(localStorage.setItem).toHaveBeenCalledWith("pi.inspector.width", "660");
    window.dispatchEvent(new PointerEvent("pointermove", { clientX: 700 }));
    expect(app.dataset.inspectorWidth).toBe("660");
  });

  it("clears streaming lifecycle state through the real disconnect path", async () => {
    vi.spyOn(window, "requestAnimationFrame").mockImplementation(() => 0);
    const cancelAnimationFrame = vi.spyOn(window, "cancelAnimationFrame").mockImplementation(() => undefined);
    const app = await connectPiApp();
    app.appendDelta({ kind: "pi", delta: "pending" });

    expect(app.querySelector(".streaming-tail .msg.streaming")).not.toBeNull();
    expect(app.pendingStreamingRow).not.toBeUndefined();
    app.disconnectedCallback();

    expect(cancelAnimationFrame).toHaveBeenCalledWith(0);
    expect(app.querySelector(".streaming-tail").childElementCount).toBe(0);
    expect(app.pendingStreamingRow).toBeUndefined();
    expect(app.streamingRenderFrame).toBeUndefined();
    expect(app.scrollFrame).toBeUndefined();
  });

  it("binds DOM and window events to current methods", async () => {
    const app = await connectPiApp();
    expect(appTemplate).not.toContain("data-path-form");
    expect(appTemplate).not.toContain("data-clone-form");
    expect(appTemplate).not.toContain("gitUrl");
    expect(appTemplate).not.toContain("克隆并打开");
    const calls = [];
    ["submitShellCommand", "saveSettingsForm", "fillSettingsForm", "fillModelControls", "syncCustomSettingInput", "setModelControlsLoading",
      "submitPrompt", "cancelActiveSession", "handlePromptPaste", "navigateList", "pickSlash", "addFiles", "startResize", "startInspectorResize", "openFolderPopover", "closeFolderPopover",
      "shortcut", "closeSessionMenus"].forEach((name) => { app[name] = (...args) => calls.push([name, ...args]); });
    app.slashPopover.hidden = false;
    app.slashPopover.removeAttribute("hidden");
    app.submitWorkspacePath = vi.fn();
    app.submitCloneWorkspace = vi.fn();
    const pathForm = document.createElement("form");
    pathForm.dataset.pathForm = "";
    const cloneForm = document.createElement("form");
    cloneForm.dataset.cloneForm = "";
    const folderTrigger = document.createElement("button");
    folderTrigger.dataset.action = "folder-open";
    const folderPopover = document.createElement("div");
    folderPopover.dataset.folderPopover = "";
    folderPopover.hidden = true;
    const folderClose = document.createElement("button");
    folderClose.dataset.action = "folder-close";
    folderPopover.append(folderClose);
    app.append(folderTrigger, folderPopover);
    const shellForm = document.createElement("form");
    const resizer = document.createElement("div");
    const inspectorResizer = document.createElement("div");
    const gitInspectorResizer = document.createElement("div");
    shellForm.dataset.shellForm = "";
    resizer.className = "sb-resizer";
    inspectorResizer.className = "fp-resizer";
    gitInspectorResizer.className = "fp-resizer";
    app.append(pathForm, cloneForm, shellForm, resizer, inspectorResizer, gitInspectorResizer);
    app.bindDomEvents();

    folderTrigger.click();
    folderClose.click();
    pathForm.dispatchEvent(new Event("submit"));
    cloneForm.dispatchEvent(new Event("submit"));
    shellForm.dispatchEvent(new Event("submit"));
    expect(app.submitWorkspacePath).not.toHaveBeenCalled();
    expect(app.submitCloneWorkspace).not.toHaveBeenCalled();
    app.querySelector("[data-settings-form]").dispatchEvent(new Event("submit"));
    app.querySelector("[data-settings-scope]").dispatchEvent(new Event("change"));
    app.querySelector("[data-setting='defaultProvider']").dispatchEvent(new Event("change"));
    app.querySelector("[data-setting='defaultModel']").dispatchEvent(new Event("change"));
    app.sendButton.click();
    app.stopButton.click();
    app.prompt.dispatchEvent(new Event("input"));
    app.prompt.dispatchEvent(new Event("paste"));
    app.prompt.dispatchEvent(new KeyboardEvent("keydown", { key: "Enter", metaKey: true, bubbles: true }));
    app.slashPopover.hidden = false;
    app.slashPopover.removeAttribute("hidden");
    app.prompt.dispatchEvent(new KeyboardEvent("keydown", { key: "ArrowDown", bubbles: true }));
    app.attachButton.click();
    app.fileInput.dispatchEvent(new Event("change"));
    resizer.dispatchEvent(new PointerEvent("pointerdown"));
    inspectorResizer.dispatchEvent(new PointerEvent("pointerdown"));
    gitInspectorResizer.dispatchEvent(new PointerEvent("pointerdown"));
    window.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape" }));
    window.dispatchEvent(new Event("click"));
    const tweaks = document.createElement("div");
    tweaks.dataset.tweaks = "";
    tweaks.hidden = true;
    app.append(tweaks);
    window.dispatchEvent(new MessageEvent("message", { data: { type: "__activate_edit_mode" } }));
    expect(tweaks.hidden).toBe(false);
    window.dispatchEvent(new MessageEvent("message", { data: { type: "__deactivate_edit_mode" } }));
    expect(tweaks.hidden).toBe(true);
    expect(calls.map(([name]) => name)).toContain("navigateList");
    expect(calls).toContainEqual(["openFolderPopover"]);
    expect(calls).toContainEqual(["closeFolderPopover"]);
  });

  it("closes the folder picker on Escape or outside click and restores trigger focus", async () => {
    const app = await connectPiApp();
    app.apiConnected = true;
    const trigger = document.createElement("button");
    trigger.dataset.action = "folder-open";
    const popover = document.createElement("div");
    popover.dataset.folderPopover = "";
    popover.hidden = true;
    popover.innerHTML = '<button data-action="folder-close"></button><span data-folder-path></span><div data-folder-list></div><button data-action="folder-open-current"></button>';
    app.append(trigger, popover);
    globalThis.fetch = vi.fn(async () => ({
      ok: true,
      status: 200,
      statusText: "OK",
      json: async () => ({ path: "/allowed", parent: "/", folders: [] }),
    }));

    trigger.click();
    await vi.waitFor(() => expect(popover.hidden).toBe(false));
    const closeButton = popover.querySelector("[data-action='folder-close']");
    closeButton.focus();
    expect(document.activeElement).toBe(closeButton);
    window.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape" }));
    expect(popover.hidden).toBe(true);
    expect(document.activeElement).toBe(trigger);

    trigger.click();
    await vi.waitFor(() => expect(popover.hidden).toBe(false));
    closeButton.focus();
    expect(document.activeElement).toBe(closeButton);
    const outside = document.createElement("button");
    document.body.append(outside);
    outside.click();
    expect(popover.hidden).toBe(true);
    expect(document.activeElement).toBe(trigger);
  });

  it("keeps settings controls usable when model discovery fails", async () => {
    const maliciousStatus = '<img src=x onerror="alert(1)">model discovery failed';
    globalThis.fetch = vi.fn(async (url) => {
      const path = String(url);
      if (path.includes("/api/app-config")) return { ok: true, json: async () => ({ config: { version: 1, brand: "pi.dev / web" }, path: "p" }) };
      if (path.includes("/settings")) return { ok: true, json: async () => ({ settings: { project: {}, global: {}, effective: { defaultProvider: "zai", defaultModel: "gpt-5.5" }, paths: { project: "p", global: "g" } } }) };
      if (path.includes("/models")) return { ok: false, status: 502, statusText: "Bad Gateway", json: async () => ({ error: maliciousStatus }) };
      if (path.includes("/auth/oauth/providers")) return { ok: true, json: async () => ({ providers: [] }) };
      if (path.includes("/auth/providers")) return { ok: true, json: async () => ({ providers: [] }) };
      return { ok: true, json: async () => ({}) };
    });
    const app = await connectPiApp();
    app.dataset.activeWorkspaceId = "w1";
    app.apiConnected = true;
    await app.openSettingsModal();
    const provider = app.querySelector("[data-setting='defaultProvider']");
    const model = app.querySelector("[data-setting='defaultModel']");
    expect(provider.disabled).toBe(false);
    expect(model.disabled).toBe(false);
    expect([...provider.options].map((option) => option.value)).toContain("zai");
    expect([...model.options].map((option) => option.value)).toContain("gpt-5.5");
    const status = app.querySelector("[data-settings-status]");
    expect(status.textContent).toBe(maliciousStatus);
    expect(status.querySelector("*")).toBeNull();
  });

  it("sends on plain Enter while preserving Shift+Enter and slash choices", async () => {
    const app = await connectPiApp();
    app.submitPrompt = vi.fn();
    app.navigateList = vi.fn();

    const enter = new KeyboardEvent("keydown", { key: "Enter", bubbles: true, cancelable: true });
    app.prompt.dispatchEvent(enter);
    expect(enter.defaultPrevented).toBe(true);
    expect(app.submitPrompt).toHaveBeenCalledTimes(1);

    const shiftEnter = new KeyboardEvent("keydown", { key: "Enter", shiftKey: true, bubbles: true, cancelable: true });
    app.prompt.dispatchEvent(shiftEnter);
    expect(shiftEnter.defaultPrevented).toBe(false);
    expect(app.submitPrompt).toHaveBeenCalledTimes(1);

    app.slashPopover.hidden = false;
    const slashEnter = new KeyboardEvent("keydown", { key: "Enter", bubbles: true, cancelable: true });
    app.prompt.dispatchEvent(slashEnter);
    expect(app.navigateList).toHaveBeenCalledWith(slashEnter, ".slash-item", expect.any(Function));
    expect(app.submitPrompt).toHaveBeenCalledTimes(1);
  });

  it("connects event streams and applies every event branch", async () => {
    const listeners = {};
    class FakeEventSource {
      constructor(url) { this.url = url; }
      addEventListener(type, callback) { listeners[type] = callback; }
      close = vi.fn();
    }
    vi.stubGlobal("EventSource", FakeEventSource);
    const app = await connectPiApp();
    app.eventSource = { close: vi.fn() };
    app.setConnection = vi.fn();
    app.applyEvent = vi.fn();
    app.dataset.activeSessionId = "s1";
    app.connectEvents("s1", { replay: false });
    const source = app.eventSource;
    source.onopen();
    source.onerror(new Error("x"));
    app.eventStreamId = Symbol("stale");
    source.onopen();
    source.onerror(new Error("x"));
    listeners["session.message"]({ data: JSON.stringify({ type: "heartbeat", sessionId: "s1" }) });
    listeners.error({ data: "{" });
    expect(app.setConnection).toHaveBeenCalledWith("ok");
    expect(app.setConnection).toHaveBeenCalledWith("reconnecting");

    app.applyEvent = Object.getPrototypeOf(app).applyEvent.bind(app);
    app.appendMessage = vi.fn();
    app.appendDelta = vi.fn();
    app.updateSessionTitle = vi.fn();
    app.appendToolOutput = vi.fn();
    app.finishTool = vi.fn();
    app.setMode = vi.fn();
    app.running = false;
    app.applyEvent({ type: "heartbeat" });
    app.applyEvent({ type: "session.status", payload: { status: "idle" } });
    app.applyEvent({ type: "session.message", payload: { kind: "pi" } });
    app.applyEvent({ type: "session.delta", payload: { delta: "x" } });
    app.running = true;
    app.applyEvent({ type: "session.delta", payload: { delta: "again" } });
    app.applyEvent({ type: "session.renamed", payload: { title: "t" } });
    app.running = false;
    app.applyEvent({ type: "tool.started", payload: { tool: "x" } });
    app.running = true;
    app.applyEvent({ type: "tool.started", payload: { tool: "x2" } });
    app.running = false;
    app.applyEvent({ type: "tool.output", payload: { tool: "x" } });
    app.running = true;
    app.applyEvent({ type: "tool.output", payload: { tool: "x2" } });
    app.applyEvent({ type: "tool.finished", payload: { tool: "x" } });
    expect(app.appendMessage).toHaveBeenCalled();
    expect(app.appendDelta).toHaveBeenCalled();
    expect(app.finishTool).toHaveBeenCalled();
  });

  it("surfaces runtime auth warnings while loading prompt metadata", async () => {
    globalThis.PI_WEB_API_BASE = "http://backend.test";
    globalThis.fetch = vi.fn(async (url) => {
      const path = String(url);
      if (path.endsWith("/runtime-model")) {
        return { ok: true, json: async () => ({ status: { model: "M", warning: "OAuth token expired" } }) };
      }
      if (path.includes("/runtime-quota")) return { ok: true, json: async () => ({ status: { fiveHourQuota: 12 } }) };
      return { ok: true, json: async () => ({ workspaces: [] }) };
    });
    const app = await connectPiApp();
    app.apiConnected = true;
    app.dataset.activeWorkspaceId = "w1";

    await app.loadRuntimeStatus();
    await Promise.resolve();

    expect(app.querySelector("[data-prompt-meta]").textContent).toContain("M");
    expect(document.querySelector(".session-toast.warning").textContent).toContain("身份验证警告");
  });

  it("updates connection, mode, and prompt metadata branches", async () => {
    const app = await connectPiApp();
    app.setConnection("ok");
    expect(app.querySelector(".statusbtn")).toBeNull();
    const indicator = document.createElement("button");
    indicator.className = "statusbtn";
    app.append(indicator);
    app.setConnection("ok");
    expect(indicator.title).toBe("已连接");
    app.setConnection("err");
    expect(indicator.title).toBe("后端连接已断开");

    app.finishRunningTools = vi.fn();
    app.syncLoadingMessage = vi.fn();
    app.loadRuntimeStatus = vi.fn();
    app.setMode("cancelled");
    expect(app.finishRunningTools).toHaveBeenCalledWith({ status: "err", resultMeta: "cancelled" });
    app.setMode("thinking");
    expect(app.running).toBe(true);
    app.setMode("idle");
    expect(app.running).toBe(false);
    app.sendButton = null;
    app.setMode("idle");

    app.updatePromptMeta({ model: "M", branch: "b", fiveHourQuota: -1, weeklyQuota: 101 });
    expect(app.querySelector("[data-prompt-meta]").textContent).toContain("M");
    expect(app.quotaLabel("bad", Number.NaN)).toBeUndefined();
  });
});
