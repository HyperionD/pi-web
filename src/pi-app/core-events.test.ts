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

    const backgroundClient = { stop: vi.fn() };
    app.backgroundSessionWatches = new Map([["s2", { client: backgroundClient }]]);
    app.scrollFrame = 2;
    app.uninstallViewportSizing = vi.fn();
    app.disconnectedCallback();
    expect(backgroundClient.stop).toHaveBeenCalled();
    expect(app.uninstallViewportSizing).toHaveBeenCalled();
    expect(app.scrollFrame).toBeUndefined();
    app.spinnerTimer = undefined;
    app.runtimeStatusTimer = undefined;
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


  it("binds DOM and window events to current methods", async () => {
    const app = await connectPiApp();
    expect(appTemplate).not.toContain("data-path-form");
    expect(appTemplate).not.toContain("data-clone-form");
    expect(appTemplate).not.toContain("gitUrl");
    expect(appTemplate).not.toContain("克隆并打开");
    expect(appTemplate).not.toContain('data-view="picker"');
    expect(appTemplate).toContain('data-action="route-picker" aria-haspopup="dialog" aria-expanded="false"');
    expect(appTemplate).toContain("点击打开按钮选择一个目录后开始你的对话");
    expect(appTemplate).not.toContain("在 pi-web-offline 中新建会话");
    expect(appTemplate).not.toContain("pi 当前空闲，请说第一句话。");
    expect(appTemplate).not.toContain("首次发送后将分配会话 ID。");
    const calls = [];
    ["submitShellCommand", "saveSettingsForm", "fillSettingsForm", "fillModelControls", "syncCustomSettingInput", "setModelControlsLoading",
      "submitPrompt", "cancelActiveSession", "handlePromptPaste", "navigateList", "pickSlash", "addFiles", "startResize", "startInspectorResize", "openFolderPopover", "closeFolderPopover",
      "shortcut", "closeSessionMenus"].forEach((name) => { app[name] = (...args) => calls.push([name, ...args]); });
    app.prompt.value = "/m";
    app.updatePrompt();
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
    expect(calls.some(([name, purpose]) => name === "openFolderPopover" && purpose === "workspace")).toBe(true);
    expect(calls).toContainEqual(["closeFolderPopover"]);
  });

  it("cancels the running session when Escape is pressed", async () => {
    const app = await connectPiApp();
    const cancelActiveSession = vi.fn();
    app.cancelActiveSession = cancelActiveSession;
    app.running = true;

    const event = new KeyboardEvent("keydown", { key: "Escape", cancelable: true });
    window.dispatchEvent(event);

    expect(cancelActiveSession).toHaveBeenCalledOnce();
    expect(event.defaultPrevented).toBe(true);
  });

  it("lets Enter submit an unknown slash command when there are no matches", async () => {
    const app = await connectPiApp();
    app.prompt.value = "/unknown";
    app.updatePrompt();
    app.submitPrompt = vi.fn();

    const event = new KeyboardEvent("keydown", { key: "Enter", cancelable: true, bubbles: true });
    app.prompt.dispatchEvent(event);

    expect(app.submitPrompt).toHaveBeenCalledOnce();
  });

  it("uses Tab to complete a slash command", async () => {
    const app = await connectPiApp();
    app.prompt.value = "/mo";
    app.updatePrompt();

    const event = new KeyboardEvent("keydown", { key: "Tab", cancelable: true, bubbles: true });
    app.prompt.dispatchEvent(event);

    expect(app.prompt.value).toBe("/model ");
    expect(event.defaultPrevented).toBe(true);
  });

  it("sends an exact slash command on Enter instead of selecting it again", async () => {
    const app = await connectPiApp();
    app.prompt.value = "/model";
    app.updatePrompt();
    app.submitPrompt = vi.fn();

    const event = new KeyboardEvent("keydown", { key: "Enter", cancelable: true, bubbles: true });
    app.prompt.dispatchEvent(event);

    expect(app.submitPrompt).toHaveBeenCalledOnce();
    expect(event.defaultPrevented).toBe(true);
  });

  it("closes slash suggestions without cancelling the running session", async () => {
    const app = await connectPiApp();
    app.running = true;
    app.prompt.value = "/mo";
    app.updatePrompt();
    app.cancelActiveSession = vi.fn();

    const event = new KeyboardEvent("keydown", { key: "Escape", cancelable: true, bubbles: true });
    app.prompt.dispatchEvent(event);

    expect(app.slashPopover.hidden).toBe(true);
    expect(app.prompt.getAttribute("aria-expanded")).toBe("false");
    expect(app.prompt.hasAttribute("aria-activedescendant")).toBe(false);
    expect(app.cancelActiveSession).not.toHaveBeenCalled();
  });

  it("does not cancel a running session while the IME is composing", async () => {
    const app = await connectPiApp();
    app.running = true;
    app.cancelActiveSession = vi.fn();
    app.prompt.dispatchEvent(new Event("compositionstart"));
    app.prompt.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape", bubbles: true }));

    expect(app.cancelActiveSession).not.toHaveBeenCalled();
  });

  it("does not intercept Enter from an active IME composition", async () => {
    const app = await connectPiApp();
    app.prompt.value = "/mo";
    app.updatePrompt();
    app.submitPrompt = vi.fn();
    const event = new KeyboardEvent("keydown", { key: "Enter", cancelable: true, bubbles: true });
    Object.defineProperty(event, "keyCode", { value: 229 });
    app.prompt.dispatchEvent(event);

    expect(app.submitPrompt).not.toHaveBeenCalled();
    expect(event.defaultPrevented).toBe(false);
  });

  it("opens the sidebar workspace popover without entering a picker route", async () => {
    const app = await connectPiApp();
    const trigger = app.querySelector("[data-action='route-picker']");
    const event = { target: trigger, clientX: 90, clientY: 112 };
    app.openFolderPopover = vi.fn();
    app.dataset.route = "workspace";

    app.handleAppClick(event);

    expect(app.dataset.route).toBe("workspace");
    expect(app.querySelector('[data-view="picker"]')).toBeNull();
    expect(app.openFolderPopover).toHaveBeenCalledWith(
      "workspace",
      expect.objectContaining({ clientX: 90, clientY: 112 }),
    );
  });

  it("opens and positions the root workspace popover through a real sidebar click", async () => {
    const app = await connectPiApp();
    const trigger = app.querySelector("[data-action='route-picker']");
    const popover = app.querySelector("[data-folder-popover]");
    app.apiConnected = true;
    Object.defineProperty(window, "innerWidth", { configurable: true, value: 1024 });
    Object.defineProperty(window, "innerHeight", { configurable: true, value: 768 });
    Object.defineProperty(popover, "offsetWidth", { configurable: true, value: 300 });
    Object.defineProperty(popover, "offsetHeight", { configurable: true, value: 200 });
    trigger.getBoundingClientRect = () => ({ left: 40, top: 80, right: 120, bottom: 112, width: 80, height: 32 });
    globalThis.fetch = vi.fn(async () => ({
      ok: true,
      status: 200,
      statusText: "OK",
      json: async () => ({ path: "/allowed", parent: "/", folders: [] }),
    }));

    trigger.dispatchEvent(new MouseEvent("click", { bubbles: true, clientX: 1000, clientY: 760 }));

    await vi.waitFor(() => expect(popover.hidden).toBe(false));
    expect(app.dataset.route).toBe("workspace");
    expect(popover.parentElement).toBe(app);
    expect(popover.closest('[data-view="picker"]')).toBeNull();
    expect(popover.style.left).toBe("708px");
    expect(popover.style.top).toBe("552px");
    expect(globalThis.fetch).toHaveBeenCalledWith(
      expect.stringContaining("purpose=workspace"),
      expect.any(Object),
    );
  });

  it("closes the folder popover on Escape or outside click and restores trigger focus", async () => {
    const app = await connectPiApp();
    app.apiConnected = true;
    app.dataset.route = "workspace";
    const trigger = app.querySelector("[data-action='route-picker']");
    const popover = app.querySelector("[data-folder-popover]");
    globalThis.fetch = vi.fn(async () => ({
      ok: true,
      status: 200,
      statusText: "OK",
      json: async () => ({ path: "/allowed", parent: "/", folders: [] }),
    }));

    expect(trigger.getAttribute("aria-expanded")).toBe("false");
    trigger.click();
    await vi.waitFor(() => expect(popover.hidden).toBe(false));
    expect(trigger.getAttribute("aria-expanded")).toBe("true");
    const closeButton = popover.querySelector("[data-action='folder-close']");
    closeButton.focus();
    expect(document.activeElement).toBe(closeButton);
    window.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape" }));
    expect(popover.hidden).toBe(true);
    expect(trigger.getAttribute("aria-expanded")).toBe("false");
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

    app.prompt.value = "/m";
    app.updatePrompt();
    app.slashPopover.hidden = false;
    const slashEnter = new KeyboardEvent("keydown", { key: "Enter", bubbles: true, cancelable: true });
    app.prompt.dispatchEvent(slashEnter);
    expect(app.navigateList).toHaveBeenCalledWith(slashEnter, ".slash-item", expect.any(Function));
    expect(app.submitPrompt).toHaveBeenCalledTimes(1);
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
