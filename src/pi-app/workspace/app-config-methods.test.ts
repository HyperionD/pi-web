// @ts-nocheck
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { getAppConfig, saveAppConfig } from "../../lib/api";
import { cleanupPiAppFixture, connectPiApp, installPiAppFixture } from "../test-helper";
import { appConfigMethods } from "./app-config-methods";
import { DEFAULT_APP_BRAND, parseAppConfigResponse } from "./app-config-schema";

function ok(body = {}) {
  return { ok: true, status: 200, statusText: "OK", json: async () => body };
}

describe("app config methods", () => {
  beforeEach(() => {
    installPiAppFixture();
    globalThis.PI_WEB_API_BASE = "http://backend.test";
    const app = document.querySelector("pi-app");
    app.insertAdjacentHTML("afterbegin", '<span data-brand></span><input data-app-setting="brand" />');
  });

  afterEach(() => {
    cleanupPiAppFixture();
  });

  it("preserves unknown keys in valid app config responses", () => {
    expect(parseAppConfigResponse({
      config: { version: 1, brand: "JuunAI / web", custom: { keep: true } },
      path: "/home/me/.pi/agent/pi-web-config.json",
      requestId: "req-1",
    })).toMatchObject({
      config: { version: 1, brand: "JuunAI / web", custom: { keep: true } },
      requestId: "req-1",
    });
  });

  it("rejects a malformed top-level response", () => {
    expect(() => parseAppConfigResponse(null)).toThrow("value:");
  });

  it("loads the server brand without a workspace id", async () => {
    const app = await connectPiApp();
    globalThis.fetch = vi.fn(async () => ok({
      config: { version: 1, brand: "JuunAI / web" },
      path: "/home/me/.pi/agent/pi-web-config.json",
    }));
    Object.assign(app, appConfigMethods);

    await app.loadAppConfigState();

    expect(app.appConfigState.config.brand).toBe("JuunAI / web");
    expect(app.querySelector("[data-brand]").textContent).toBe("JuunAI / web");
    expect(globalThis.fetch.mock.calls[0][0]).toBe("http://backend.test/api/app-config");
  });

  it("renders brand as text rather than HTML", async () => {
    const app = await connectPiApp();
    Object.assign(app, appConfigMethods);

    app.applyBrand("<b>unsafe</b>");

    expect(app.querySelector("[data-brand]").textContent).toBe("<b>unsafe</b>");
    expect(app.querySelector("[data-brand] b")).toBeNull();
  });

  it("throws when the server response is malformed", async () => {
    const app = await connectPiApp();
    globalThis.fetch = vi.fn(async () => ok({ config: { version: 1 }, path: "/config" }));
    Object.assign(app, appConfigMethods);

    await expect(app.loadAppConfigState()).rejects.toThrow("invalid app config response");
  });

  it("saves only the app brand patch", async () => {
    const app = await connectPiApp();
    const input = app.querySelector("[data-app-setting='brand']");
    input.value = "next";
    Object.assign(app, appConfigMethods);
    globalThis.fetch = vi.fn(async () => ok({
      config: { version: 1, brand: "next" },
      path: "/config",
    }));

    const response = await saveAppConfig(app.appConfigPatchFromForm());

    expect(response.config.brand).toBe("next");
    expect(globalThis.fetch.mock.calls[0][0]).toBe("http://backend.test/api/app-config");
    expect(globalThis.fetch.mock.calls[0][1]).toMatchObject({ method: "PATCH", body: '{"brand":"next"}' });
  });

  it("fills and reads only the app brand control", async () => {
    const app = await connectPiApp();
    Object.assign(app, appConfigMethods);
    app.appConfigState = { config: { version: 1, brand: "saved", other: "ignored" }, path: "/config" };

    app.fillAppConfigForm();
    expect(app.querySelector("[data-app-setting='brand']").value).toBe("saved");
    app.querySelector("[data-app-setting='brand']").value = "  next  ";

    expect(app.appConfigPatchFromForm()).toEqual({ brand: "next" });
  });

  it("restores the default brand for blank or non-string values", async () => {
    const app = await connectPiApp();
    Object.assign(app, appConfigMethods);

    app.applyBrand("  ");
    expect(app.querySelector("[data-brand]").textContent).toBe(DEFAULT_APP_BRAND);
    app.applyBrand(42);
    expect(app.querySelector("[data-brand]").textContent).toBe(DEFAULT_APP_BRAND);
  });

  it("handles missing app config controls without throwing", async () => {
    const app = await connectPiApp();
    Object.assign(app, appConfigMethods);
    app.querySelector("[data-brand]").remove();
    app.querySelector("[data-app-setting='brand']").remove();

    app.applyBrand("next");
    app.fillAppConfigForm();

    expect(app.appConfigPatchFromForm()).toEqual({});
  });

  it("exposes the app config GET API wrapper", async () => {
    globalThis.fetch = vi.fn(async () => ok({ config: {}, path: "/config" }));

    await getAppConfig();

    expect(globalThis.fetch.mock.calls[0][0]).toBe("http://backend.test/api/app-config");
  });
});
