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

export function storeFullToolBody(element, body = "") {
  if (!isLargeToolBody(body)) return;
  element.__fullToolBody = body;
  element.dataset.largeToolBody = "true";
}

export function fullToolBody(element) {
  return element?.__fullToolBody || element?.dataset?.fullToolBody || "";
}
