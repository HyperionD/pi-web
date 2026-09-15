import {
  type DurableSessionEvent,
  type SessionStateSnapshot,
  type TranscriptItem,
  type TranscriptState,
  compareSessionSeq,
  nextSessionSeq,
  normalizeSessionSeq,
} from "./transcript-state";

export class TranscriptResetError extends Error {
  readonly reason: "gap" | "rebase" | "invalid";

  constructor(reason: "gap" | "rebase" | "invalid", message: string) {
    super(message);
    this.name = "TranscriptResetError";
    this.reason = reason;
  }
}

function cloneItem(item: TranscriptItem): TranscriptItem {
  return { ...item };
}

function normalizeItem(raw: Partial<TranscriptItem> & { itemId?: string; body?: string }, ordinal: number): TranscriptItem {
  const id = raw.id || raw.itemId || `item:${raw.ordinal || ordinal}`;
  return {
    ...raw,
    id,
    ordinal: raw.ordinal || ordinal,
    kind: raw.kind || "pi",
    origin: raw.origin || "runtime",
    text: raw.text || "",
    bodyPreview: raw.bodyPreview ?? raw.body ?? "",
  } as TranscriptItem;
}

function upsertItem(state: TranscriptState, item: TranscriptItem): TranscriptState {
  const itemsById = new Map(state.itemsById);
  itemsById.set(item.id, item);
  const itemOrder = state.itemOrder.includes(item.id) ? state.itemOrder : [...state.itemOrder, item.id];
  return { ...state, itemsById, itemOrder };
}

function terminalItem(item: TranscriptItem, status: string): TranscriptItem {
  if (!item.status || item.status === "ok" || item.status === "err" || item.status === "cancelled" || item.status === "interrupted") {
    return item;
  }
  return { ...item, status: item.kind === "tool" ? status : "committed" };
}

export function installSessionSnapshot(state: TranscriptState, snapshot: SessionStateSnapshot): TranscriptState {
  const itemsById = new Map<string, TranscriptItem>();
  const itemOrder: string[] = [];
  const toolsByCallId = new Map<string, string>();
  for (let index = 0; index < (snapshot.items || []).length; index++) {
    const raw = snapshot.items[index] as Partial<TranscriptItem> & { itemId?: string; body?: string };
    const item = normalizeItem(raw, index + 1);
    itemsById.set(item.id, item);
    itemOrder.push(item.id);
    if (item.kind === "tool" && item.runId && item.toolCallId) {
      toolsByCallId.set(`${item.runId}:${item.toolCallId}`, item.id);
    }
  }
  return {
    ...state,
    sessionId: snapshot.sessionId || state.sessionId,
    projectionGeneration: snapshot.projectionGeneration || state.projectionGeneration,
    appliedSeq: snapshot.seq,
    status: (snapshot.status as TranscriptState["status"]) || "idle",
    runId: snapshot.runId,
    sessionTitle: snapshot.metadata?.title,
    sessionModel: snapshot.metadata && {
      provider: snapshot.metadata.provider,
      model: snapshot.metadata.model,
      thinkingLevel: snapshot.metadata.thinkingLevel,
    },
    contextUsage: snapshot.metadata?.contextUsage,
    itemOrder,
    itemsById,
    toolsByCallId,
    activeSegments: new Map(),
    historyCursor: snapshot.cursor,
    historyHasMore: !!snapshot.hasMore,
  };
}

export function mergeHistoryPage(state: TranscriptState, snapshot: SessionStateSnapshot): TranscriptState {
  if (snapshot.projectionGeneration && snapshot.projectionGeneration !== state.projectionGeneration) {
    throw new TranscriptResetError("rebase", "projection generation changed");
  }
  const itemsById = new Map(state.itemsById);
  const older: string[] = [];
  for (let index = 0; index < (snapshot.items || []).length; index++) {
    const item = normalizeItem(
      snapshot.items[index] as Partial<TranscriptItem> & { itemId?: string; body?: string },
      index + 1,
    );
    if (itemsById.has(item.id)) continue;
    itemsById.set(item.id, cloneItem(item));
    older.push(item.id);
  }
  return {
    ...state,
    itemsById,
    itemOrder: [...older, ...state.itemOrder],
    historyCursor: snapshot.cursor,
    historyHasMore: !!snapshot.hasMore,
  };
}

export function reduceSessionEvent(state: TranscriptState, event: DurableSessionEvent): TranscriptState {
  if (event.sessionId && event.sessionId !== state.sessionId) return state;
  const expected = nextSessionSeq(state.appliedSeq);
  const incoming = normalizeSessionSeq(event.seq);
  if (incoming === undefined) throw new TranscriptResetError("invalid", "invalid session sequence");
  if (compareSessionSeq(incoming, state.appliedSeq) <= 0) return state;
  if (expected === undefined || incoming !== expected) {
    throw new TranscriptResetError("gap", `sequence gap: expected ${expected}, got ${incoming}`);
  }
  if (event.type === "session.rebased") {
    throw new TranscriptResetError("rebase", "session projection was rebased");
  }
  const next = applyEvent(state, event);
  return { ...next, appliedSeq: incoming };
}

function applyEvent(state: TranscriptState, event: DurableSessionEvent): TranscriptState {
  const payload = (event.payload || {}) as Record<string, any>;
  switch (event.type) {
    case "transcript.item.created":
      return upsertItem(state, {
        id: event.entityId as string,
        ordinal: state.itemOrder.length + 1,
        kind: payload.kind || "annotation",
        origin: "legacy",
        text: payload.text || String(payload.value ?? ""),
        bodyPreview: payload.body || "",
        attachments: payload.attachments || [],
      } as TranscriptItem);
    case "user.message.created":
      return upsertItem(state, {
        id: `user:${event.sourceId || event.seq}`,
        ordinal: state.itemOrder.length + 1,
        kind: "user",
        origin: "runtime",
        text: payload.text || "",
        bodyPreview: "",
        attachments: payload.attachments || [],
      });
    case "run.started":
      return { ...state, status: "running", runId: event.runId };
    case "session.created":
      return { ...state, sessionTitle: payload.title || state.sessionTitle };
    case "session.renamed":
      return { ...state, sessionTitle: payload.title || state.sessionTitle };
    case "session.model.changed":
    case "runtime.model.updated":
      return {
        ...state,
        sessionModel: {
          provider: payload.provider as string | undefined,
          model: payload.model as string | undefined,
          thinkingLevel: payload.thinkingLevel as string | undefined,
        },
      };
    case "context.usage.updated":
      return { ...state, contextUsage: payload.contextUsage };
    case "compaction.started":
      return { ...state, status: "compacting" };
    case "compaction.committed":
      return { ...state, status: "idle" };
    case "compaction.failed":
      return { ...state, status: "error" };
    case "run.cancel.requested":
    case "steer.accepted":
    case "session.deleted":
      return state;
    case "assistant.segment.started": {
      const item = ensureSegmentItem(state, event, payload.kind);
      const activeSegments = new Map(state.activeSegments);
      activeSegments.set(payload.kind || "pi", item.id);
      return { ...upsertItem(state, item), activeSegments };
    }
    case "assistant.segment.delta": {
      const itemId = event.entityId as string;
      const existing = state.itemsById.get(itemId);
      const item = existing ? cloneItem(existing) : segmentFallbackItem(state, event, payload.kind);
      item.text = `${item.text || ""}${payload.delta || ""}`;
      item.status = item.status || "streaming";
      return upsertItem(state, item);
    }
    case "assistant.segment.committed": {
      const itemId = event.entityId as string;
      const existing = state.itemsById.get(itemId);
      const item = existing ? cloneItem(existing) : segmentFallbackItem(state, event, payload.kind);
      item.text = payload.text !== undefined && payload.text !== "" ? payload.text : item.text;
      item.status = "committed";
      const activeSegments = new Map(state.activeSegments);
      activeSegments.delete(payload.kind || item.kind);
      return { ...upsertItem(state, item), activeSegments };
    }
    case "tool.started": {
      const item: TranscriptItem = {
        id: event.entityId as string,
        ordinal: state.itemOrder.length + 1,
        kind: "tool",
        origin: "runtime",
        runId: event.runId,
        entityId: event.entityId,
        tool: payload.tool || "tool",
        toolCallId: payload.toolCallId || "",
        args: payload.args || "",
        text: "",
        bodyPreview: "",
        status: "running",
      };
      const toolsByCallId = new Map(state.toolsByCallId);
      if (item.runId && item.toolCallId) toolsByCallId.set(`${item.runId}:${item.toolCallId}`, item.id);
      return { ...upsertItem(state, item), toolsByCallId };
    }
    case "tool.args.delta": {
      const item = toolItemFor(state, event);
      item.args = `${item.args || ""}${payload.delta || ""}`;
      return upsertItem(state, item);
    }
    case "tool.output": {
      const item = toolItemFor(state, event);
      item.bodyPreview = payload.mode === "replace" ? payload.chunk || "" : `${item.bodyPreview || ""}${payload.chunk || ""}`;
      return upsertItem(state, item);
    }
    case "tool.finished": {
      const item = toolItemFor(state, event);
      if (payload.tool && item.tool === "tool") item.tool = payload.tool;
      if (payload.args && !item.args) item.args = payload.args;
      if (item.bodyPreview !== payload.body && payload.body !== undefined) item.bodyPreview = payload.body || "";
      item.status = payload.status || "ok";
      item.resultMeta = payload.resultMeta;
      item.durationMs = payload.durationMs;
      return upsertItem(state, item);
    }
    case "run.finished":
      return terminalizeRun(state, "ok", "idle");
    case "run.failed": {
      const next = terminalizeRun(state, "err", "error");
      const errorText = typeof payload.error === "string" ? payload.error.trim() : "";
      if (!errorText) return next;
      const id = `error:${event.runId || event.seq}`;
      return upsertItem(next, {
        id,
        ordinal: next.itemOrder.length + 1,
        kind: "error",
        origin: "runtime",
        runId: event.runId,
        text: errorText,
        bodyPreview: "",
      });
    }
    case "run.cancelled":
      return terminalizeRun(state, "cancelled", "cancelled");
    case "run.interrupted":
      return terminalizeRun(state, "interrupted", "interrupted");
    default:
      return state;
  }
}

function ensureSegmentItem(state: TranscriptState, event: DurableSessionEvent, kind: string): TranscriptItem {
  const existing = state.itemsById.get(event.entityId as string);
  if (existing) return cloneItem(existing);
  return segmentFallbackItem(state, event, kind);
}

function segmentFallbackItem(state: TranscriptState, event: DurableSessionEvent, kind: string): TranscriptItem {
  return {
    id: event.entityId as string,
    ordinal: state.itemOrder.length + 1,
    kind: kind === "think" ? "think" : "pi",
    origin: "runtime",
    runId: event.runId,
    entityId: event.entityId,
    text: "",
    bodyPreview: "",
    status: "streaming",
  };
}

function toolItemFor(state: TranscriptState, event: DurableSessionEvent): TranscriptItem {
  const existing = state.itemsById.get(event.entityId as string);
  if (existing) return cloneItem(existing);
  const itemId = event.entityId as string;
  const parts = itemId.split(":");
  const toolCallId = parts.length >= 3 ? parts.slice(2).join(":") : "";
  return {
    id: itemId,
    ordinal: state.itemOrder.length + 1,
    kind: "tool",
    origin: "runtime",
    runId: event.runId,
    entityId: event.entityId,
    tool: "tool",
    toolCallId,
    text: "",
    bodyPreview: "",
    status: "running",
  };
}

function terminalizeRun(state: TranscriptState, toolStatus: string, status: TranscriptState["status"]): TranscriptState {
  const itemsById = new Map(state.itemsById);
  for (const [id, item] of itemsById) {
    if (!item.status) continue;
    const next = terminalItem(item, toolStatus);
    if (next !== item) itemsById.set(id, next);
  }
  return { ...state, itemsById, status, runId: undefined, activeSegments: new Map() };
}
