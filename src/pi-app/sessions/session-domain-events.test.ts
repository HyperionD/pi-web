import { describe, expect, it } from "vitest";
import { emptyTranscriptState } from "./transcript-state";
import { reduceSessionEvent } from "./transcript-reducer";

describe("session command/event contract", () => {
  it("keeps state unchanged until user.message.created arrives", () => {
    const before = emptyTranscriptState("s1", "view-1");
    const afterReceipt = before;

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
