// @ts-nocheck
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import "../index";

const nativeEventSource = globalThis.EventSource;
const nativeFetch = globalThis.fetch;

function durableState(id, text = "", overrides = {}) {
  return {
    session: { id, title: id, workspaceId: "w1" },
    metadata: { title: id, workspaceId: "w1" },
    items: text ? [{ itemId: `message:${id}`, ordinal: 1, kind: "pi", origin: "runtime", text, bodyPreview: text }] : [],
    status: "idle",
    seq: "0",
    cursor: "",
    hasMore: false,
    limit: 120,
    projectionSchemaVersion: 1,
    projectionGeneration: "1",
    ...overrides,
  };
}

function appFixture() {
  document.body.innerHTML = `
    <pi-app data-tree="on" data-sidebar="open" data-active-workspace-id="w1">
      <span class="statusbtn"></span>
      <span data-active-session-title></span>
      <main data-main="session"><div class="term"><div class="term-inner"></div><div class="streaming-tail" data-streaming-tail></div></div></main>
      <main data-main="empty" hidden></main>
      <div class="prompt-region">
        <div class="slash-pop" hidden></div>
        <div class="attach-chips" hidden></div>
        <textarea class="prompt-textarea"></textarea>
        <button class="stop-btn" hidden>stop</button>
        <button class="send-btn" disabled>send</button>
        <button class="attach-btn">attach</button>
        <input data-file-input type="file" />
        <div class="prompt-meta" data-prompt-meta></div>
      </div>
    </pi-app>
  `;
  return document.querySelector("pi-app");
}

async function connectedApp() {
  const app = appFixture();
  await customElements.whenDefined("pi-app");
  app.bootstrapAPI = vi.fn();
  app.startRuntimeStatusPolling = vi.fn();
  app.startSpinners = vi.fn();
  app.connectedCallback();
  return app;
}

describe("pi-app session isolation", () => {
  beforeEach(() => {
    globalThis.PI_WEB_API_BASE = "http://backend.test";
  });

  afterEach(() => {
    vi.restoreAllMocks();
    delete globalThis.PI_WEB_API_BASE;
    globalThis.EventSource = nativeEventSource;
    globalThis.fetch = nativeFetch;
    document.body.innerHTML = "";
  });

  it("ignores events that belong to a different session", async () => {
    const app = await connectedApp();
    app.dataset.activeSessionId = "s2";
    app.renderMessages([]);

    app.applyEvent({
      type: "session.message",
      sessionId: "s1",
      payload: { kind: "pi", text: "old session output" },
    });

    expect(app.querySelector(".msg")).toBeNull();
  });

  it("keeps the latest session load when older requests finish later", async () => {
    let resolveFirst;
    globalThis.fetch = vi.fn((url) => {
      if (String(url).includes("/api/sessions/s1/state")) {
        return new Promise((resolve) => {
          resolveFirst = resolve;
        });
      }
      return Promise.resolve({
        ok: true,
        status: 200,
        statusText: "OK",
        json: async () => durableState("s2", "second session"),
      });
    });
    const app = await connectedApp();
    app.connectDurableEvents = vi.fn();

    const firstLoad = app.loadSession("s1");
    await app.loadSession("s2");
    resolveFirst({
      ok: true,
      status: 200,
      statusText: "OK",
      json: async () => durableState("s1", "first session"),
    });
    await firstLoad;

    expect(app.dataset.activeSessionId).toBe("s2");
    expect(app.querySelector(".msg .body").textContent).toBe("second session");
    expect(app.connectDurableEvents).toHaveBeenCalledTimes(1);
    expect(app.connectDurableEvents).toHaveBeenCalledWith("s2", "0");
  });

  it("drops a session load that predates a new prompt generation", async () => {
    let resolveLoad;
    globalThis.fetch = vi.fn(() => new Promise((resolve) => {
      resolveLoad = resolve;
    }));
    const app = await connectedApp();
    app.dataset.activeSessionId = "s1";
    const oldLoad = app.loadSession("s1");
    app.sessionViewGeneration = Symbol("new-session-view");
    app.termInner.replaceChildren();
    app.termInner.textContent = "new prompt";
    resolveLoad({
      ok: true,
      status: 200,
      statusText: "OK",
      json: async () => durableState("s1", "stale response"),
    });
    await oldLoad;

    expect(app.termInner.textContent).toBe("new prompt");
  });

  it("ignores stale older-page responses after a session switch", async () => {
    globalThis.PI_WEB_API_BASE = "http://backend.test";
    let resolveOlder;
    globalThis.fetch = vi.fn(() => new Promise((resolve) => {
      resolveOlder = resolve;
    }));
    const app = await connectedApp();
    app.dataset.activeSessionId = "s1";
    app.sessionHistoryCursor = "cursor";
    app.sessionHistoryHasMore = true;

    const olderLoad = app.loadOlderSessionMessages();
    app.dataset.activeSessionId = "s2";
    resolveOlder({
      ok: true,
      status: 200,
      statusText: "OK",
      json: async () => durableState("s1", "stale", { cursor: "old" }),
    });
    await olderLoad;

    expect(app.transcriptItems || []).toHaveLength(0);
  });

  it("resets transient running state when loading another session", async () => {
    globalThis.fetch = vi.fn(async () => ({
      ok: true,
      status: 200,
      statusText: "OK",
      json: async () => durableState("s2"),
    }));
    const app = await connectedApp();
    app.connectDurableEvents = vi.fn();
    app.dataset.activeSessionId = "s1";
    app.setMode("running");

    await app.loadSession("s2");

    expect(app.running).toBe(false);
    expect(app.querySelector(".msg.loading")).toBeNull();
    expect(app.querySelector(".stop-btn").hidden).toBe(true);
  });

  it("restores running controls from loaded session status", async () => {
    globalThis.fetch = vi.fn(async () => ({
      ok: true,
      status: 200,
      statusText: "OK",
      json: async () => durableState("s2", "", { status: "running" }),
    }));
    const app = await connectedApp();
    app.connectDurableEvents = vi.fn();

    await app.loadSession("s2");

    expect(app.running).toBe(true);
    expect(app.querySelector(".stop-btn").hidden).toBe(false);
    expect(app.querySelector(".msg.loading .spinner")).not.toBeNull();
  });

});
