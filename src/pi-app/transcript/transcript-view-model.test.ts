// @ts-nocheck
import { describe, expect, it } from "vitest";
import { emptyTranscriptState } from "../sessions/transcript-state";
import { selectTranscriptView, selectTranscriptViewMessage } from "./transcript-view-model";

describe("transcript view model", () => {
  it("projects ordered transcript items without DOM state", () => {
    const state = emptyTranscriptState("s1");
    state.status = "running";
    state.runId = "run-1";
    state.itemOrder = ["user-1", "tool-1", "error-1"];
    state.itemsById = new Map([
      ["user-1", {
        id: "user-1", ordinal: 1, kind: "user", origin: "runtime", text: "hello",
        bodyPreview: "", attachments: [{ name: "a.txt" }],
      }],
      ["tool-1", {
        id: "tool-1", ordinal: 2, kind: "tool", origin: "runtime", text: "",
        bodyPreview: "output", tool: "bash", args: "pwd", status: "err", resultMeta: "failed", durationMs: 12,
      }],
      ["error-1", {
        id: "error-1", ordinal: 3, kind: "error", origin: "runtime", text: "boom", bodyPreview: "",
      }],
    ]);

    expect(selectTranscriptView(state)).toEqual({
      sessionId: "s1",
      status: "running",
      runId: "run-1",
      items: [
        { key: "user-1", kind: "user", text: "hello", attachments: [{ name: "a.txt" }] },
        {
          key: "tool-1", kind: "tool", text: "", tool: "bash", args: "pwd", body: "output",
          status: "err", resultMeta: "failed", durationMs: 12,
        },
        { key: "error-1", kind: "error", text: "boom" },
      ],
    });
  });

  it("maps sparse items to stable render messages", () => {
    expect(selectTranscriptViewMessage({ id: "x", kind: "pi", text: "answer" })).toEqual({
      key: "x", kind: "pi", text: "answer",
    });
  });

  it("returns an empty view for an empty state", () => {
    const state = emptyTranscriptState("s1");
    expect(selectTranscriptView(state)).toEqual({ sessionId: "s1", status: "idle", items: [] });
  });
});
