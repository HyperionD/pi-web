import { describe, expect, it } from "vitest";
import { emptyTranscriptState } from "./transcript-state";
import { reduceSessionEvent } from "./transcript-reducer";

type CommandReceipt = {
  clientRequestId: string;
  runId: string;
  acceptedSeq: string;
};

function dispatchCommandReceipt(state: ReturnType<typeof emptyTranscriptState>, receipt: CommandReceipt) {
  return reduceSessionEvent(state, {
    seq: state.appliedSeq,
    sessionId: state.sessionId,
    sourceId: `command:${receipt.clientRequestId}`,
    type: "command.receipt",
    payload: receipt,
  });
}

describe("session command/event contract", () => {
  it("keeps state unchanged until user.message.created arrives", () => {
    const before = emptyTranscriptState("s1", Symbol("view-1"));
    const afterReceipt = dispatchCommandReceipt(before, {
      clientRequestId: "req-1",
      runId: "run-1",
      acceptedSeq: "0",
    });

    expect(afterReceipt).toEqual(before);
    expect(afterReceipt.itemsById.size).toBe(0);

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
