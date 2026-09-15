// @ts-nocheck
import { describe, expect, it } from "vitest";
import { adaptLegacySessionEvent } from "./legacy-session-event-adapter";

function event(type, id, payload) {
  return { type, id, sessionId: "s1", payload };
}

describe("legacy session event adapter", () => {
  it("maps assistant, tool, and terminal events to durable events", () => {
    const activeToolIds = new Map();
    const context = { sessionId: "s1", runId: "run-1", activeToolIds };
    expect(adaptLegacySessionEvent(event("session.delta", 1, { kind: "pi", delta: "hel" }), context)).toEqual([
      {
        seq: "1", sessionId: "s1", runId: "run-1", sourceId: "legacy:1",
        type: "assistant.segment.delta", entityId: "assistant:legacy:pi",
        payload: { kind: "pi", delta: "hel" },
      },
    ]);
    expect(adaptLegacySessionEvent(event("tool.started", 2, {
      id: "call-1", tool: "bash", args: "pwd", status: "running",
    }), context)[0]).toMatchObject({
      seq: "2", type: "tool.started", entityId: "tool:run-1:call-1",
      payload: { toolCallId: "call-1", tool: "bash", args: "pwd" },
    });
    expect(activeToolIds.get("call-1")).toBe("tool:run-1:call-1");
    expect(adaptLegacySessionEvent(event("tool.output", 3, {
      id: "call-1", chunk: "out", mode: "append",
    }), context)[0]).toMatchObject({
      seq: "3", type: "tool.output", entityId: "tool:run-1:call-1",
      payload: { chunk: "out", mode: "append" },
    });
    expect(adaptLegacySessionEvent(event("tool.finished", 4, {
      id: "call-1", body: "boom", status: "err", resultMeta: "failed", durationMs: 5,
    }), context)[0]).toMatchObject({
      seq: "4", type: "tool.finished", entityId: "tool:run-1:call-1",
      payload: { body: "boom", status: "err", resultMeta: "failed", durationMs: 5 },
    });
    expect(activeToolIds.has("call-1")).toBe(false);
    expect(adaptLegacySessionEvent(event("session.status", 5, { status: "idle" }), context)[0]).toMatchObject({
      seq: "5", type: "run.finished", runId: "run-1",
    });
  });

  it("creates deterministic ids for sparse tool events", () => {
    const context = { sessionId: "s1", runId: "run-1", activeToolIds: new Map() };
    const [started] = adaptLegacySessionEvent(event("tool.started", 10, { tool: "read" }), context);
    const [output] = adaptLegacySessionEvent(event("tool.output", 11, { tool: "read", chunk: "x" }), context);
    expect(started.entityId).toBe("tool:run-1:legacy:10");
    expect(output.entityId).toBe("tool:run-1:legacy:10");
  });

  it("preserves failure events and ignores heartbeats", () => {
    const context = { sessionId: "s1", runId: "run-1", activeToolIds: new Map() };
    expect(adaptLegacySessionEvent(event("error", 8, { error: "offline" }), context)[0]).toMatchObject({
      seq: "8", type: "run.failed", payload: { error: "offline" },
    });
    expect(adaptLegacySessionEvent(event("heartbeat", 9, {},), context)).toEqual([]);
  });
});
