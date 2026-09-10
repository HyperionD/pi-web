export type SessionEventEnvelope = {
  id: number;
  type: string;
  sessionId: string;
  payload: unknown;
};

export function shouldResyncAfterGap(event: SessionEventEnvelope): boolean {
  return event?.type === "broker_gap";
}
