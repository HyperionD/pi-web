import { describe, expect, it } from "vitest";
import {
  MAX_SESSION_SEQ,
  compareSessionSeq,
  emptyTranscriptItem,
  emptyTranscriptState,
  isTerminalStatus,
  nextSessionSeq,
  normalizeSessionSeq,
  type DurableSessionEvent,
  type SessionStateSnapshot,
  type TranscriptItem,
} from "./transcript-state";
import {
  TranscriptResetError,
  installSessionSnapshot,
  mergeHistoryPage,
  reduceSessionEvent,
} from "./transcript-reducer";

function snapshot(overrides: Partial<SessionStateSnapshot> = {}): SessionStateSnapshot {
  return {
    sessionId: "s1",
    items: [],
    status: "idle",
    seq: "4",
    hasMore: false,
    limit: 120,
    projectionSchemaVersion: 1,
    projectionGeneration: "1",
    ...overrides,
  };
}

function toolItem(overrides: Partial<TranscriptItem> = {}): TranscriptItem {
  return {
    id: "tool:run-1:call-1",
    ordinal: 1,
    kind: "tool",
    origin: "runtime",
    runId: "run-1",
    entityId: "tool:run-1:call-1",
    tool: "bash",
    toolCallId: "call-1",
    text: "",
    bodyPreview: "",
    status: "running",
    ...overrides,
  };
}

function event(overrides: Partial<DurableSessionEvent> = {}): DurableSessionEvent {
  return { seq: "5", sessionId: "s1", type: "run.started", runId: "run-1", ...overrides };
}

describe("session sequence helpers", () => {
  it("normalizes and compares decimal strings with BigInt", () => {
    expect(normalizeSessionSeq("007")).toBe("7");
    expect(normalizeSessionSeq(-1)).toBeUndefined();
    expect(normalizeSessionSeq(1.5)).toBeUndefined();
    expect(normalizeSessionSeq("abc")).toBeUndefined();
    expect(normalizeSessionSeq(MAX_SESSION_SEQ.toString())).toBe(MAX_SESSION_SEQ.toString());
    expect(normalizeSessionSeq((MAX_SESSION_SEQ + 1n).toString())).toBeUndefined();
    expect(normalizeSessionSeq("9223372036854775808")).toBeUndefined();
    expect(normalizeSessionSeq("1" + "0".repeat(20))).toBeUndefined();
    expect(compareSessionSeq("9", "10")).toBe(-1);
    expect(compareSessionSeq("10", "10")).toBe(0);
    expect(compareSessionSeq("11", "10")).toBe(1);
    expect(compareSessionSeq(undefined, "1")).toBe(-1);
  });

  it("increments without precision loss and clamps at max", () => {
    expect(nextSessionSeq("9")).toBe("10");
    expect(nextSessionSeq("9007199254740993")).toBe("9007199254740994");
    expect(nextSessionSeq(MAX_SESSION_SEQ.toString())).toBeUndefined();
    expect(nextSessionSeq("bad")).toBeUndefined();
    expect(normalizeSessionSeq(true)).toBeUndefined();
    expect(normalizeSessionSeq(5)).toBe("5");
    expect(compareSessionSeq("1", "nope")).toBe(1);
  });

  it("builds empty state and items and classifies terminal statuses", () => {
    const state = emptyTranscriptState("s1");
    expect(state.appliedSeq).toBe("0");
    expect(state.viewGeneration).toBeTypeOf("symbol");
    expect(emptyTranscriptItem("x", 3)).toMatchObject({ id: "x", ordinal: 3, text: "" });
    expect(isTerminalStatus("ok")).toBe(true);
    expect(isTerminalStatus("running")).toBe(false);
    expect(isTerminalStatus(undefined)).toBe(false);
  });
});

describe("transcript reducer", () => {
  it("installs snapshots with tool index and projection generation", () => {
    const state = installSessionSnapshot(emptyTranscriptState("s1"), snapshot({ items: [toolItem()], seq: "4", cursor: "c1", hasMore: true }));
    expect(state.appliedSeq).toBe("4");
    expect(state.historyCursor).toBe("c1");
    expect(state.historyHasMore).toBe(true);
    expect(state.toolsByCallId.get("run-1:call-1")).toBe("tool:run-1:call-1");
  });

  it("merges older history pages without duplicating ids", () => {
    let state = installSessionSnapshot(emptyTranscriptState("s1"), snapshot({ items: [toolItem()], seq: "4" }));
    state = mergeHistoryPage(state, snapshot({ items: [toolItem({ id: "user:1", kind: "user", toolCallId: undefined, runId: undefined })], seq: "4", cursor: "c2" }));
    expect(state.itemOrder).toEqual(["user:1", "tool:run-1:call-1"]);
    expect(state.historyCursor).toBe("c2");
    const unchanged = mergeHistoryPage(state, snapshot({ items: [toolItem()], seq: "4" }));
    expect(unchanged.itemOrder).toEqual(["user:1", "tool:run-1:call-1"]);
  });

  it("rejects a history page from another projection generation", () => {
    const state = installSessionSnapshot(emptyTranscriptState("s1"), snapshot());
    expect(() => mergeHistoryPage(state, snapshot({ projectionGeneration: "2" }))).toThrow(TranscriptResetError);
  });

  it("applies text and tool lifecycles and ends the run", () => {
    let state = installSessionSnapshot(emptyTranscriptState("s1"), snapshot({ seq: "0", status: "running" }));
    state = reduceSessionEvent(state, event({ seq: "1", type: "user.message.created", sourceId: "u1", payload: { text: "hi" } }));
    expect(state.itemOrder).toHaveLength(1);
    state = reduceSessionEvent(state, event({ seq: "2", type: "assistant.segment.started", entityId: "assistant:run-1:1", payload: { kind: "pi" } }));
    state = reduceSessionEvent(state, event({ seq: "3", type: "assistant.segment.delta", entityId: "assistant:run-1:1", payload: { delta: "hel" } }));
    state = reduceSessionEvent(state, event({ seq: "4", type: "assistant.segment.delta", entityId: "assistant:run-1:1", payload: { delta: "lo" } }));
    state = reduceSessionEvent(state, event({ seq: "5", type: "assistant.segment.committed", entityId: "assistant:run-1:1", payload: { kind: "pi", text: "hello" } }));
    expect(state.itemsById.get("assistant:run-1:1")?.text).toBe("hello");
    expect(state.itemsById.get("assistant:run-1:1")?.status).toBe("committed");
    state = reduceSessionEvent(state, event({ seq: "6", type: "tool.started", entityId: "tool:run-1:call-1", payload: { tool: "bash", toolCallId: "call-1" } }));
    state = reduceSessionEvent(state, event({ seq: "7", type: "tool.args.delta", entityId: "tool:run-1:call-1", payload: { delta: "pwd" } }));
    state = reduceSessionEvent(state, event({ seq: "8", type: "tool.output", entityId: "tool:run-1:call-1", payload: { chunk: "a", mode: "append" } }));
    state = reduceSessionEvent(state, event({ seq: "9", type: "tool.output", entityId: "tool:run-1:call-1", payload: { chunk: "b", mode: "replace" } }));
    expect(state.itemsById.get("tool:run-1:call-1")?.bodyPreview).toBe("b");
    state = reduceSessionEvent(state, event({ seq: "10", type: "tool.finished", entityId: "tool:run-1:call-1", payload: { body: "final", status: "ok" } }));
    expect(state.itemsById.get("tool:run-1:call-1")?.status).toBe("ok");
    state = reduceSessionEvent(state, event({ seq: "11", type: "run.finished" }));
    expect(state.status).toBe("idle");
    expect(state.runId).toBeUndefined();
  });

  it("creates fallback items for deltas and tool events without a prior start", () => {
    let state = emptyTranscriptState("s1");
    state = { ...state, status: "running", runId: "run-1" };
    state = reduceSessionEvent(state, event({ seq: "1", type: "assistant.segment.delta", entityId: "assistant:run-1:1", payload: { kind: "think", delta: "x" } }));
    expect(state.itemsById.get("assistant:run-1:1")?.kind).toBe("think");
    state = reduceSessionEvent(state, event({ seq: "2", type: "assistant.segment.committed", entityId: "thinking:run-1:2", payload: { kind: "think", text: "" } }));
    expect(state.itemsById.get("thinking:run-1:2")?.text).toBe("");
    state = reduceSessionEvent(state, event({ seq: "3", type: "tool.output", entityId: "tool:run-1:call-9", payload: { chunk: "z" } }));
    expect(state.itemsById.get("tool:run-1:call-9")?.toolCallId).toBe("call-9");
    state = reduceSessionEvent(state, event({ seq: "4", type: "tool.finished", entityId: "tool:run-1:call-9", payload: {} }));
    expect(state.itemsById.get("tool:run-1:call-9")?.status).toBe("ok");
  });

  it("terminalizes tools and segments for every terminal run event", () => {
    for (const [type, status, toolStatus] of [
      ["run.failed", "error", "err"],
      ["run.cancelled", "cancelled", "cancelled"],
      ["run.interrupted", "interrupted", "interrupted"],
    ] as const) {
      let state = installSessionSnapshot(emptyTranscriptState("s1"), snapshot({ seq: "0", items: [toolItem({ status: "running" })], status: "running" }));
      state = reduceSessionEvent(state, event({ seq: "1", type: "assistant.segment.started", entityId: "assistant:run-1:1", payload: { kind: "pi" } }));
      state = reduceSessionEvent(state, event({ seq: "2", type }));
      expect(state.status).toBe(status);
      expect(state.itemsById.get("tool:run-1:call-1")?.status).toBe(toolStatus);
      expect(state.itemsById.get("assistant:run-1:1")?.status).toBe("committed");
    }
  });

  it("ignores duplicate, stale and foreign events and rejects gaps and rebases", () => {
    const state = { ...emptyTranscriptState("s1"), appliedSeq: "5" };
    expect(reduceSessionEvent(state, event({ seq: "5" }))).toBe(state);
    expect(reduceSessionEvent(state, event({ seq: "4" }))).toBe(state);
    expect(reduceSessionEvent(state, event({ seq: "6", sessionId: "s2" }))).toBe(state);
    expect(() => reduceSessionEvent(state, event({ seq: "8" }))).toThrow(TranscriptResetError);
    expect(() => reduceSessionEvent(state, event({ seq: "bad" }))).toThrow(TranscriptResetError);
    expect(() => reduceSessionEvent(state, event({ seq: "6", type: "session.rebased" }))).toThrow(TranscriptResetError);
  });

  it("covers segment kind fallbacks and output replacement edge cases", () => {
    let state = installSessionSnapshot(emptyTranscriptState("s1"), snapshot({ seq: "0" }));
    state = reduceSessionEvent(state, event({ seq: "1", type: "assistant.segment.started", entityId: "thinking:run-1:1", payload: {} }));
    expect(state.itemsById.get("thinking:run-1:1")?.kind).toBe("pi");
    state = reduceSessionEvent(state, event({ seq: "2", type: "assistant.segment.committed", entityId: "thinking:run-1:1", payload: {} }));
    expect(state.itemsById.get("thinking:run-1:1")?.status).toBe("committed");
    state = reduceSessionEvent(state, event({ seq: "3", type: "tool.output", entityId: "tool:run-1:call-7", payload: { mode: "replace" } }));
    expect(state.itemsById.get("tool:run-1:call-7")?.bodyPreview).toBe("");
    state = reduceSessionEvent(state, event({ seq: "4", type: "tool.finished", entityId: "tool:run-1:call-7", payload: {} }));
    expect(state.itemsById.get("tool:run-1:call-7")?.status).toBe("ok");
    state = reduceSessionEvent(state, event({ seq: "5", type: "tool.output", entityId: "tool:run-1:call-7", payload: { chunk: "old" } }));
    state = reduceSessionEvent(state, event({ seq: "6", type: "tool.finished", entityId: "tool:run-1:call-7", payload: { body: "" } }));
    expect(state.itemsById.get("tool:run-1:call-7")?.bodyPreview).toBe("");
    state = reduceSessionEvent(state, event({ seq: "7", type: "tool.output", entityId: "tool:run-1:call:extra", payload: {} }));
    expect(state.itemsById.get("tool:run-1:call:extra")?.toolCallId).toBe("call:extra");
    const rich = state.itemsById.get("tool:run-1:call:extra")!;
    state = { ...state, itemsById: new Map(state.itemsById).set(rich.id, { ...rich, text: "seeded", status: undefined }) };
    state = reduceSessionEvent(state, event({ seq: "8", type: "assistant.segment.delta", entityId: rich.id, payload: {} }));
    expect(state.itemsById.get(rich.id)?.status).toBe("streaming");
  });

  it("applies tool events with sparse payloads and non-namespaced ids", () => {
    let state = installSessionSnapshot(emptyTranscriptState("s1"), snapshot({ seq: "0" }));
    state = reduceSessionEvent(state, event({ seq: "1", type: "tool.started", entityId: "plain", payload: {} }));
    expect(state.itemsById.get("plain")?.tool).toBe("tool");
    expect(state.itemsById.get("plain")?.toolCallId).toBe("");
    state = reduceSessionEvent(state, event({ seq: "2", type: "tool.args.delta", entityId: "plain", payload: {} }));
    expect(state.itemsById.get("plain")?.args).toBe("");
    state = reduceSessionEvent(state, event({ seq: "3", type: "tool.output", entityId: "plain", payload: {} }));
    expect(state.itemsById.get("plain")?.bodyPreview).toBe("");
    state = reduceSessionEvent(state, event({ seq: "4", type: "tool.finished", entityId: "plain", payload: { body: "" } }));
    expect(state.itemsById.get("plain")?.status).toBe("ok");
    state = reduceSessionEvent(state, event({ seq: "5", type: "tool.finished", entityId: "plain", payload: { body: "", status: "ok" } }));
    expect(state.itemsById.get("plain")?.status).toBe("ok");
  });

  it("handles missing snapshot fields and fallback ids", () => {
    const sparseSnapshot = { ...snapshot({ seq: "1" }), sessionId: "", projectionGeneration: "", items: undefined as unknown as TranscriptItem[] };
    const installed = installSessionSnapshot(emptyTranscriptState("s1"), sparseSnapshot);
    expect(installed.sessionId).toBe("s1");
    expect(installed.projectionGeneration).toBe("1");
    expect(installed.itemOrder).toHaveLength(0);
    const merged = mergeHistoryPage(installed, { ...sparseSnapshot, items: [{ ...toolItem({ id: "m1", bodyPreview: null as unknown as string }) }] });
    expect(merged.itemOrder).toEqual(["m1"]);
    expect(mergeHistoryPage(merged, sparseSnapshot).itemOrder).toEqual(["m1"]);
    const userEvent = reduceSessionEvent(installed, { seq: "2", sessionId: "s1", type: "user.message.created", payload: {} });
    expect(userEvent.itemOrder[0]).toBe("user:2");
    expect(userEvent.itemsById.get("user:2")?.text).toBe("");
    const fallbackTool = reduceSessionEvent(userEvent, event({ seq: "3", type: "tool.output", entityId: "plain2", payload: {} }));
    expect(fallbackTool.itemsById.get("plain2")?.toolCallId).toBe("");
  });

  it("marks the session running on run.started", () => {
    const state = reduceSessionEvent(emptyTranscriptState("s1"), event({ seq: "1", type: "run.started", runId: "run-9" }));
    expect(state.status).toBe("running");
    expect(state.runId).toBe("run-9");
  });

  it("ignores unknown event types while advancing the sequence", () => {
    const state = { ...emptyTranscriptState("s1"), appliedSeq: "5" };
    const next = reduceSessionEvent(state, event({ seq: "6", type: "workspace.files.changed" }));
    expect(next.appliedSeq).toBe("6");
    expect(next.itemOrder).toHaveLength(0);
  });

  it("handles snapshot edge cases and terminalization of untyped items", () => {
    const bare = installSessionSnapshot(emptyTranscriptState("s1"), { ...snapshot({ seq: "2" }), status: "", items: [
      toolItem({ runId: undefined, toolCallId: undefined, bodyPreview: undefined as unknown as string }),
      { ...toolItem({ id: "plain", kind: "pi" }), status: undefined },
    ] });
    expect(bare.status).toBe("idle");
    expect(bare.toolsByCallId.size).toBe(0);
    const terminal = reduceSessionEvent({ ...bare, appliedSeq: "2" }, event({ seq: "3", type: "run.finished" }));
    expect(terminal.itemsById.get("plain")?.status).toBeUndefined();
  });

  it("keeps an existing segment item when restarted and treats empty session ids as local", () => {
    let state = installSessionSnapshot(emptyTranscriptState("s1"), snapshot({ seq: "0" }));
    state = reduceSessionEvent(state, event({ seq: "1", sessionId: "", type: "assistant.segment.started", entityId: "assistant:run-1:1", payload: { kind: "pi" } }));
    state = reduceSessionEvent(state, event({ seq: "2", sessionId: "", type: "assistant.segment.started", entityId: "assistant:run-1:1", payload: { kind: "pi" } }));
    expect(state.itemOrder).toHaveLength(1);
    state = reduceSessionEvent(state, event({ seq: "3", sessionId: "", type: "assistant.segment.committed", entityId: "assistant:run-1:1", payload: { kind: "pi", text: "kept" } }));
    expect(state.itemsById.get("assistant:run-1:1")?.text).toBe("kept");
  });
});
