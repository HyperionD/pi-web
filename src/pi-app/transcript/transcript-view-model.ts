import type { TranscriptItem, TranscriptState } from "../sessions/transcript-state";

export type TranscriptViewMessage = {
  key: string;
  kind: TranscriptItem["kind"];
  text: string;
  tool?: string;
  toolCallId?: string;
  args?: string;
  body?: string;
  status?: string;
  resultMeta?: string;
  durationMs?: number;
  attachments?: unknown[];
};

export type TranscriptViewModel = {
  sessionId: string;
  status: TranscriptState["status"];
  runId?: string;
  items: TranscriptViewMessage[];
};

export function selectTranscriptViewMessage(item: TranscriptItem): TranscriptViewMessage {
  const message: TranscriptViewMessage = {
    key: item.id,
    kind: item.kind,
    text: item.text || "",
  };
  if (item.tool) message.tool = item.tool;
  if (item.toolCallId) message.toolCallId = item.toolCallId;
  if (item.args) message.args = item.args;
  if (item.bodyPreview) message.body = item.bodyPreview;
  if (item.status) message.status = item.status;
  if (item.resultMeta) message.resultMeta = item.resultMeta;
  if (item.durationMs !== undefined) message.durationMs = item.durationMs;
  if (item.attachments?.length) message.attachments = item.attachments;
  return message;
}

export function selectTranscriptView(state: TranscriptState): TranscriptViewModel {
  const items = state.itemOrder
    .map((id) => state.itemsById.get(id))
    .filter((item): item is TranscriptItem => !!item)
    .map(selectTranscriptViewMessage);
  return {
    sessionId: state.sessionId,
    status: state.status,
    ...(state.runId ? { runId: state.runId } : {}),
    items,
  };
}
