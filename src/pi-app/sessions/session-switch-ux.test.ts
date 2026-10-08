// @ts-nocheck
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { applyUiLocale } from "../../i18n/client";
import { UI_LOCALE_STORAGE_KEY } from "../../i18n/locales";
import { cleanupPiAppFixture, connectPiApp, installPiAppFixture } from "../test-helper";

describe("pi-app session switch UX", () => {
  beforeEach(() => {
    installPiAppFixture();
    globalThis.PI_WEB_API_BASE = "http://backend.test";
  });

  afterEach(cleanupPiAppFixture);

  it("localizes the transcript loading placeholder while a session loads", async () => {
    localStorage.setItem(UI_LOCALE_STORAGE_KEY, "en");
    let resolveFetch;
    globalThis.fetch = vi.fn(() => new Promise((resolve) => {
      resolveFetch = resolve;
    }));
    const app = await connectPiApp();
    app.connectDurableEvents = vi.fn();
    app.stopDurableStream = vi.fn();
    const sessionTitle = "build $& session";
    app.append(app.createSessionRow("w1", { id: "s1", title: sessionTitle, lastUsed: "now" }));

    const loading = app.loadSession("s1");

    expect(app.stopDurableStream).toHaveBeenCalledTimes(1);
    expect(app.querySelector(".session-switch-loading")).not.toBeNull();
    const label = app.querySelector(".session-switch-label");
    expect(label.textContent).toBe("Loading build $& session…");

    applyUiLocale("zh-CN", app);
    expect(label.textContent).toBe("正在加载 build $& session…");

    resolveFetch({
      ok: true,
      status: 200,
      statusText: "OK",
      json: async () => ({
        session: { id: "s1", title: "large session", workspaceId: "w1" },
        metadata: { title: "large session", workspaceId: "w1" },
        items: [{ itemId: "loaded", ordinal: 1, kind: "pi", origin: "runtime", text: "loaded", bodyPreview: "loaded" }],
        status: "idle", seq: "0", cursor: "", hasMore: false, limit: 120,
        projectionSchemaVersion: 1, projectionGeneration: "1",
      }),
    });
    await loading;

    expect(app.querySelector(".session-switch-loading")).toBeNull();
    expect(app.querySelector(".msg .body").textContent).toBe("loaded");
  });
});
