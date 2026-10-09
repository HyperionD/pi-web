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
  afterEach(cleanupPiAppFixture);

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
    expect(app.querySelector("[data-model-catalog-info]").textContent).toContain("1 个模型");
  });

  it("disables selection and reports a broken configured catalog", async () => {
    globalThis.PI_WEB_API_BASE = "http://backend.test";
    globalThis.fetch = vi.fn(async (url) => {
      if (String(url).endsWith("/settings")) {
        return response({ settings: { ...settings, modelSelection: { catalogError: "invalid Pi models.json" } } });
      }
      if (String(url).endsWith("/models")) {
        return { ok: false, status: 502, statusText: "Bad Gateway", json: async () => ({ error: "invalid Pi models.json" }) };
      }
      return response({ providers: [] });
    });
    const app = await connectPiApp();
    app.apiConnected = true;
    app.dataset.activeWorkspaceId = "w1";
    await app.openSettingsModal();
    expect(app.querySelector("[data-account-model]").disabled).toBe(true);
    expect(app.querySelector("[data-account-model-status]").textContent).toContain("models.json");
  });
});
