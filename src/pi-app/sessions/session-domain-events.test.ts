import { describe, expect, it } from "vitest";
import { applySessionCommandReceipt, type SessionCommandReceipt } from "./session-command-state";
import { emptyTranscriptState } from "./transcript-state";
import { reduceSessionEvent } from "./transcript-reducer";

describe("session command/event contract", () => {
  it("keeps state unchanged until user.message.created arrives", () => {
    const before = emptyTranscriptState("s1", Symbol("view-1"));
    const receipt: SessionCommandReceipt = {
      clientRequestId: "req-1",
      runId: "run-1",
      acceptedSeq: "0",
    };
    const afterReceipt = applySessionCommandReceipt(before, receipt);

    expect(afterReceipt.itemOrder).toEqual(before.itemOrder);
    expect(afterReceipt.itemsById).toEqual(before.itemsById);
    expect(afterReceipt.status).toBe(before.status);
    expect(afterReceipt.runId).toBe(before.runId);
    expect(afterReceipt.appliedSeq).toBe(before.appliedSeq);

    const afterEvent = reduceSessionEvent(afterReceipt, {
      seq: "1",
      sessionId: "s1",
      sourceId: "req-1:user",
      type: "user.message.created",
      payload: { text: "hello" },
    });

    expect(afterEvent.itemOrder).toHaveLength(1);
    expect(afterEvent.itemsById.get("user:req-1:user")?.text).toBe("hello");
  });
});
