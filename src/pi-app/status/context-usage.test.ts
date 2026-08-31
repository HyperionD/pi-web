// @ts-nocheck
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanupPiAppFixture, connectPiApp, installPiAppFixture } from "../test-helper";

describe("context usage status", () => {
  beforeEach(() => {
    globalThis.fetch = vi.fn(async () => ({ ok: true, json: async () => ({ workspaces: [] }) }));
    installPiAppFixture();
  });

  afterEach(() => cleanupPiAppFixture());

  it("renders compact used and total context with semantic pressure colors", async () => {
    const app = await connectPiApp();
    app.runtimeStatus = {};
    app.updatePromptMeta({
      model: "GPT-5.5",
      contextUsage: { tokens: 60000, contextWindow: 200000, percent: 30 },
    });
    expect(app.querySelector("[data-prompt-meta]").textContent).toBe("GPT-5.5 | — | Ctx 60K / 200K (30%)");
    expect(app.querySelector(".prompt-meta-context-safe")).not.toBeNull();

    app.runtimeStatus = {};
    app.updatePromptMeta({
      model: "GPT-5.5",
      contextUsage: { tokens: null, contextWindow: 200000, percent: null },
    });
    expect(app.querySelector("[data-prompt-meta]").textContent).toBe("GPT-5.5 | — | Ctx — / 200K");

    app.runtimeStatus = {};
    app.updatePromptMeta({
      model: "GPT-5.5",
      contextUsage: { tokens: 160000, contextWindow: 200000, percent: 80 },
    });
    expect(app.querySelector(".prompt-meta-context-warning")).not.toBeNull();

    app.runtimeStatus = {};
    app.updatePromptMeta({
      model: "GPT-5.5",
      contextUsage: { tokens: 180000, contextWindow: 200000, percent: -5 },
    });
    expect(app.querySelector("[data-prompt-meta]").textContent).toContain("Ctx 180K / 200K (0%)");

    app.runtimeStatus = {};
    app.updatePromptMeta({
      model: "GPT-5.5",
      contextUsage: { tokens: 180000, contextWindow: 200000, percent: 150 },
    });
    expect(app.querySelector("[data-prompt-meta]").textContent).toContain("Ctx 180K / 200K (100%)");
    expect(app.querySelector(".prompt-meta-context-danger")).not.toBeNull();

    app.runtimeStatus = {};
    app.updatePromptMeta({
      model: "GPT-5.5",
      contextUsage: { tokens: 180000, contextWindow: 200000, percent: 90 },
    });
    expect(app.querySelector(".prompt-meta-context-danger")).not.toBeNull();

    app.runtimeStatus = {};
    app.updatePromptMeta({ model: "GPT-5.5", contextUsage: null });
    expect(app.querySelector("[data-prompt-meta]").textContent).toBe("GPT-5.5 | —");

    expect(app.formatContextTokens(1_500_000)).toBe("1.5M");
    expect(app.formatContextTokens(999)).toBe("999");
  });

  it("compacts the active session and restores the compact control", async () => {
    const app = await connectPiApp();
    app.apiConnected = true;
    app.dataset.activeSessionId = "s1";
    globalThis.PI_WEB_API_BASE = "http://backend.test";
    app.loadSession = vi.fn();
    let resolveRequest;
    globalThis.fetch = vi.fn(() => new Promise((resolve) => { resolveRequest = resolve; }));

    const compacting = app.compactActiveSession();
    const button = app.querySelector("[data-action='compact-session']");
    expect(button.disabled).toBe(true);
    expect(button.textContent).toBe("压缩中…");
    app.applyEvent({ type: "session.status", payload: { status: "idle" } });
    expect(button.disabled).toBe(true);
    expect(button.textContent).toBe("压缩中…");
    resolveRequest({ ok: true, json: async () => ({ compacted: true }) });
    await compacting;

    expect(globalThis.fetch).toHaveBeenCalledWith(
      "http://backend.test/api/sessions/s1/compact",
      expect.objectContaining({ method: "POST" }),
    );
    expect(app.loadSession).toHaveBeenCalledWith("s1");
    expect(button.disabled).toBe(false);
    expect(button.textContent).toBe("压缩上下文");
  });

  it("restores compacting mode when the session status is loaded", async () => {
    const app = await connectPiApp();
    app.apiConnected = true;
    app.dataset.activeSessionId = "s1";

    app.setMode("compacting");

    expect(app.compacting).toBe(true);
    expect(app.querySelector("[data-action='compact-session']").disabled).toBe(true);
  });

  it("resets the compact control after a compaction error", async () => {
    const app = await connectPiApp();
    app.apiConnected = true;
    app.dataset.activeSessionId = "s1";
    app.setConnection = vi.fn();
    globalThis.PI_WEB_API_BASE = "http://backend.test";
    globalThis.fetch = vi.fn(async () => ({
      ok: false,
      status: 500,
      statusText: "Internal Server Error",
      json: async () => ({ error: "quota exceeded" }),
    }));

    await app.compactActiveSession();

    expect(app.setConnection).toHaveBeenCalledWith("err");
    const button = app.querySelector("[data-action='compact-session']");
    expect(button.disabled).toBe(false);
    expect(button.textContent).toBe("压缩上下文");
  });

  it("ignores context usage returned after the active session changes", async () => {
    const app = await connectPiApp();
    app.apiConnected = true;
    app.dataset.activeSessionId = "old";
    globalThis.PI_WEB_API_BASE = "http://backend.test";
    let resolveRequest;
    globalThis.fetch = vi.fn(() => new Promise((resolve) => { resolveRequest = resolve; }));

    const loading = app.loadContextUsage();
    app.dataset.activeSessionId = "new";
    resolveRequest({
      ok: true,
      json: async () => ({ contextUsage: { tokens: 90000, contextWindow: 100000, percent: 90 } }),
    });
    await loading;

    expect(app.querySelector("[data-prompt-meta]").textContent).not.toContain("Ctx");
  });

  it("loads active-session context usage with runtime status polling", async () => {
    const app = await connectPiApp();
    app.apiConnected = true;
    app.dataset.activeWorkspaceId = "w1";
    app.dataset.activeSessionId = "s/1";
    globalThis.PI_WEB_API_BASE = "http://backend.test";
    globalThis.fetch = vi.fn(async (url) => {
      const path = String(url);
      if (path.endsWith("/runtime-model")) {
        return { ok: true, json: async () => ({ status: { model: "GPT-5.5", currentBranch: "main" } }) };
      }
      if (path.includes("/context-usage")) {
        return {
          ok: true,
          json: async () => ({ contextUsage: { tokens: 60000, contextWindow: 200000, percent: 30 } }),
        };
      }
      return { ok: true, json: async () => ({ status: {} }) };
    });

    await app.loadRuntimeStatus();

    expect(globalThis.fetch).toHaveBeenCalledWith(
      "http://backend.test/api/sessions/s%2F1/context-usage",
      expect.any(Object),
    );
    expect(app.querySelector("[data-prompt-meta]").textContent).toContain("Ctx 60K / 200K (30%)");
  });
});
