import type { SessionEventEnvelope } from "./session-recovery";
import {
  emptyTranscriptState,
  type DurableSessionEvent,
  type TranscriptItem,
  type TranscriptState,
} from "./transcript-state";

export type LegacySessionEventContext = {
  sessionId: string;
  runId?: string;
  activeToolIds: Map<string, string>;
  activeSegmentIds?: Map<string, string>;
};

type EventPayload = Record<string, any>;

function payloadOf(event: SessionEventEnvelope): EventPayload {
  return event.payload && typeof event.payload === "object" ? event.payload as EventPayload : {};
}

function sequenceOf(event: SessionEventEnvelope): string {
  return String(event.id);
}

function baseEvent(event: SessionEventEnvelope, context: LegacySessionEventContext, type: string): DurableSessionEvent {
  return {
    seq: sequenceOf(event),
    sessionId: context.sessionId,
    runId: context.runId,
    sourceId: `legacy:${sequenceOf(event)}`,
    type,
  };
}

function explicitToolCallId(payload: EventPayload): string {
  return String(payload.id || payload.toolCallId || "");
}

function toolEntityId(runId: string | undefined, callId: string): string {
  return `tool:${runId || "legacy"}:${callId}`;
}

function adaptToolStarted(event: SessionEventEnvelope, context: LegacySessionEventContext): DurableSessionEvent {
  const payload = payloadOf(event);
  const callId = explicitToolCallId(payload) || `legacy:${sequenceOf(event)}`;
  const entityId = context.activeToolIds.get(callId) || toolEntityId(context.runId, callId);
  context.activeToolIds.set(callId, entityId);
  context.activeToolIds.set(`tool:${payload.tool || "tool"}`, entityId);
  const next = baseEvent(event, context, "tool.started");
  next.entityId = entityId;
  next.payload = {
    toolCallId: callId,
    tool: payload.tool || "tool",
    args: payload.args || "",
  };
  return next;
}

function adaptToolOutput(event: SessionEventEnvelope, context: LegacySessionEventContext): DurableSessionEvent {
  const payload = payloadOf(event);
  const explicitCallId = explicitToolCallId(payload);
  const fallbackEntity = !explicitCallId && payload.tool
    ? context.activeToolIds.get(`tool:${payload.tool}`)
    : undefined;
  const callId = explicitCallId || fallbackEntity?.split(":").slice(2).join(":") || `legacy:${sequenceOf(event)}`;
  const entityId = fallbackEntity || context.activeToolIds.get(callId) || toolEntityId(context.runId, callId);
  const next = baseEvent(event, context, "tool.output");
  next.entityId = entityId;
  next.payload = { chunk: payload.chunk || "", mode: payload.mode === "replace" ? "replace" : "append" };
  return next;
}

function adaptToolFinished(event: SessionEventEnvelope, context: LegacySessionEventContext): DurableSessionEvent {
  const payload = payloadOf(event);
  const explicitCallId = explicitToolCallId(payload);
  const fallbackEntity = !explicitCallId && payload.tool
    ? context.activeToolIds.get(`tool:${payload.tool}`)
    : undefined;
  const callId = explicitCallId || fallbackEntity?.split(":").slice(2).join(":") || `legacy:${sequenceOf(event)}`;
  const entityId = fallbackEntity || context.activeToolIds.get(callId) || toolEntityId(context.runId, callId);
  context.activeToolIds.delete(callId);
  if (payload.tool) context.activeToolIds.delete(`tool:${payload.tool}`);
  const next = baseEvent(event, context, "tool.finished");
  next.entityId = entityId;
  next.payload = {
    tool: payload.tool || "tool",
    args: payload.args || "",
    body: payload.body || "",
    status: payload.status || "ok",
    ...(payload.resultMeta !== undefined ? { resultMeta: payload.resultMeta } : {}),
    ...(payload.durationMs !== undefined ? { durationMs: payload.durationMs } : {}),
  };
  return next;
}

export function createLegacyTranscriptState(
  sessionId: string,
  messages: Array<Record<string, any>> = [],
  status = "idle",
  appliedSeq = "0",
): TranscriptState {
  const state = emptyTranscriptState(sessionId);
  const normalizedMessages = Array.isArray(messages) ? messages : [];
  const items = normalizedMessages.map((message, index): TranscriptItem => {
    const id = message.id || `legacy:${message.kind || "message"}:${index + 1}`;
    return {
      id,
      ordinal: index + 1,
      kind: message.kind || "pi",
      origin: "legacy",
      runId: message.runId,
      entityId: message.entityId,
      text: message.text || "",
      tool: message.tool,
      toolCallId: message.toolCallId || message.id,
      args: message.args,
      bodyPreview: message.body || message.bodyPreview || "",
      status: message.status,
      resultMeta: message.resultMeta,
      durationMs: message.durationMs,
      attachments: message.attachments,
    };
  });
  state.itemOrder = items.map((item) => item.id);
  state.itemsById = new Map(items.map((item) => [item.id, item]));
  state.status = status as TranscriptState["status"];
  state.appliedSeq = appliedSeq;
  return state;
}

export function adaptLegacySessionEvent(
  event: SessionEventEnvelope,
  context: LegacySessionEventContext,
): DurableSessionEvent[] {
  const payload = payloadOf(event);
  switch (event.type) {
    case "session.delta": {
      const kind = payload.kind === "think" ? "think" : "pi";
      const activeSegments = context.activeSegmentIds || (context.activeSegmentIds = new Map());
      const entityId = activeSegments.get(kind) || `assistant:legacy:${kind}:${sequenceOf(event)}`;
      activeSegments.set(kind, entityId);
      const next = baseEvent(event, context, "assistant.segment.delta");
      next.entityId = entityId;
      next.payload = { kind, delta: payload.delta || "" };
      return [next];
    }
    case "session.message": {
      if (payload.kind === "user") {
        const next = baseEvent(event, context, "user.message.created");
        next.payload = { text: payload.text || "", attachments: payload.attachments || [] };
        return [next];
      }
      if (!["pi", "think"].includes(payload.kind)) {
        const next = baseEvent(event, context, "transcript.item.created");
        next.entityId = `legacy:item:${sequenceOf(event)}`;
        next.payload = payload;
        return [next];
      }
      const kind = payload.kind === "think" ? "think" : "pi";
      const activeSegments = context.activeSegmentIds || (context.activeSegmentIds = new Map());
      const entityId = activeSegments.get(kind) || `assistant:legacy:${kind}:${sequenceOf(event)}`;
      activeSegments.delete(kind);
      const next = baseEvent(event, context, "assistant.segment.committed");
      next.entityId = entityId;
      next.payload = { kind, text: payload.text || "" };
      return [next];
    }
    case "tool.started":
      return [adaptToolStarted(event, context)];
    case "tool.output":
      return [adaptToolOutput(event, context)];
    case "tool.args.delta": {
      const payload = payloadOf(event);
      const explicitCallId = explicitToolCallId(payload);
      const fallbackEntity = !explicitCallId && payload.tool
        ? context.activeToolIds.get(`tool:${payload.tool}`)
        : undefined;
      const callId = explicitCallId || fallbackEntity?.split(":").slice(2).join(":") || `legacy:${sequenceOf(event)}`;
      const next = baseEvent(event, context, "tool.args.delta");
      next.entityId = fallbackEntity || context.activeToolIds.get(callId) || toolEntityId(context.runId, callId);
      next.payload = { delta: payload.chunk || payload.delta || "" };
      return [next];
    }
    case "tool.finished":
      return [adaptToolFinished(event, context)];
    case "session.status": {
      const status = payload.status;
      if (status === "running" || status === "thinking") {
        const next = baseEvent(event, context, "run.started");
        next.payload = { status };
        return [next];
      }
      if (status === "cancelled") {
        const next = baseEvent(event, context, "run.cancelled");
        next.payload = { status };
        return [next];
      }
      if (status === "idle") {
        const next = baseEvent(event, context, "run.finished");
        next.payload = { status };
        return [next];
      }
      return [];
    }
    case "error": {
      const next = baseEvent(event, context, "run.failed");
      next.payload = { error: payload.error || payload.message || String(event.payload || "runtime failed") };
      return [next];
    }
    default:
      return [];
  }
}
