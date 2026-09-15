export type SessionSeq = string;

export const MAX_SESSION_SEQ = 9_223_372_036_854_775_807n;

export type TranscriptItemKind = "user" | "pi" | "think" | "tool" | "error" | "annotation" | "banner";

export type TranscriptItem = {
  id: string;
  ordinal: number;
  kind: TranscriptItemKind;
  origin: string;
  runId?: string;
  entityId?: string;
  text: string;
  tool?: string;
  toolCallId?: string;
  args?: string;
  bodyPreview: string;
  contentRef?: string;
  contentVersion?: number;
  contentLength?: number;
  status?: string;
  resultMeta?: string;
  durationMs?: number;
  attachments?: unknown[];
};

export type TranscriptStatus = "idle" | "running" | "thinking" | "compacting" | "cancelled" | "interrupted" | "error";

export type TranscriptState = {
  sessionId: string;
  viewGeneration: symbol;
  projectionGeneration: string;
  appliedSeq: SessionSeq;
  status: TranscriptStatus;
  runId?: string;
  sessionTitle?: string;
  sessionDeleted?: boolean;
  sessionModel?: { provider?: string; model?: string; thinkingLevel?: string };
  contextUsage?: unknown;
  itemOrder: string[];
  itemsById: Map<string, TranscriptItem>;
  toolsByCallId: Map<string, string>;
  activeSegments: Map<string, string>;
  historyCursor?: string;
  historyHasMore: boolean;
};

export type DurableSessionEvent = {
  seq: SessionSeq;
  sessionId: string;
  runId?: string;
  sourceId?: string;
  type: string;
  entityId?: string;
  payload?: Record<string, unknown>;
};

export type SessionStateSnapshot = {
  sessionId: string;
  metadata?: {
    title?: string;
    deleted?: boolean;
    provider?: string;
    model?: string;
    thinkingLevel?: string;
    contextUsage?: unknown;
  };
  items: TranscriptItem[];
  status: string;
  runId?: string;
  seq: SessionSeq;
  cursor?: string;
  hasMore: boolean;
  limit: number;
  projectionSchemaVersion: number;
  projectionGeneration: string;
};

export function normalizeSessionSeq(value: unknown): SessionSeq | undefined {
  if (typeof value === "number") {
    if (!Number.isSafeInteger(value) || value < 0) return undefined;
    return String(value);
  }
  if (typeof value !== "string" || !/^\d+$/.test(value)) return undefined;
  const normalized = value.replace(/^0+(?=\d)/, "");
  if (normalized.length > MAX_SESSION_SEQ.toString().length) return undefined;
  if (BigInt(normalized) > MAX_SESSION_SEQ) return undefined;
  return normalized;
}

export function compareSessionSeq(left: unknown, right: unknown): number {
  const leftSeq = normalizeSessionSeq(left) || "0";
  const rightSeq = normalizeSessionSeq(right) || "0";
  const leftValue = BigInt(leftSeq);
  const rightValue = BigInt(rightSeq);
  if (leftValue === rightValue) return 0;
  return leftValue > rightValue ? 1 : -1;
}

export function nextSessionSeq(value: unknown): SessionSeq | undefined {
  const normalized = normalizeSessionSeq(value);
  if (normalized === undefined) return undefined;
  const next = BigInt(normalized) + 1n;
  return next <= MAX_SESSION_SEQ ? next.toString() : undefined;
}

export function emptyTranscriptState(sessionId: string, viewGeneration: symbol = Symbol(sessionId)): TranscriptState {
  return {
    sessionId,
    viewGeneration,
    projectionGeneration: "1",
    appliedSeq: "0",
    status: "idle",
    itemOrder: [],
    itemsById: new Map(),
    toolsByCallId: new Map(),
    activeSegments: new Map(),
    historyHasMore: false,
  };
}

export function emptyTranscriptItem(id: string, ordinal: number): TranscriptItem {
  return { id, ordinal, kind: "pi", origin: "runtime", text: "", bodyPreview: "" };
}

export function isTerminalStatus(status: string | undefined): boolean {
  return status === "ok" || status === "err" || status === "cancelled" || status === "interrupted";
}
