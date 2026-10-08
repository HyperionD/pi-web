// @ts-nocheck
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  getAdminPasswordStatus,
  getAppConfig,
  listFolders,
  saveAppConfig,
} from "../../lib/api";
import { cleanupPiAppFixture, connectPiApp, installPiAppFixture } from "../test-helper";
import { appConfigMethods } from "./app-config-methods";
import {
  appConfigPatchSchema,
  DEFAULT_APP_BRAND,
  parseAppConfigResponse,
} from "./app-config-schema";

function ok(body = {}) {
  return { ok: true, status: 200, statusText: "OK", json: async () => body };
}

describe("app config methods", () => {
  beforeEach(() => {
    installPiAppFixture();
    globalThis.PI_WEB_API_BASE = "http://backend.test";
    const app = document.querySelector("pi-app");
  });

  afterEach(() => {
    cleanupPiAppFixture();
    delete document.body.dataset.userManagement;
    delete document.body.dataset.accountRole;
  });

  it("preserves unknown keys in valid app config responses", () => {
    expect(parseAppConfigResponse({
      config: { version: 1, brand: "pi-web / web", custom: { keep: true } },
      path: "/home/me/.pi/agent/pi-web-config.json",
      requestId: "req-1",
    })).toMatchObject({
      config: { version: 1, brand: "pi-web / web", custom: { keep: true } },
      requestId: "req-1",
    });
  });

  it("rejects a malformed top-level response", () => {
    expect(() => parseAppConfigResponse(null)).toThrow("value:");
  });

  it("preserves multiple allowed roots in the app config response", () => {
    const response = parseAppConfigResponse({
      config: { version: 1, brand: "x", workspace: { allowedRoots: ["/a", "/b"] } },
      path: "/config",
    });

    expect(response.config.workspace.allowedRoots).toEqual(["/a", "/b"]);
  });

  it("preserves the workspace max upload bytes in the app config response", () => {
    const response = parseAppConfigResponse({
      config: { version: 1, brand: "x", workspace: { maxUploadBytes: 10485760 } },
      path: "/config",
    });

    expect(response.config.workspace.maxUploadBytes).toBe(10485760);
    expect(appConfigPatchSchema.parse({ workspace: { maxUploadBytes: 10485760 } })).toEqual({
      workspace: { maxUploadBytes: 10485760 },
    });
  });

  it("rejects an invalid workspace allowed-roots response", () => {
    expect(() => parseAppConfigResponse({
      config: { version: 1, brand: "x", workspace: { allowedRoots: ["/a", 42] } },
      path: "/config",
    })).toThrow("workspace.allowedRoots");
  });

  it("passes allowed-root purpose through folder listing requests", async () => {
    globalThis.fetch = vi.fn(async () => ok({}));
    await listFolders("/a", "allowed-root");

    expect(globalThis.fetch.mock.calls[0][0]).toBe(
      "http://backend.test/api/system/folders?path=%2Fa&purpose=allowed-root",
    );
  });

  it("sends the admin password in a header for app config saves", async () => {
    globalThis.fetch = vi.fn(async () => ok({ config: {}, path: "/config" }));

    await saveAppConfig({ brand: "Custom" }, "secret");

    expect(globalThis.fetch.mock.calls[0][1]).toMatchObject({
      method: "PATCH",
      headers: { "X-Admin-Password": "secret" },
    });
  });

  it("does not prompt an authenticated administrator for admin password", async () => {
    const app = await connectPiApp();
    Object.assign(app, appConfigMethods);
    document.body.dataset.userManagement = "true";
    document.body.dataset.accountRole = "admin";
    app.adminPasswordConfigured = true;
    const prompt = vi.spyOn(window, "prompt");

    await expect(app.requestAdminPassword()).resolves.toBe("");

    expect(prompt).not.toHaveBeenCalled();
  });

  it("loads the admin password status and shows the first-use gate", async () => {
    const app = await connectPiApp();
    Object.assign(app, appConfigMethods);
    globalThis.fetch = vi.fn(async () => ok({ configured: false }));

    await app.loadAdminPasswordState();

    expect(app.adminPasswordConfigured).toBe(false);
    expect(app.querySelector("[data-admin-password-gate]").hidden).toBe(false);
    expect(globalThis.fetch.mock.calls[0][0]).toBe("http://backend.test/api/admin/password/status");
  });

  it("sets the first admin password and releases the gate", async () => {
    const app = await connectPiApp();
    Object.assign(app, appConfigMethods);
    app.adminPasswordConfigured = false;
    app.showAdminPasswordGate();
    app.querySelector("[data-admin-password]").value = "correct horse battery";
    app.querySelector("[data-admin-password-confirmation]").value = "correct horse battery";
    globalThis.fetch = vi.fn(async () => ok({ configured: true }));

    await app.submitAdminPasswordSetup({ preventDefault: vi.fn() });

    expect(app.adminPasswordConfigured).toBe(true);
    expect(app.querySelector("[data-admin-password-gate]").hidden).toBe(true);
    expect(globalThis.fetch.mock.calls[0][0]).toBe("http://backend.test/api/admin/password");
  });

  it("loads the server brand without a workspace id", async () => {
    const app = await connectPiApp();
    globalThis.fetch = vi.fn(async () => ok({
      config: { version: 1, brand: "pi-web / web" },
      path: "/home/me/.pi/agent/pi-web-config.json",
    }));
    Object.assign(app, appConfigMethods);

    await app.loadAppConfigState();

    expect(app.appConfigState.config.brand).toBe("pi-web / web");
    expect(app.querySelector("[data-brand]").textContent).toBe("pi-web / web");
    expect(document.title).toBe("pi-web / web");
    expect(globalThis.fetch.mock.calls[0][0]).toBe("http://backend.test/api/app-config");
  });

  it("bootstraps app config and workspaces independently", async () => {
    const app = await connectPiApp();
    app.renderWorkspaces = vi.fn();
    app.loadWorkspaceMeta = vi.fn();
    app.loadSession = vi.fn();
    const requests = [];
    globalThis.fetch = vi.fn(async (url) => {
      requests.push(String(url));
      if (String(url).endsWith("/app-config")) {
        return ok({ config: { version: 1, brand: "Custom" }, path: "/config" });
      }
      return ok({ workspaces: [] });
    });

    await app.bootstrapAPI();

    expect(requests).toContain("http://backend.test/api/app-config");
    expect(requests).toContain("http://backend.test/api/workspaces");
    expect(app.querySelector("[data-brand]").textContent).toBe("Custom");
  });

  it("ignores stale app config success from an older bootstrap generation", async () => {
    const app = await connectPiApp();
    app.renderWorkspaces = vi.fn();
    let resolveConfig;
    globalThis.fetch = vi.fn(async (url) => String(url).endsWith("/app-config")
      ? new Promise((resolve) => { resolveConfig = resolve; })
      : ok({ workspaces: [] }));

    const bootstrap = app.bootstrapAPI();
    await vi.waitFor(() => expect(globalThis.fetch).toHaveBeenCalledWith(
      expect.stringContaining("/app-config"),
      expect.any(Object),
    ));
    app.bootstrapGeneration = Symbol("newer-bootstrap");
    resolveConfig(ok({ config: { version: 1, brand: "stale" }, path: "/config" }));
    await bootstrap;

    expect(app.appConfigState).toBeUndefined();
    expect(app.querySelector("[data-brand]").textContent).toBe("pi.dev / web");
  });

  it("keeps workspace bootstrap and the default brand when app config fails", async () => {
    const app = await connectPiApp();
    app.renderWorkspaces = vi.fn();
    const requests = [];
    globalThis.fetch = vi.fn(async (url) => {
      requests.push(String(url));
      if (String(url).endsWith("/app-config")) return { ok: false, status: 500, statusText: "ERR", json: async () => ({}) };
      return ok({ workspaces: [] });
    });

    await app.bootstrapAPI();

    expect(requests).toContain("http://backend.test/api/app-config");
    expect(requests).toContain("http://backend.test/api/workspaces");
    expect(app.querySelector("[data-brand]").textContent).toBe(DEFAULT_APP_BRAND);
  });

  it("confirms the administrator before saving app config changes", async () => {
    const app = await connectPiApp();
    app.apiConnected = true;
    app.adminPasswordConfigured = true;
    app.settingsState = { project: {}, global: {}, effective: {}, paths: {} };
    app.appConfigState = { config: { version: 1, brand: "old" }, path: "/config" };
    Object.assign(app, appConfigMethods);
    app.fillAppConfigForm();
    app.querySelector("[data-app-setting='brand']").value = "Custom";
    vi.spyOn(window, "prompt").mockReturnValue("secret");
    globalThis.fetch = vi.fn(async () => ok({ config: { version: 1, brand: "Custom" }, path: "/config" }));

    await app.saveSettingsForm({ preventDefault: vi.fn() });

    const patchCalls = globalThis.fetch.mock.calls.filter(([, options]) => options?.method === "PATCH");
    expect(patchCalls[0][1].headers).toMatchObject({ "X-Admin-Password": "secret" });
  });

  it("does not request admin confirmation when app config has no loaded baseline", async () => {
    const app = await connectPiApp();
    app.apiConnected = true;
    app.adminPasswordConfigured = true;
    app.dataset.activeWorkspaceId = "w1";
    app.settingsState = { project: {}, global: {}, effective: {}, paths: {} };
    app.querySelector("[data-setting='defaultThinkingLevel']").value = "high";
    app.requestAdminPassword = vi.fn();
    globalThis.fetch = vi.fn(async (url, options = {}) => options.method === "PUT"
      ? ok({ settings: { project: {}, global: {}, effective: {}, paths: {} } })
      : ok({ config: { version: 1, brand: "Custom" }, path: "/config" }));

    await app.saveSettingsForm({ preventDefault: vi.fn() });

    expect(app.appConfigPatchFromForm()).toEqual({});
    expect(app.requestAdminPassword).not.toHaveBeenCalled();
  });

  it("does not request admin confirmation when allowed roots are unchanged", async () => {
    const app = await connectPiApp();
    app.apiConnected = true;
    app.adminPasswordConfigured = true;
    app.dataset.activeWorkspaceId = "w1";
    app.settingsState = { project: {}, global: {}, effective: {}, paths: {} };
    app.appConfigState = {
      config: { version: 1, brand: "Custom", workspace: { allowedRoots: ["/a"] } },
      path: "/config",
    };
    Object.assign(app, appConfigMethods);
    app.fillAppConfigForm();
    app.querySelector("[data-setting='defaultThinkingLevel']").value = "high";
    app.requestAdminPassword = vi.fn();
    globalThis.fetch = vi.fn(async (url, options = {}) => options.method === "PUT"
      ? ok({ settings: { project: {}, global: {}, effective: {}, paths: {} } })
      : ok({ config: { version: 1, brand: "Custom", workspace: { allowedRoots: ["/a"] } }, path: "/config" }));

    await app.saveSettingsForm({ preventDefault: vi.fn() });

    expect(app.appConfigPatchFromForm()).toEqual({});
    expect(app.requestAdminPassword).not.toHaveBeenCalled();
  });

  it("does not request admin confirmation when app settings are unchanged", async () => {
    const app = await connectPiApp();
    app.apiConnected = true;
    app.adminPasswordConfigured = true;
    app.dataset.activeWorkspaceId = "w1";
    app.settingsState = { project: {}, global: {}, effective: {}, paths: {} };
    app.appConfigState = { config: { version: 1, brand: "Custom" }, path: "/config" };
    Object.assign(app, appConfigMethods);
    app.fillAppConfigForm();
    app.querySelector("[data-setting='defaultThinkingLevel']").value = "high";
    app.requestAdminPassword = vi.fn();
    globalThis.fetch = vi.fn(async (url, options = {}) => options.method === "PUT"
      ? ok({ settings: { project: {}, global: {}, effective: {}, paths: {} } })
      : ok({ config: { version: 1, brand: "Custom" }, path: "/config" }));

    await app.saveSettingsForm({ preventDefault: vi.fn() });

    expect(app.appConfigPatchFromForm()).toEqual({});
    expect(app.requestAdminPassword).not.toHaveBeenCalled();
  });

  it("saves app config without an active workspace", async () => {
    const app = await connectPiApp();
    app.apiConnected = true;
    app.settingsState = { project: {}, global: {}, effective: {}, paths: {} };
    app.appConfigState = { config: { version: 1, brand: "pi.dev / web" }, path: "/config" };
    Object.assign(app, appConfigMethods);
    app.fillAppConfigForm();
    app.querySelector("[data-app-setting='brand']").value = "Custom";
    globalThis.fetch = vi.fn(async () => ok({ config: { version: 1, brand: "Custom" }, path: "/config" }));

    await app.saveSettingsForm({ preventDefault: vi.fn() });

    const patchCalls = globalThis.fetch.mock.calls.filter(([, options]) => options?.method === "PATCH");
    expect(patchCalls).toHaveLength(1);
    expect(patchCalls[0][0]).toBe("http://backend.test/api/app-config");
    expect(JSON.parse(patchCalls[0][1].body)).toEqual({ brand: "Custom" });
  });

  it("refreshes attachment guidance immediately after saving the upload limit", async () => {
    const app = await connectPiApp();
    const guidance = document.createElement("p");
    guidance.dataset.attachmentGuidance = "";
    guidance.innerHTML = `
      <span data-i18n="attachmentGuidancePrefix">较大文件建议上传到工作区。</span>
      <strong><span data-attachment-max-upload>100</span> MiB</strong>
      <span data-i18n="attachmentGuidanceSuffix">附件预算说明。</span>
    `;
    app.append(guidance);
    app.apiConnected = true;
    app.settingsState = { project: {}, global: {}, effective: {}, paths: {} };
    app.appConfigState = {
      config: { version: 1, brand: "saved", workspace: { maxUploadBytes: 100 * 1024 * 1024 } },
      path: "/config",
    };
    Object.assign(app, appConfigMethods);
    app.fillAppConfigForm();
    app.updateAttachmentGuidance();
    expect(guidance.textContent).toContain("100 MiB");
    app.querySelector("[data-app-setting='max-upload-mib']").value = "256";
    app.requestAdminPassword = vi.fn().mockResolvedValue("secret");
    globalThis.fetch = vi.fn(async () => ok({
      config: { version: 1, brand: "saved", workspace: { maxUploadBytes: 256 * 1024 * 1024 } },
      path: "/config",
    }));

    await app.saveSettingsForm({ preventDefault: vi.fn() });

    expect(guidance.textContent).toContain("256 MiB");
  });

  it("renders one row per allowed root and exposes unrestricted empty state", async () => {
    const app = await connectPiApp();
    Object.assign(app, appConfigMethods);
    app.appConfigState = {
      config: { version: 1, brand: "x", workspace: { allowedRoots: ["/a", "/b"] } },
      path: "/config",
    };

    app.fillAppConfigForm();

    expect(app.querySelectorAll("[data-allowed-root-row]")).toHaveLength(2);
    expect(app.querySelector("[data-app-setting='allowed-roots']").value).toBe("/a\n/b");
    app.appConfigState.config.workspace.allowedRoots = [];
    app.fillAppConfigForm();
    expect(app.querySelector("[data-allowed-roots-hint]").textContent).toContain("不限制");
  });

  it("adds a filled root after an existing root and collects both visible rows", async () => {
    const app = await connectPiApp();
    Object.assign(app, appConfigMethods);
    app.appConfigState = {
      config: { version: 1, brand: "x", workspace: { allowedRoots: ["/a"] } },
      path: "/config",
    };
    app.fillAppConfigForm();

    app.addAllowedRootRow();
    app.querySelector("[data-allowed-root-row]:last-child input").value = "/b";

    expect(app.appConfigPatchFromForm()).toEqual({ workspace: { allowedRoots: ["/a", "/b"] } });
  });

  it("adds and removes allowed-root rows", async () => {
    const app = await connectPiApp();
    Object.assign(app, appConfigMethods);
    app.renderAllowedRootRows(["/a"]);

    app.addAllowedRootRow();
    expect(app.querySelectorAll("[data-allowed-root-row]")).toHaveLength(2);
    app.querySelector("[data-allowed-root-row]:last-child input").value = "/b";
    app.removeAllowedRootRow(app.querySelector("[data-allowed-root-row]:last-child"));

    expect(app.appConfigPatchFromForm().workspace.allowedRoots).toEqual(["/a"]);
  });

  it("collects an empty list after removing the only allowed-root row", () => {
    const app = document.querySelector("pi-app");
    Object.assign(app, appConfigMethods);
    app.appConfigState = {
      config: { version: 1, brand: "x", workspace: { allowedRoots: ["/a"] } },
      path: "/config",
    };
    app.fillAppConfigForm();

    app.removeAllowedRootRow(app.querySelector("[data-allowed-root-row]"));

    expect(app.appConfigPatchFromForm()).toEqual({ workspace: { allowedRoots: [] } });
  });

  it("submits an empty brand with an empty roots list in the same patch", () => {
    const app = document.querySelector("pi-app");
    Object.assign(app, appConfigMethods);
    app.appConfigState = {
      config: { version: 1, brand: "x", workspace: { allowedRoots: ["/a"] } },
      path: "/config",
    };
    app.fillAppConfigForm();
    app.querySelector("[data-app-setting='brand']").value = "";
    app.removeAllowedRootRow(app.querySelector("[data-allowed-root-row]"));

    expect(app.appConfigPatchFromForm()).toEqual({ brand: "", workspace: { allowedRoots: [] } });
  });

  it("sends workspace and brand app-config patches together and applies the response", async () => {
    const app = await connectPiApp();
    app.apiConnected = true;
    app.settingsState = { project: {}, global: {}, effective: {}, paths: {} };
    app.appConfigState = { config: { version: 1, brand: "pi.dev / web" }, path: "/config" };
    Object.assign(app, appConfigMethods);
    app.fillAppConfigForm();
    app.querySelector("[data-app-setting='brand']").value = "";
    app.renderAllowedRootRows(["/a", "/b"]);
    globalThis.fetch = vi.fn(async () => ok({
      config: { version: 1, brand: "x", workspace: { allowedRoots: ["/a", "/b"] } },
      path: "/config",
    }));

    await app.saveSettingsForm(new Event("submit"));

    const patchCalls = globalThis.fetch.mock.calls.filter(([, options]) => options?.method === "PATCH");
    expect(patchCalls).toHaveLength(1);
    expect(JSON.parse(patchCalls[0][1].body)).toEqual({ brand: "", workspace: { allowedRoots: ["/a", "/b"] } });
    expect(app.appConfigState.config.workspace.allowedRoots).toEqual(["/a", "/b"]);
  });

  it("sends an empty allowed-roots list to restore unrestricted workspace access", async () => {
    const app = await connectPiApp();
    Object.assign(app, appConfigMethods);
    app.appConfigState = {
      config: { version: 1, brand: "x", workspace: { allowedRoots: ["/a"] } },
      path: "/config",
    };
    app.fillAppConfigForm();
    app.removeAllowedRootRow(app.querySelector("[data-allowed-root-row]"));

    expect(app.appConfigPatchFromForm()).toEqual({ workspace: { allowedRoots: [] } });
  });

  it("keeps app config saves user-level when pi settings scope changes", async () => {
    const app = await connectPiApp();
    app.apiConnected = true;
    app.dataset.activeWorkspaceId = "w1";
    app.settingsState = { project: {}, global: {}, effective: {}, paths: {} };
    app.appConfigState = { config: { version: 1, brand: "pi.dev / web" }, path: "/config" };
    Object.assign(app, appConfigMethods);
    app.fillAppConfigForm();
    app.querySelector("[data-app-setting='brand']").value = "Custom";
    globalThis.fetch = vi.fn(async (url, options = {}) => {
      if (options.method === "PATCH") return ok({ config: { version: 1, brand: "Custom" }, path: "/config" });
      return ok({ settings: { project: {}, global: {}, effective: {}, paths: {} } });
    });

    await app.saveSettingsForm({ preventDefault: vi.fn() });
    app.querySelector("[data-settings-scope]").value = "global";
    await app.saveSettingsForm({ preventDefault: vi.fn() });

    const patchCalls = globalThis.fetch.mock.calls.filter(([, options]) => options?.method === "PATCH");
    expect(patchCalls).toHaveLength(1);
    expect(patchCalls.map(([, options]) => JSON.parse(options.body))).toEqual([{ brand: "Custom" }]);
    expect(globalThis.fetch.mock.calls.some(([, options]) => options?.method === "PUT")).toBe(false);
  });

  it("renders a saved brand as literal text", async () => {
    const app = await connectPiApp();
    app.apiConnected = true;
    app.settingsState = { project: {}, global: {}, effective: {}, paths: {} };
    app.appConfigState = { config: { version: 1, brand: "pi.dev / web" }, path: "/config" };
    Object.assign(app, appConfigMethods);
    app.fillAppConfigForm();
    const unsafe = "<img src=x onerror=alert(1)>";
    app.querySelector("[data-app-setting='brand']").value = unsafe;
    globalThis.fetch = vi.fn(async () => ok({ config: { version: 1, brand: unsafe }, path: "/config" }));

    await app.saveSettingsForm({ preventDefault: vi.fn() });

    expect(app.querySelector("[data-brand]").textContent).toBe(unsafe);
    expect(app.querySelector("[data-brand] img")).toBeNull();
  });

  it("renders brand as text rather than HTML", async () => {
    const app = await connectPiApp();
    Object.assign(app, appConfigMethods);

    app.applyBrand("<b>unsafe</b>");

    expect(app.querySelector("[data-brand]").textContent).toBe("<b>unsafe</b>");
    expect(app.querySelector("[data-brand] b")).toBeNull();
    expect(document.title).toBe("<b>unsafe</b>");
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
    Object.assign(app, appConfigMethods);
    app.appConfigState = { config: { version: 1, brand: "pi.dev / web" }, path: "/config" };
    app.fillAppConfigForm();
    input.value = "next";
    globalThis.fetch = vi.fn(async () => ok({
      config: { version: 1, brand: "next" },
      path: "/config",
    }));

    const response = await saveAppConfig(app.appConfigPatchFromForm());

    expect(response.config.brand).toBe("next");
    expect(globalThis.fetch.mock.calls[0][0]).toBe("http://backend.test/api/app-config");
    expect(globalThis.fetch.mock.calls[0][1]).toMatchObject({ method: "PATCH", body: '{"brand":"next"}' });
  });

  it("fills the default upload limit in MiB", async () => {
    const app = await connectPiApp();
    Object.assign(app, appConfigMethods);
    app.appConfigState = { config: { version: 1, brand: "saved" }, path: "/config" };

    app.fillAppConfigForm();

    expect(app.querySelector("[data-app-setting='max-upload-mib']").value).toBe("100");
  });

  it("fills a saved upload limit after converting bytes to MiB", async () => {
    const app = await connectPiApp();
    Object.assign(app, appConfigMethods);
    app.appConfigState = {
      config: { version: 1, brand: "saved", workspace: { maxUploadBytes: 268435456 } },
      path: "/config",
    };

    app.fillAppConfigForm();

    expect(app.querySelector("[data-app-setting='max-upload-mib']").value).toBe("256");
  });

  it("keeps the upload input constraints and describes its hint", async () => {
    const app = await connectPiApp();
    const input = app.querySelector("[data-app-setting='max-upload-mib']");
    const hint = app.querySelector("#max-upload-mib-hint");

    expect(input.getAttribute("min")).toBe("1");
    expect(input.getAttribute("max")).toBe("2048");
    expect(input.getAttribute("step")).toBe("1");
    expect(input.getAttribute("aria-describedby")).toBe("max-upload-mib-hint");
    expect(hint?.textContent).toBe("MiB；1–2048，保存后立即生效");
  });

  it("keeps existing allowed roots when only the upload limit changes", async () => {
    const app = await connectPiApp();
    Object.assign(app, appConfigMethods);
    app.appConfigState = {
      config: { version: 1, brand: "saved", workspace: { allowedRoots: ["/a", "/b"], maxUploadBytes: 104857600 } },
      path: "/config",
    };

    app.fillAppConfigForm();
    app.querySelector("[data-app-setting='max-upload-mib']").value = "256";

    expect(app.appConfigPatchFromForm()).toEqual({ workspace: { maxUploadBytes: 268435456 } });
  });

  it.each(["1", "2048"])("accepts upload limit boundary %s MiB", async (value) => {
    const app = await connectPiApp();
    Object.assign(app, appConfigMethods);
    app.appConfigState = {
      config: { version: 1, brand: "saved", workspace: { maxUploadBytes: 104857600 } },
      path: "/config",
    };
    app.fillAppConfigForm();
    app.querySelector("[data-app-setting='max-upload-mib']").value = value;

    expect(app.appConfigPatchFromForm()).toEqual({
      workspace: { maxUploadBytes: Number(value) * 1024 * 1024 },
    });
  });

  it("rejects decimal upload limits", async () => {
    const app = await connectPiApp();
    Object.assign(app, appConfigMethods);
    app.appConfigState = {
      config: { version: 1, brand: "saved", workspace: { maxUploadBytes: 104857600 } },
      path: "/config",
    };
    app.fillAppConfigForm();
    app.querySelector("[data-app-setting='max-upload-mib']").value = "1.5";

    expect(() => app.appConfigPatchFromForm()).toThrow("上传上限必须是 1–2048 MiB 的整数");
  });

  it("converts a changed upload limit to bytes without dropping existing workspace patches", async () => {
    const app = await connectPiApp();
    Object.assign(app, appConfigMethods);
    app.appConfigState = {
      config: { version: 1, brand: "saved", workspace: { allowedRoots: ["/a"], maxUploadBytes: 104857600 } },
      path: "/config",
    };

    app.fillAppConfigForm();
    app.querySelector("[data-app-setting='brand']").value = "next";
    app.renderAllowedRootRows(["/a", "/b"]);
    app.querySelector("[data-app-setting='max-upload-mib']").value = "256";

    expect(app.appConfigPatchFromForm()).toEqual({
      brand: "next",
      workspace: { allowedRoots: ["/a", "/b"], maxUploadBytes: 268435456 },
    });
  });

  it.each(["", "0", "2049", "not-a-number"])(
    "rejects an upload limit value of %j",
    async (value) => {
      const app = await connectPiApp();
      Object.assign(app, appConfigMethods);
      app.appConfigState = {
        config: { version: 1, brand: "saved", workspace: { maxUploadBytes: 104857600 } },
        path: "/config",
      };
      app.fillAppConfigForm();
      app.querySelector("[data-app-setting='max-upload-mib']").value = value;

      expect(() => app.appConfigPatchFromForm()).toThrow("上传上限必须是 1–2048 MiB 的整数");
    },
  );

  it("shows a clear upload-limit error without sending an invalid patch", async () => {
    const app = await connectPiApp();
    Object.assign(app, appConfigMethods);
    app.apiConnected = true;
    app.appConfigState = {
      config: { version: 1, brand: "saved", workspace: { maxUploadBytes: 104857600 } },
      path: "/config",
    };
    app.fillAppConfigForm();
    app.querySelector("[data-app-setting='max-upload-mib']").value = "0";
    globalThis.fetch = vi.fn(async () => ok({ config: {}, path: "/config" }));

    await app.saveSettingsForm({ preventDefault: vi.fn() });

    expect(app.querySelector("[data-settings-status]").textContent).toBe("上传上限必须是 1–2048 MiB 的整数");
    expect(globalThis.fetch).not.toHaveBeenCalled();
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
    expect(document.title).toBe(DEFAULT_APP_BRAND);
    app.applyBrand(42);
    expect(app.querySelector("[data-brand]").textContent).toBe(DEFAULT_APP_BRAND);
    expect(document.title).toBe(DEFAULT_APP_BRAND);
  });

  it("handles missing app config controls without throwing", async () => {
    const app = await connectPiApp();
    Object.assign(app, appConfigMethods);
    app.querySelector("[data-brand]").remove();
    app.querySelector("[data-app-setting='brand']").remove();

    app.applyBrand("next");
    app.fillAppConfigForm();

    expect(document.title).toBe("next");
    expect(app.appConfigPatchFromForm()).toEqual({});
  });

  it("tracks the allowed-root dialog trigger with aria-expanded", async () => {
    const app = await connectPiApp();
    const settings = app.querySelector("[data-settings-modal]");
    const addButton = app.querySelector("[data-allowed-root-add]");
    app.apiConnected = true;
    app.appConfigState = { config: { workspace: { allowedRoots: [] } } };
    settings.removeAttribute("hidden");
    app.fillAppConfigForm();
    globalThis.fetch = vi.fn(async () => ok({ path: "/selected", folders: [] }));

    expect(addButton.getAttribute("aria-haspopup")).toBe("dialog");
    expect(addButton.getAttribute("aria-expanded")).toBe("false");
    addButton.click();
    await vi.waitFor(() => expect(app.currentFolder).toBe("/selected"));
    expect(addButton.getAttribute("aria-expanded")).toBe("true");

    app.closeFolderPopover();
    expect(addButton.getAttribute("aria-expanded")).toBe("false");
  });

  it("returns from the allowed-root popover to settings on cancel and completion", async () => {
    const app = await connectPiApp();
    const settings = app.querySelector("[data-settings-modal]");
    const addButton = app.querySelector("[data-allowed-root-add]");
    app.apiConnected = true;
    app.dataset.route = "workspace";
    app.appConfigState = {
      config: { version: 1, brand: "x", workspace: { allowedRoots: ["/existing"] } },
      path: "/config",
    };
    settings.removeAttribute("hidden");
    app.fillAppConfigForm();
    globalThis.fetch = vi.fn(async () => ok({ path: "/selected", folders: [] }));

    addButton.click();
    await vi.waitFor(() => expect(app.currentFolder).toBe("/selected"));
    expect(settings.hidden).toBe(true);
    expect(app.dataset.route).toBe("workspace");
    expect(app.querySelector('[data-view="picker"]')).toBeNull();

    app.closeFolderPopover();
    expect(settings.hidden).toBe(false);
    expect(document.activeElement).toBe(addButton);
    expect(app.querySelectorAll("[data-allowed-root-row]")).toHaveLength(1);

    addButton.click();
    await vi.waitFor(() => {
      expect(globalThis.fetch).toHaveBeenCalledTimes(2);
      expect(app.folderLoadPending).toBe(false);
    });
    app.querySelector("[data-action='folder-open-current']").click();

    expect(settings.hidden).toBe(false);
    expect(app.dataset.route).toBe("workspace");
    expect(app.querySelector('[data-view="picker"]')).toBeNull();
    expect(app.querySelectorAll("[data-allowed-root-row]")).toHaveLength(2);
    expect(document.activeElement).toBe(addButton);
  });

  it("uses the saved trigger rect for zero-coordinate keyboard activation", async () => {
    const app = await connectPiApp();
    const settings = app.querySelector("[data-settings-modal]");
    const addButton = app.querySelector("[data-allowed-root-add]");
    const popover = app.querySelector("[data-folder-popover]");
    app.apiConnected = true;
    app.appConfigState = { config: { version: 1, brand: "x", workspace: { allowedRoots: [] } }, path: "/config" };
    settings.removeAttribute("hidden");
    app.fillAppConfigForm();
    Object.defineProperty(window, "innerWidth", { configurable: true, value: 800 });
    Object.defineProperty(window, "innerHeight", { configurable: true, value: 600 });
    Object.defineProperty(popover, "offsetWidth", { configurable: true, value: 300 });
    Object.defineProperty(popover, "offsetHeight", { configurable: true, value: 200 });
    addButton.getBoundingClientRect = () => settings.hidden
      ? { left: 0, top: 0, right: 0, bottom: 0, width: 0, height: 0 }
      : { left: 72, top: 96, right: 160, bottom: 128, width: 88, height: 32 };
    globalThis.fetch = vi.fn(async () => ok({ path: "/selected", folders: [] }));

    addButton.dispatchEvent(new MouseEvent("click", { bubbles: true, clientX: 0, clientY: 0 }));
    await vi.waitFor(() => expect(app.currentFolder).toBe("/selected"));

    expect(popover.style.left).toBe("72px");
    expect(popover.style.top).toBe("136px");
  });

  it("exposes the app config GET API wrapper", async () => {
    globalThis.fetch = vi.fn(async () => ok({ config: {}, path: "/config" }));

    await getAppConfig();

    expect(globalThis.fetch.mock.calls[0][0]).toBe("http://backend.test/api/app-config");
  });

  it("exposes the admin password status API wrapper", async () => {
    globalThis.fetch = vi.fn(async () => ok({ configured: true }));

    await getAdminPasswordStatus();

    expect(globalThis.fetch.mock.calls[0][0]).toBe("http://backend.test/api/admin/password/status");
  });
});
