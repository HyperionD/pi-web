// @ts-nocheck
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanupPiAppFixture, connectPiApp, installPiAppFixture } from "../test-helper";

const response = (body) => ({ ok: true, status: 200, statusText: "OK", json: async () => body });
const settings = {
  global: {}, project: {}, personalGlobal: {}, personalProject: {},
  effective: { defaultProvider: "local", defaultModel: "a", defaultThinkingLevel: "off" },
  modelSelection: { source: "first", invalidPersonal: false }, paths: {},
};

describe("offline account model settings", () => {
  beforeEach(installPiAppFixture);
  afterEach(() => {
    cleanupPiAppFixture();
    delete document.body.dataset.accountRole;
  });

  it("only offers configured models and saves the account-wide choice", async () => {
    globalThis.PI_WEB_API_BASE = "http://backend.test";
    globalThis.fetch = vi.fn(async (url, options = {}) => {
      if (String(url).endsWith("/models")) return response({ providers: [
        { id: "local", models: [{ id: "a", provider: "local" }, { id: "b", provider: "local" }] },
      ] });
      if (String(url).endsWith("/settings")) return response({ settings });
      return response({ providers: [] });
    });
    const app = await connectPiApp();
    document.body.dataset.accountRole = "user";
    app.apiConnected = true;
    app.dataset.activeWorkspaceId = "w1";
    app.dataset.activeSessionId = "s1";
    await app.openSettingsModal();

    const select = app.querySelector("[data-account-model]");
    expect([...select.options].map((option) => option.textContent)).toEqual([
      "使用管理员默认", "local / a", "local / b",
    ]);
    select.value = JSON.stringify(["local", "b"]);
    await app.saveSettingsForm(new Event("submit"));
    const puts = globalThis.fetch.mock.calls.filter(([, options]) => options?.method === "PUT");
    expect(puts).toHaveLength(1);
    expect(JSON.parse(puts[0][1].body)).toMatchObject({
      scope: "personal", location: "global", settings: { defaultProvider: "local", defaultModel: "b" },
    });
    expect(globalThis.fetch.mock.calls.some(([url]) => String(url).includes("/sessions/s1/model"))).toBe(false);
  });

  it("applies a selected model to only the active session without saving it", async () => {
    globalThis.PI_WEB_API_BASE = "http://backend.test";
    globalThis.fetch = vi.fn(async (url) => {
      if (String(url).endsWith("/models")) return response({ providers: [
        { id: "local", models: [{ id: "a", provider: "local" }, { id: "b", provider: "local" }] },
      ] });
      if (String(url).endsWith("/settings")) return response({ settings });
      return response({ accepted: true });
    });
    const app = await connectPiApp();
    app.apiConnected = true;
    app.dataset.activeWorkspaceId = "w1";
    app.dataset.activeSessionId = "s1";
    await app.openSettingsModal();
    app.querySelector("[data-account-model]").value = JSON.stringify(["local", "b"]);
    await app.applyEffectiveModelToActiveSession();
    const post = globalThis.fetch.mock.calls.find(([url]) => String(url).endsWith("/sessions/s1/model"));
    expect(JSON.parse(post[1].body)).toMatchObject({ provider: "local", model: "b" });
    expect(globalThis.fetch.mock.calls.some(([, options]) => options?.method === "PUT")).toBe(false);
  });

  it("refreshes combined model and shared settings after concurrent saves", async () => {
    globalThis.PI_WEB_API_BASE = "http://backend.test";
    let current = structuredClone(settings);
    let finishPersonal;
    const personalSaved = new Promise((resolve) => { finishPersonal = resolve; });
    globalThis.fetch = vi.fn(async (url, options = {}) => {
      if (String(url).endsWith("/models")) return response({ providers: [
        { id: "local", models: [{ id: "a", provider: "local" }, { id: "b", provider: "local" }] },
      ] });
      if (String(url).endsWith("/settings") && options.method === "PUT") {
        const body = JSON.parse(options.body);
        if (body.scope === "personal") {
          current = { ...current, personalGlobal: body.settings, effective: {
            ...current.effective, ...body.settings,
          } };
          finishPersonal();
          return response({ settings: structuredClone(current) });
        }
        await personalSaved;
        current = { ...current, project: body.settings, effective: {
          ...current.effective, compaction: { enabled: false },
        } };
        return response({ settings: structuredClone(current) });
      }
      if (String(url).endsWith("/settings")) return response({ settings: structuredClone(current) });
      return response({ providers: [] });
    });
    const app = await connectPiApp();
    document.body.dataset.accountRole = "admin";
    app.apiConnected = true;
    app.dataset.activeWorkspaceId = "w1";
    await app.openSettingsModal();
    app.querySelector("[data-account-model]").value = JSON.stringify(["local", "b"]);
    app.querySelector("[data-setting='compaction.enabled']").value = "false";
    await app.saveSettingsForm(new Event("submit"));
    expect(app.settingsState.effective.defaultModel).toBe("b");
    expect(app.settingsState.effective.compaction.enabled).toBe(false);
  });

  it("does not apply a save response after switching workspaces", async () => {
    globalThis.PI_WEB_API_BASE = "http://backend.test";
    let finishSave;
    const saveReply = new Promise((resolve) => { finishSave = resolve; });
    globalThis.fetch = vi.fn(async (url, options = {}) => {
      if (String(url).endsWith("/models")) return response({ providers: [
        { id: "local", models: [{ id: "a", provider: "local" }, { id: "b", provider: "local" }] },
      ] });
      if (String(url).endsWith("/settings") && options.method === "PUT") return saveReply;
      if (String(url).endsWith("/settings")) return response({ settings });
      return response({ providers: [] });
    });
    const app = await connectPiApp();
    app.apiConnected = true;
    app.dataset.activeWorkspaceId = "w1";
    await app.openSettingsModal();
    app.querySelector("[data-account-model]").value = JSON.stringify(["local", "b"]);
    const saving = app.saveSettingsForm(new Event("submit"));
    app.dataset.activeWorkspaceId = "w2";
    app.settingsState = { ...settings, effective: { defaultProvider: "local", defaultModel: "a" } };
    finishSave(response({ settings: { ...settings, effective: { defaultProvider: "local", defaultModel: "b" } } }));
    await saving;
    expect(app.settingsState.effective.defaultModel).toBe("a");
  });

  it("shows the configured catalog path and count in administrator diagnostics", async () => {
    globalThis.PI_WEB_API_BASE = "http://backend.test";
    globalThis.fetch = vi.fn(async (url) => {
      if (String(url).endsWith("/settings")) return response({ settings });
      if (String(url).endsWith("/models")) return response({
        providers: [{ id: "local", models: [{ id: "a", provider: "local" }] }],
        path: "/srv/.pi/agent/models.json", count: 1,
      });
      return response({ providers: [] });
    });
    const app = await connectPiApp();
    document.body.dataset.accountRole = "admin";
    app.apiConnected = true;
    app.dataset.activeWorkspaceId = "w1";
    await app.openSettingsModal();
    expect(app.querySelector("[data-model-catalog-info]").textContent).toContain("/srv/.pi/agent/models.json");
    expect(app.querySelector("[data-model-catalog-info]").textContent).toContain("Pi 可用模型：1 个");
  });

  it("retains a removed personal model when only thinking preference is saved", async () => {
    globalThis.PI_WEB_API_BASE = "http://backend.test";
    globalThis.fetch = vi.fn(async (url, options = {}) => {
      if (String(url).endsWith("/models")) return response({ providers: [
        { id: "local", models: [{ id: "a", provider: "local" }] },
      ] });
      if (String(url).endsWith("/settings")) return response({ settings: {
        ...settings, personalGlobal: { defaultProvider: "local", defaultModel: "removed" },
        modelSelection: { source: "first", invalidPersonal: true },
      } });
      return response({ providers: [] });
    });
    const app = await connectPiApp();
    app.apiConnected = true;
    app.dataset.activeWorkspaceId = "w1";
    await app.openSettingsModal();
    app.querySelector("[data-setting='defaultThinkingLevel']").value = "high";
    await app.saveSettingsForm(new Event("submit"));
    const put = globalThis.fetch.mock.calls.find(([, options]) => options?.method === "PUT");
    expect(JSON.parse(put[1].body).settings).toEqual({ defaultThinkingLevel: "high" });
  });

  it("only clears a stale preference after an explicit reset", async () => {
    globalThis.PI_WEB_API_BASE = "http://backend.test";
    globalThis.fetch = vi.fn(async (url) => {
      if (String(url).endsWith("/models")) return response({ providers: [
        { id: "local", models: [{ id: "a", provider: "local" }] },
      ] });
      if (String(url).endsWith("/settings")) return response({ settings: {
        ...settings, personalGlobal: { defaultProvider: "local", defaultModel: "removed" },
        modelSelection: { source: "first", invalidPersonal: true },
      } });
      return response({ providers: [] });
    });
    const app = await connectPiApp();
    app.apiConnected = true;
    app.dataset.activeWorkspaceId = "w1";
    await app.openSettingsModal();
    app.querySelector("[data-action='reset-account-model']").click();
    await app.saveSettingsForm(new Event("submit"));
    const put = globalThis.fetch.mock.calls.find(([, options]) => options?.method === "PUT");
    expect(JSON.parse(put[1].body).settings).toEqual({ defaultProvider: null, defaultModel: null });
  });

  it("disables selection and reports a broken configured catalog", async () => {
    globalThis.PI_WEB_API_BASE = "http://backend.test";
    globalThis.fetch = vi.fn(async (url) => {
      if (String(url).endsWith("/settings")) {
        return response({ settings: { ...settings,
          personalGlobal: { defaultProvider: "local", defaultModel: "removed" },
          modelSelection: { catalogError: "cannot list Pi models" },
        } });
      }
      if (String(url).endsWith("/models")) {
        return { ok: false, status: 502, statusText: "Bad Gateway", json: async () => ({ error: "cannot list Pi models" }) };
      }
      return response({ providers: [] });
    });
    const app = await connectPiApp();
    app.apiConnected = true;
    app.dataset.activeWorkspaceId = "w1";
    await app.openSettingsModal();
    expect(app.querySelector("[data-account-model]").disabled).toBe(true);
    expect(app.querySelector("[data-action='reset-account-model']").disabled).toBe(true);
    expect(app.querySelector("[data-account-model-status]").textContent).toContain("cannot list Pi models");
  });
});
