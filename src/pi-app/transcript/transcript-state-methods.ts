import type { TranscriptState } from "../sessions/transcript-state";

export function selectTranscriptRenderItem(state: TranscriptState | undefined, itemId: string) {
  if (!state || !itemId) return undefined;
  return state.itemsById.get(itemId);
}

export function selectOrderedTranscriptItems(state: TranscriptState | undefined) {
  if (!state) return [];
  return state.itemOrder
    .map((id) => state.itemsById.get(id))
    .filter((item): item is NonNullable<typeof item> => !!item);
}

export function selectToolItemIdForCall(state: TranscriptState | undefined, runId: string, toolCallId: string) {
  if (!state || !runId || !toolCallId) return undefined;
  return state.toolsByCallId.get(`${runId}:${toolCallId}`);
}
