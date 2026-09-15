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

  it("rejects a cursor beyond uint64 even when represented as a string", async () => {
    const app = await connectPiApp();
    app.setSessionEventCursor("s1", "18446744073709551616");
    expect(app.sessionEventCursors.get("s1")).toBeUndefined();
  });
});
