export type SessionEventCursor = string;

const MAX_EVENT_CURSOR = "9223372036854775807";

export type SessionEventEnvelope = {
  id: SessionEventCursor | number;
  type: string;
  sessionId: string;
  payload: unknown;
};

export function normalizeEventCursor(value: unknown): SessionEventCursor | undefined {
  if (typeof value === "number") {
    if (!Number.isSafeInteger(value) || value < 0) return undefined;
    return String(value);
  }
  if (typeof value !== "string" || !/^\d+$/.test(value)) return undefined;
  const normalized = value.replace(/^0+(?=\d)/, "");
  if (normalized.length > MAX_EVENT_CURSOR.length
    || (normalized.length === MAX_EVENT_CURSOR.length && normalized > MAX_EVENT_CURSOR)) return undefined;
  return normalized;
}

export function compareEventCursors(left: unknown, right: unknown): number {
  const leftCursor = normalizeEventCursor(left) || "0";
  const rightCursor = normalizeEventCursor(right) || "0";
  if (leftCursor.length !== rightCursor.length) return leftCursor.length > rightCursor.length ? 1 : -1;
  if (leftCursor === rightCursor) return 0;
  return leftCursor > rightCursor ? 1 : -1;
}

export function shouldResyncAfterGap(event: SessionEventEnvelope): boolean {
  return event?.type === "broker_gap";
}
