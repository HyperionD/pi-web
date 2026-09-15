import type { TranscriptState } from "./transcript-state";

export type SessionCommandReceipt = {
  clientRequestId: string;
  runId: string;
  acceptedSeq: string;
  existing?: boolean;
};

/**
 * Command receipts acknowledge durable acceptance; they are not durable facts.
 * Keep the transcript projection unchanged until the event stream delivers them.
 */
export function applySessionCommandReceipt(state: TranscriptState, _receipt: SessionCommandReceipt): TranscriptState {
  return state;
}
