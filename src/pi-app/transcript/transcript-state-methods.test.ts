import { describe, expect, it } from "vitest";
import { emptyTranscriptState, type TranscriptItem } from "../sessions/transcript-state";
import { installSessionSnapshot } from "../sessions/transcript-reducer";
import { selectOrderedTranscriptItems, selectToolItemIdForCall, selectTranscriptRenderItem } from "./transcript-state-methods";

function item(overrides: Partial<TranscriptItem> = {}): TranscriptItem {
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

const state = installSessionSnapshot(emptyTranscriptState("s1"), {
  sessionId: "s1",
  items: [item(), item({ id: "user:1", kind: "user", runId: undefined, toolCallId: undefined })],
  status: "running",
  seq: "3",
  hasMore: false,
  limit: 120,
  projectionSchemaVersion: 1,
  projectionGeneration: "1",
});

describe("transcript state selectors", () => {
  it("selects a single item by id and returns undefined for misses", () => {
    expect(selectTranscriptRenderItem(state, "tool:run-1:call-1")?.status).toBe("running");
    expect(selectTranscriptRenderItem(state, "missing")).toBeUndefined();
    expect(selectTranscriptRenderItem(undefined, "x")).toBeUndefined();
    expect(selectTranscriptRenderItem(state, "")).toBeUndefined();
  });

  it("selects ordered items and drops unknown ids", () => {
    expect(selectOrderedTranscriptItems(state).map((entry) => entry.id)).toEqual([
      "tool:run-1:call-1",
      "user:1",
    ]);
    expect(selectOrderedTranscriptItems(undefined)).toEqual([]);
    const withDangling = { ...state, itemOrder: [...state.itemOrder, "ghost"] };
    expect(selectOrderedTranscriptItems(withDangling).map((entry) => entry.id)).toEqual(["tool:run-1:call-1", "user:1"]);
  });

  it("resolves tool call ids for a run", () => {
    expect(selectToolItemIdForCall(state, "run-1", "call-1")).toBe("tool:run-1:call-1");
    expect(selectToolItemIdForCall(state, "run-1", "nope")).toBeUndefined();
    expect(selectToolItemIdForCall(state, "", "call-1")).toBeUndefined();
    expect(selectToolItemIdForCall(undefined, "run-1", "call-1")).toBeUndefined();
  });
});
