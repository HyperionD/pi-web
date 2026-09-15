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

  it("covers durable connection confirmation, mode, and quota labels", async () => {
    const app = await connectPiApp();
    app.dataset.activeSessionId = "s1";
    app.confirmConnection();
    expect(app.apiConnected).toBe(true);
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
