// @ts-nocheck
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { connectPiApp, cleanupPiAppFixture, installPiAppFixture } from "../test-helper";
import { shouldResyncAfterGap } from "./session-recovery";

const nativeEventSource = globalThis.EventSource;

describe("session recovery", () => {
  beforeEach(installPiAppFixture);
  afterEach(() => {
    cleanupPiAppFixture();
    globalThis.EventSource = nativeEventSource;
  });

  it("only treats broker gaps as gap resync events", () => {
    expect(shouldResyncAfterGap({ id: 0, type: "broker_gap", sessionId: "s1", payload: {} })).toBe(true);
    expect(shouldResyncAfterGap({ id: 4, type: "session.message", sessionId: "s1", payload: {} })).toBe(false);
    expect(shouldResyncAfterGap({ id: 0, type: "heartbeat", sessionId: "s1", payload: {} })).toBe(false);
  });

  it("reloads a snapshot after a broker gap without rendering gap text", async () => {
    const app = await connectPiApp();
    app.dataset.activeSessionId = "s1";
    app.loadSession = vi.fn();
    app.appendMessage = vi.fn();

    app.applyEvent({
      id: 0,
      type: "broker_gap",
      sessionId: "s1",
      payload: { sessionId: "s1", after: 3, latestEventId: 8 },
    });

    expect(app.loadSession).toHaveBeenCalledWith("s1", expect.objectContaining({ resync: true }));
    expect(app.appendMessage).not.toHaveBeenCalled();
  });

  it("coalesces open and status resync requests", async () => {
    const sources = [];
    globalThis.EventSource = class {
      constructor() {
        this.listeners = {};
        this.close = vi.fn();
        sources.push(this);
      }
      addEventListener(type, listener) {
        this.listeners[type] = listener;
      }
    };
    const app = await connectPiApp();
    app.dataset.activeSessionId = "s1";
    app.loadSession = vi.fn(() => new Promise(() => {}));
    app.connectEvents("s1");
    sources[0].onopen();
    sources[0].listeners["session.status"]({
      data: JSON.stringify({ id: "8", type: "session.status", sessionId: "s1", payload: { status: "running" } }),
    });
    sources[0].listeners["session.status"]({
      data: JSON.stringify({ id: "9", type: "session.status", sessionId: "s1", payload: { status: "idle" } }),
    });

    expect(app.loadSession).toHaveBeenCalledTimes(1);
  });

  it("does not reload the snapshot for an active status during stream recovery", async () => {
    const app = await connectPiApp();
    app.apiConnected = true;
    app.dataset.activeSessionId = "s1";
    app.loadSession = vi.fn();

    app.applyEvent({
      id: "8",
      type: "session.status",
      sessionId: "s1",
      payload: { status: "thinking" },
    });

    expect(app.loadSession).not.toHaveBeenCalled();
    expect(app.running).toBe(true);
  });

  it("connects after the snapshot cursor with replay enabled", async () => {
    const app = await connectPiApp();
    app.dataset.activeSessionId = "s1";
    app.connectEvents = vi.fn();
    app.applyLoadedSession({ id: "s1" }, [], "idle", { latestEventId: "42" });

    expect(app.connectEvents).toHaveBeenCalledWith("s1", {
      replay: true,
      after: "42",
      reloadOnOpen: false,
    });
  });

  it("uses the active-run cursor when restoring a running snapshot", async () => {
    const app = await connectPiApp();
    app.dataset.activeSessionId = "s1";
    app.connectEvents = vi.fn();
    app.applyLoadedSession(
      { id: "s1" },
      [],
      "running",
      { latestEventId: "42", activeEventCursor: "7", activeRun: true },
    );

    expect(app.sessionEventCursors.get("s1")).toBe("7");
    expect(app.connectEvents).toHaveBeenCalledWith("s1", {
      replay: "active",
      after: "7",
      snapshotAfter: "42",
      reloadOnOpen: false,
    });
  });

  it("resyncs an id-less broker gap despite browser lastEventId inheritance", async () => {
    class FakeEventSource {
      static instance;
      constructor() {
        this.listeners = {};
        this.lastEventId = "";
        FakeEventSource.instance = this;
      }
      addEventListener(type, listener) {
        this.listeners[type] = listener;
      }
      dispatchSse(type, data, id) {
        if (id !== undefined) this.lastEventId = id;
        this.listeners[type]?.({ data: JSON.stringify(data), lastEventId: this.lastEventId });
      }
      close() {}
    }
    globalThis.EventSource = FakeEventSource;
    const app = await connectPiApp();
    app.dataset.activeSessionId = "s1";
    app.loadSession = vi.fn();
    const applyEvent = vi.spyOn(app, "applyEvent");
    app.connectEvents("s1");

    FakeEventSource.instance.dispatchSse("heartbeat", { type: "heartbeat", sessionId: "s1" }, "7");
    FakeEventSource.instance.dispatchSse("heartbeat", { type: "heartbeat", sessionId: "s1" });
    FakeEventSource.instance.dispatchSse("broker_gap", {
      id: 0,
      type: "broker_gap",
      sessionId: "s1",
      payload: { sessionId: "s1", after: "3", latestEventId: "8" },
    });

    expect(app.loadSession).toHaveBeenCalledWith("s1", expect.objectContaining({ resync: true }));
    expect(applyEvent).toHaveBeenCalledTimes(2);
    expect(applyEvent).toHaveBeenLastCalledWith(expect.objectContaining({ type: "broker_gap", id: 0 }));
    expect(app.sessionEventCursors.get("s1")).toBe("7");
  });

  it("keeps a string cursor and reconnects after the snapshot cursor", async () => {
    const sources = [];
    globalThis.EventSource = class {
      constructor(url) {
        this.url = url;
        this.listeners = {};
        this.close = vi.fn();
        sources.push(this);
      }
      addEventListener(type, listener) {
        this.listeners[type] = listener;
      }
    };
    const app = await connectPiApp();
    app.dataset.activeSessionId = "s1";
    app.connectEvents("s1", { after: "9007199254740993" });
    expect(sources[0].url).toContain("after=9007199254740993");
    sources[0].listeners["session.message"]({
      data: JSON.stringify({ id: "9007199254740994", type: "session.message", sessionId: "s1", payload: { kind: "pi", text: "ok" } }),
    });
    expect(app.sessionEventCursors.get("s1")).toBe("9007199254740994");
    expect(app.isDuplicateSessionEvent({ id: "9007199254740993", sessionId: "s1" })).toBe(true);
  });

  it("ignores stale AG-UI cursor callbacks after a new run generation", async () => {
    const app = await connectPiApp();
    app.dataset.activeSessionId = "s1";
    const stale = app.aguiSubscriber("s1");
    const current = app.aguiSubscriber("s1");

    stale.onBrokerEventCursor?.("17");
    current.onBrokerEventCursor?.("19");

    expect(app.sessionEventCursors.get("s1")).toBe("19");
  });

  it("uses the consumed AG-UI cursor for a replaying disconnect reconnect", async () => {
    const sources = [];
    globalThis.EventSource = class {
      constructor(url) {
        this.url = url;
        this.close = vi.fn();
        this.listeners = {};
        sources.push(this);
      }
      addEventListener(type, listener) {
        this.listeners[type] = listener;
      }
    };
    const app = await connectPiApp();
    app.dataset.activeSessionId = "s1";
    const agui = app.aguiSubscriber("s1");
    agui.onRunStarted();
    agui.onBrokerEventCursor("9007199254740993");
    app.connectEvents("s1", { replay: true });

    expect(app.sessionEventCursors.get("s1")).toBe("9007199254740993");
    expect(sources[0].url).toContain("after=9007199254740993");
    expect(sources[0].url).not.toContain("replay=false");
  });

  it("rejects a cursor beyond uint64 even when represented as a string", async () => {
    const app = await connectPiApp();
    app.setSessionEventCursor("s1", "18446744073709551616");
    expect(app.sessionEventCursors.get("s1")).toBeUndefined();
  });
});
