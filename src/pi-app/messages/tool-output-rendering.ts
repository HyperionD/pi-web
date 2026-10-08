import { renderAnsiBody } from "../../lib/renderers";

const LARGE_TOOL_BODY_CHARS = 16_000;
const TOOL_BODY_PREVIEW_CHARS = 4_000;

export function isLargeToolBody(body = "") {
  return body.length > LARGE_TOOL_BODY_CHARS;
}

export function renderToolBodyPreview(body = "") {
  if (!isLargeToolBody(body)) return renderAnsiBody(body);
  const tail = body.slice(-TOOL_BODY_PREVIEW_CHARS);
  const omitted = body.length - tail.length;
  return [
    renderAnsiBody(tail),
    `<div class="tc-body-notice">`,
    `Large output previewed for performance. ${omitted.toLocaleString()} characters omitted.`,
    ` <button type="button" data-action="show-full-tool-output">show full output</button>`,
    `</div>`,
  ].join("");
}

export function renderToolBody(body = "", { full = false } = {}) {
  if (!full && isLargeToolBody(body)) return renderToolBodyPreview(body);
  return renderAnsiBody(body);
}

export type ToolOutputScrollState = { followLatest: boolean; scrollTop: number };

function toolOutputScrollState(element): ToolOutputScrollState {
  element.__toolOutputScrollState ??= { followLatest: true, scrollTop: 0 };
  return element.__toolOutputScrollState;
}

export function captureToolOutputScroll(element) {
  if (!element?.__toolOutputScrollReady || !element.isConnected || !element.clientHeight) return;
  const state = toolOutputScrollState(element);
  state.followLatest = element.scrollHeight - element.scrollTop - element.clientHeight < 24;
  state.scrollTop = element.scrollTop;
}

export function restoreToolOutputScroll(element) {
  if (!element?.isConnected || !element.clientHeight) return;
  const state = toolOutputScrollState(element);
  element.scrollTop = state.followLatest ? element.scrollHeight : state.scrollTop;
  element.__toolOutputScrollReady = true;
  state.scrollTop = element.scrollTop;
}

function scheduleToolOutputScroll(element) {
  if (element.__toolOutputScrollFrame != null) return;
  element.__toolOutputScrollFrame = window.requestAnimationFrame(() => {
    element.__toolOutputScrollFrame = undefined;
    restoreToolOutputScroll(element);
  });
}

export function bindToolOutputScroll(element, state?: ToolOutputScrollState): ToolOutputScrollState {
  if (element.__toolOutputScrollBound) return toolOutputScrollState(element);
  element.__toolOutputScrollBound = true;
  element.__toolOutputScrollReady = false;
  element.__toolOutputScrollState = state || toolOutputScrollState(element);
  element.addEventListener("scroll", () => captureToolOutputScroll(element), { passive: true });
  const card = element.closest("details");
  card?.addEventListener("toggle", () => {
    if (card.open) scheduleToolOutputScroll(element);
  });
  // Detached and collapsed cards have no scroll geometry until their next visible frame.
  scheduleToolOutputScroll(element);
  return toolOutputScrollState(element);
}

export function updateToolBody(element, body = "") {
  if (!element) return;
  captureToolOutputScroll(element);
  element.innerHTML = renderToolBody(body);
  element.__fullToolBody = body;
  delete element.dataset.largeToolBody;
  storeFullToolBody(element, body);
  restoreToolOutputScroll(element);
}

export function storeFullToolBody(element, body = "") {
  if (!isLargeToolBody(body)) return;
  element.__fullToolBody = body;
  element.dataset.largeToolBody = "true";
}

export function fullToolBody(element) {
  return element?.__fullToolBody || element?.dataset?.fullToolBody || "";
}
