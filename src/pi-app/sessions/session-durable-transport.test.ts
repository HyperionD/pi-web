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
    expect(app.querySelector(".msg[data-kind='user'] .body").textContent).toContain("hello");
    expect(app.querySelectorAll(".msg[data-kind='user']")).toHaveLength(1);
    const source = FakeEventSource.instances.at(-1);
    source.dispatch("session.event", { seq: "1", sessionId: "s1", type: "user.message.created", sourceId: "command:req:user", payload: { text: "hello" } });
    expect(app.querySelectorAll(".msg[data-kind='user']")).toHaveLength(1);
    source.dispatch("session.event", { seq: "2", sessionId: "s1", runId: "run-2", type: "run.started", payload: {} });
    source.dispatch("session.event", { seq: "3", sessionId: "s1", runId: "run-2", type: "assistant.segment.started", entityId: "assistant:run-2:1", payload: { kind: "pi" } });
    source.dispatch("session.event", { seq: "4", sessionId: "s1", runId: "run-2", type: "assistant.segment.delta", entityId: "assistant:run-2:1", payload: { kind: "pi", delta: "answer" } });
    source.dispatch("session.event", { seq: "5", sessionId: "s1", runId: "run-2", type: "assistant.segment.committed", entityId: "assistant:run-2:1", payload: { kind: "pi", text: "answer" } });
    source.dispatch("session.event", { seq: "6", sessionId: "s1", runId: "run-2", type: "run.finished", payload: {} });

    expect(app.querySelector(".msg[data-kind='pi'] .body").textContent).toContain("answer");
  });

  it("does not render a committed thinking segment as agent text", async () => {
    const app = await connectPiApp();
    app.durableEvents = true;
    app.connectEvents = vi.fn();
    globalThis.fetch = vi.fn(async (url) => String(url).includes("/state")
      ? { ok: true, status: 200, statusText: "OK", json: async () => ({ ...durableState(), items: [], status: "running", runId: "run-1", seq: "0" }) }
      : { ok: true, status: 200, statusText: "OK", json: async () => ({}) });
    await app.loadSession("s1");
    const source = FakeEventSource.instances.at(-1);

    source.dispatch("session.event", { seq: "1", sessionId: "s1", runId: "run-1", type: "assistant.segment.started", entityId: "thinking:run-1:1", payload: { kind: "think" } });
    source.dispatch("session.event", { seq: "2", sessionId: "s1", runId: "run-1", type: "assistant.segment.delta", entityId: "thinking:run-1:1", payload: { kind: "think", delta: "plan" } });
    source.dispatch("session.event", { seq: "3", sessionId: "s1", runId: "run-1", type: "assistant.segment.committed", entityId: "thinking:run-1:1", payload: { kind: "think", text: "plan" } });

    expect(app.querySelector(".msg[data-kind='pi'] .body")?.textContent || "").not.toContain("plan");
    expect(app.querySelector(".msg[data-kind='think'] .body")?.textContent || "").toContain("plan");
  });

  it("immediately shows a repeated user prompt exactly once", async () => {
    const priorUser = { itemId: "user:prior", ordinal: 1, kind: "user", origin: "runtime", text: "hello" };
    globalThis.fetch = vi.fn(async (url) => {
      if (String(url).includes("/state")) {
        const state = { ...durableState(), items: [priorUser], status: "idle", runId: undefined, seq: "1" };
        return { ok: true, status: 200, statusText: "OK", json: async () => state };
      }
      return { ok: true, status: 202, statusText: "Accepted", json: async () => ({ accepted: true, runId: "run-2" }) };
    });
    const app = await connectPiApp();
    app.durableEvents = true;
    app.apiConnected = true;
    app.connectEvents = vi.fn();
    await app.loadSession("s1");
    app.prompt.value = "hello";

    await app.submitPrompt();
    expect(app.querySelectorAll(".msg[data-kind='user']")).toHaveLength(2);
    FakeEventSource.instances.at(-1).dispatch("session.event", {
      seq: "2", sessionId: "s1", type: "user.message.created",
      sourceId: "command:req:user", payload: { text: "hello" },
    });
    expect(app.querySelectorAll(".msg[data-kind='user']")).toHaveLength(2);
  });

  it("reports durable run failures without a completion notification", async () => {
    const app = await connectPiApp();
    app.durableEvents = true;
    app.connectEvents = vi.fn();
    await app.loadSession("s1");
    app.notifyResponseFailure = vi.fn(() => { app.responseFailureToastShown = true; });
    app.notifyResponseCompletedOnce = vi.fn();
    const source = FakeEventSource.instances.at(-1);

    source.dispatch("session.event", {
      seq: "4",
      sessionId: "s1",
      runId: "run-1",
      type: "run.failed",
      payload: { error: "runtime failed" },
    });

    expect(app.notifyResponseFailure).toHaveBeenCalledWith("runtime failed");
    expect(app.notifyResponseCompletedOnce).not.toHaveBeenCalled();
  });

  it("applies a terminal event once when SSE repeats it", async () => {
    const app = await connectPiApp();
    app.durableEvents = true;
    app.connectEvents = vi.fn();
    await app.loadSession("s1");
    app.finishRunningTools = vi.fn();
    app.commitStreamingMessages = vi.fn();
    app.removeLoadingMessage = vi.fn();
    app.setMode = vi.fn();
    const source = FakeEventSource.instances.at(-1);
    const terminal = { seq: "4", sessionId: "s1", runId: "run-1", type: "run.finished", payload: {} };

    source.dispatch("session.event", terminal);
    source.dispatch("session.event", terminal);

    expect(app.finishRunningTools).toHaveBeenCalledTimes(1);
    expect(app.commitStreamingMessages).toHaveBeenCalledTimes(1);
    expect(app.removeLoadingMessage).toHaveBeenCalledTimes(1);
    expect(app.setMode).toHaveBeenCalledTimes(1);
    expect(app.setMode).toHaveBeenCalledWith("idle");
  });

  it("ignores stale terminal events without completion side effects", async () => {
    const app = await connectPiApp();
    app.durableEvents = true;
    app.connectEvents = vi.fn();
    await app.loadSession("s1");
    app.setMode = vi.fn();
    const source = FakeEventSource.instances.at(-1);

    source.dispatch("session.event", {
      seq: "3",
      sessionId: "s1",
      runId: "run-1",
      type: "run.finished",
      payload: {},
    });

    expect(app.setMode).not.toHaveBeenCalled();
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
    const source = FakeEventSource.instances.at(-1);
    await app.loadOlderSessionMessages();

    expect(source.closed).toBe(false);
    expect(globalThis.fetch).toHaveBeenCalledWith(
      "http://backend.test/api/sessions/s1/state?limit=120&before=cursor-1",
      expect.anything(),
    );
    expect(app.transcriptState.itemOrder[0]).toBe("user:old");
    expect(app.querySelector(".msg .body").textContent).toContain("older");
  });

  it("restores the durable transcript and stream after a resync fetch failure", async () => {
    let stateCalls = 0;
    globalThis.fetch = vi.fn(async (url) => {
      if (String(url).includes("/state")) {
        stateCalls += 1;
        if (stateCalls === 2) throw new Error("state temporarily unavailable");
        return { ok: true, status: 200, statusText: "OK", json: async () => durableState() };
      }
      return { ok: true, status: 202, statusText: "Accepted", json: async () => ({ accepted: true }) };
    });
    const app = await connectPiApp();
    app.durableEvents = true;
    app.setConnection = vi.fn();
    await app.loadSession("s1");
    const previousSource = FakeEventSource.instances.at(-1);

    await app.loadSession("s1", { resync: true });

    expect(previousSource.closed).toBe(true);
    expect(app.querySelector(".tool-card")).not.toBeNull();
    expect(FakeEventSource.instances.at(-1)).not.toBe(previousSource);
    expect(app.setConnection).toHaveBeenCalledWith("err");
  });

  it("resyncs durable state when the stop request does not cancel the run", async () => {
    let stateCalls = 0;
    globalThis.fetch = vi.fn(async (url) => {
      const target = String(url);
      if (target.includes("/state")) {
        stateCalls += 1;
        const status = stateCalls === 1 ? "running" : "idle";
        return { ok: true, status: 200, statusText: "OK", json: async () => ({ ...durableState(), status, runId: status === "running" ? "run-1" : undefined }) };
      }
      if (target.includes("/cancel")) return { ok: true, status: 200, statusText: "OK", json: async () => ({ cancelled: false }) };
      return { ok: true, status: 200, statusText: "OK", json: async () => ({ workspaces: [] }) };
    });
    const app = await connectPiApp();
    app.durableEvents = true;
    await app.loadSession("s1");
    expect(app.running).toBe(true);

    await app.cancelActiveSession();

    expect(app.running).toBe(false);
  });

  it("resyncs durable state when the stop request fails", async () => {
    let stateCalls = 0;
    globalThis.fetch = vi.fn(async (url) => {
      const target = String(url);
      if (target.includes("/state")) {
        stateCalls += 1;
        const status = stateCalls === 1 ? "running" : "idle";
        return { ok: true, status: 200, statusText: "OK", json: async () => ({ ...durableState(), status, runId: status === "running" ? "run-1" : undefined }) };
      }
      if (target.includes("/cancel")) throw new Error("cancel unavailable");
      return { ok: true, status: 200, statusText: "OK", json: async () => ({ workspaces: [] }) };
    });
    const app = await connectPiApp();
    app.durableEvents = true;
    await app.loadSession("s1");
    expect(app.running).toBe(true);

    await app.cancelActiveSession();

    expect(app.running).toBe(false);
  });

  it("does not fall back to the legacy session when durable state temporarily fails", async () => {
    const requests = [];
    globalThis.fetch = vi.fn(async (url) => {
      requests.push(String(url));
      if (String(url).includes("/state")) throw new Error("state temporarily unavailable");
      return { ok: true, status: 200, statusText: "OK", json: async () => ({ session: { id: "s1" }, messages: [], status: "running" }) };
    });
    const app = await connectPiApp();
    app.durableEvents = true;
    app.setConnection = vi.fn();

    await app.loadSession("s1");

    expect(requests.filter((url) => !url.includes("/state"))).toHaveLength(0);
    expect(app.setConnection).toHaveBeenCalledWith("err");
  });

  it("rejects a durable snapshot for a different session without legacy fallback", async () => {
    const state = { ...durableState(), session: { id: "s2", title: "wrong", workspaceId: "w1" } };
    globalThis.fetch = vi.fn(async () => ({ ok: true, status: 200, statusText: "OK", json: async () => state }));
    const app = await connectPiApp();
    app.durableEvents = true;
    app.setConnection = vi.fn();

    await app.loadSession("s1");

    expect(globalThis.fetch).toHaveBeenCalledTimes(1);
    expect(app.transcriptState).toBeUndefined();
    expect(app.setConnection).toHaveBeenCalledWith("err");
  });

  it("stops the previous durable stream before a session switch loads", async () => {
    const app = await connectPiApp();
    app.durableEvents = true;
    app.connectEvents = vi.fn();
    await app.loadSession("s1");
    const source = FakeEventSource.instances.at(-1);
    let resolveSecond;
    globalThis.fetch = vi.fn(() => new Promise((resolve) => { resolveSecond = resolve; }));

    const loading = app.loadSession("s2");

    expect(source.closed).toBe(true);
    resolveSecond({ ok: true, status: 200, statusText: "OK", json: async () => ({ ...durableState(), session: { id: "s2", workspaceId: "w1" }, sessionId: "s2" }) });
    await loading;
  });

  it("does not let a stale durable load replace the active transcript state", async () => {
    let resolveFirst;
    globalThis.fetch = vi.fn((url) => {
      if (String(url).includes("/sessions/s1/state")) {
        return new Promise((resolve) => { resolveFirst = resolve; });
      }
      const state = { ...durableState(), session: { id: "s2", title: "second", workspaceId: "w1" }, sessionId: "s2" };
      return Promise.resolve({ ok: true, status: 200, statusText: "OK", json: async () => state });
    });
    const app = await connectPiApp();
    app.durableEvents = true;
    app.connectEvents = vi.fn();

    const firstLoad = app.loadSession("s1");
    await app.loadSession("s2");
    resolveFirst({ ok: true, status: 200, statusText: "OK", json: async () => ({ ...durableState(), sessionId: "s1" }) });
    await firstLoad;

    expect(app.transcriptState.sessionId).toBe("s2");
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
