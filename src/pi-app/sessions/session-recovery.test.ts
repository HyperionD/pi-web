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

    expect(app.loadSession).toHaveBeenCalledWith("s1", { resync: true });
    expect(app.appendMessage).not.toHaveBeenCalled();
  });

  it("keeps a per-session cursor and reconnects after the snapshot cursor", async () => {
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
    app.connectEvents("s1", { after: 7 });
    expect(sources[0].url).toContain("after=7");
    sources[0].listeners["session.message"]({
      data: JSON.stringify({ id: 8, type: "session.message", sessionId: "s1", payload: { kind: "pi", text: "ok" } }),
    });
    expect(app.sessionEventCursors.get("s1")).toBe(8);
  });
});
