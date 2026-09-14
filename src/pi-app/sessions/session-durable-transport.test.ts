// @ts-nocheck
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanupPiAppFixture, connectPiApp, installPiAppFixture } from "../test-helper";

class FakeEventSource {
  static instances = [];
  constructor(url) {
    this.url = url;
    this.listeners = new Map();
    this.closed = false;
    FakeEventSource.instances.push(this);
  }
  addEventListener(type, listener) {
    if (!this.listeners.has(type)) this.listeners.set(type, []);
    this.listeners.get(type).push(listener);
  }
  dispatch(type, payload) {
    for (const listener of this.listeners.get(type) || []) {
      listener({ data: JSON.stringify(payload), lastEventId: "" });
    }
  }
  close() {
    this.closed = true;
  }
}

function durableState() {
  return {
    session: { id: "s1", title: "durable", workspaceId: "w1" },
    items: [
      { itemId: "tool:run-1:call-1", ordinal: 1, kind: "tool", origin: "runtime", runId: "run-1", entityId: "tool:run-1:call-1", tool: "bash", toolCallId: "call-1", args: "pwd", text: "", bodyPreview: "partial", status: "running" },
    ],
    status: "running",
    runId: "run-1",
    seq: "3",
    cursor: "",
    hasMore: false,
    limit: 120,
    projectionSchemaVersion: 1,
    projectionGeneration: "1",
  };
}

describe("durable session transport", () => {
  beforeEach(() => {
    installPiAppFixture();
    globalThis.PI_WEB_API_BASE = "http://backend.test";
    FakeEventSource.instances = [];
    globalThis.EventSource = FakeEventSource;
    globalThis.fetch = vi.fn(async (url) => {
      if (String(url).includes("/state")) {
        return { ok: true, status: 200, statusText: "OK", json: async () => durableState() };
      }
      if (String(url).includes("/api/health")) {
        return { ok: true, status: 200, statusText: "OK", json: async () => ({ ok: true, durableEvents: true }) };
      }
      return { ok: true, status: 200, statusText: "OK", json: async () => ({ workspaces: [] }) };
    });
  });

  afterEach(cleanupPiAppFixture);

  it("renders a durable snapshot and resumes from its sequence", async () => {
    const app = await connectPiApp();
    app.durableEvents = true;
    app.connectEvents = vi.fn();
    await app.loadSession("s1");

    const card = app.querySelector(".tool-card");
    expect(card).not.toBeNull();
    expect(card.dataset.toolCallId).toBe("call-1");
    expect(app.transcriptState.appliedSeq).toBe("3");

    const source = FakeEventSource.instances.at(-1);
    expect(source.url).toContain("/api/sessions/s1/events-v2");
    expect(source.url).toContain("after=3");

    source.dispatch("session.event", {
      seq: "4",
      sessionId: "s1",
      runId: "run-1",
      type: "tool.finished",
      entityId: "tool:run-1:call-1",
      payload: { body: "final output", status: "ok" },
    });

    expect(app.querySelectorAll(".tool-card")).toHaveLength(1);
    expect(app.querySelector(".tool-card").dataset.status).toBe("ok");
    expect(app.querySelector(".tool-card .tc-body").textContent).toContain("final output");
    expect(app.transcriptState.appliedSeq).toBe("4");
  });

  it("renders assistant output from events after a durable prompt", async () => {
    const app = await connectPiApp();
    app.durableEvents = true;
    app.apiConnected = true;
    app.dataset.activeSessionId = "s1";
    app.prompt.value = "hello";
    app.connectEvents = vi.fn();
    globalThis.fetch = vi.fn(async (url) => {
      if (String(url).includes("/state")) {
        return { ok: true, status: 200, statusText: "OK", json: async () => ({ ...durableState(), items: [], status: "idle", runId: undefined, seq: "0" }) };
      }
      if (String(url).includes("/prompt")) {
        return { ok: true, status: 202, statusText: "Accepted", json: async () => ({ accepted: true, runId: "run-2" }) };
      }
      return { ok: true, status: 200, statusText: "OK", json: async () => ({ workspaces: [] }) };
    });
    await app.loadSession("s1");
    await app.submitPrompt();
    const source = FakeEventSource.instances.at(-1);
    source.dispatch("session.event", { seq: "1", sessionId: "s1", type: "user.message.created", sourceId: "command:req:user", payload: { text: "hello" } });
    source.dispatch("session.event", { seq: "2", sessionId: "s1", runId: "run-2", type: "run.started", payload: {} });
    source.dispatch("session.event", { seq: "3", sessionId: "s1", runId: "run-2", type: "assistant.segment.started", entityId: "assistant:run-2:1", payload: { kind: "pi" } });
    source.dispatch("session.event", { seq: "4", sessionId: "s1", runId: "run-2", type: "assistant.segment.delta", entityId: "assistant:run-2:1", payload: { kind: "pi", delta: "answer" } });
    source.dispatch("session.event", { seq: "5", sessionId: "s1", runId: "run-2", type: "assistant.segment.committed", entityId: "assistant:run-2:1", payload: { kind: "pi", text: "answer" } });
    source.dispatch("session.event", { seq: "6", sessionId: "s1", runId: "run-2", type: "run.finished", payload: {} });

    expect(app.querySelector(".msg[data-kind='pi'] .body").textContent).toContain("answer");
  });

  it("loads older durable projection pages with the durable cursor", async () => {
    const app = await connectPiApp();
    app.durableEvents = true;
    app.connectEvents = vi.fn();
    globalThis.fetch = vi.fn(async (url) => {
      if (String(url).includes("before=cursor-1")) {
        return { ok: true, status: 200, statusText: "OK", json: async () => ({
          sessionId: "s1",
          items: [{ itemId: "user:old", ordinal: 0, kind: "user", origin: "runtime", text: "older" }],
          status: "running",
          seq: "3",
          cursor: "",
          hasMore: false,
          limit: 120,
          projectionSchemaVersion: 1,
          projectionGeneration: "1",
        }) };
      }
      return { ok: true, status: 200, statusText: "OK", json: async () => ({ ...durableState(), cursor: "cursor-1", hasMore: true }) };
    });
    await app.loadSession("s1");
    await app.loadOlderSessionMessages();

    expect(globalThis.fetch).toHaveBeenCalledWith(
      "http://backend.test/api/sessions/s1/state?limit=120&before=cursor-1",
      expect.anything(),
    );
    expect(app.transcriptState.itemOrder[0]).toBe("user:old");
    expect(app.querySelector(".msg .body").textContent).toContain("older");
  });

  it("resyncs when the stream resets", async () => {
    const app = await connectPiApp();
    app.durableEvents = true;
    app.connectEvents = vi.fn();
    await app.loadSession("s1");
    app.loadSession = vi.fn();
    const source = FakeEventSource.instances.at(-1);
    source.dispatch("stream.reset", { type: "stream.reset", payload: { reason: "cursor_expired" } });
    expect(app.loadSession).toHaveBeenCalledWith("s1", expect.objectContaining({ resync: true }));
  });
});
