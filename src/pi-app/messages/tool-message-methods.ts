import { currentUiLocale } from "../../i18n/client";
import { uiMessage } from "../../i18n/ui";
import { escapeHtml } from "../../lib/renderers";
import { captureToolOutputScroll, updateToolBody } from "./tool-output-rendering";

const TOOL_ICON_PATHS = {
  "book-open": `<path d="M12 7v14"/><path d="M3 18a1 1 0 0 1-1-1V4a1 1 0 0 1 1-1h5a4 4 0 0 1 4 4 4 4 0 0 1 4-4h5a1 1 0 0 1 1 1v13a1 1 0 0 1-1 1h-6a3 3 0 0 0-3 3 3 3 0 0 0-3-3z"/>`,
  "file-plus": `<path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><path d="M14 2v6h6"/><path d="M12 18v-6"/><path d="M9 15h6"/>`,
  pencil: `<path d="M21.2 6.8 17.2 2.8a2 2 0 0 0-2.8 0L3 14.2V21h6.8L21.2 9.6a2 2 0 0 0 0-2.8z"/><path d="m14 5 5 5"/>`,
  terminal: `<path d="m4 17 6-6-6-6"/><path d="M12 19h8"/>`,
  cpu: `<rect x="6" y="6" width="12" height="12" rx="2"/><path d="M9 1v3"/><path d="M15 1v3"/><path d="M9 20v3"/><path d="M15 20v3"/><path d="M20 9h3"/><path d="M20 15h3"/><path d="M1 9h3"/><path d="M1 15h3"/>`,
  globe: `<circle cx="12" cy="12" r="10"/><path d="M2 12h20"/><path d="M12 2a15.3 15.3 0 0 1 0 20"/><path d="M12 2a15.3 15.3 0 0 0 0 20"/>`,
  search: `<circle cx="11" cy="11" r="8"/><path d="m21 21-4.3-4.3"/>`,
  braces: `<path d="M8 3H7a2 2 0 0 0-2 2v4a2 2 0 0 1-2 2 2 2 0 0 1 2 2v4a2 2 0 0 0 2 2h1"/><path d="M16 21h1a2 2 0 0 0 2-2v-4a2 2 0 0 1 2-2 2 2 0 0 1-2-2V7a2 2 0 0 0-2-2h-1"/>`,
  "file-text": `<path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><path d="M14 2v6h6"/><path d="M16 13H8"/><path d="M16 17H8"/><path d="M10 9H8"/>`,
  "circle-question-mark": `<circle cx="12" cy="12" r="10"/><path d="M9.1 9a3 3 0 1 1 5.8 1c-.5 1-1.4 1.5-2.1 2-.6.4-.8.8-.8 2"/><path d="M12 17h.01"/>`,
  users: `<path d="M16 21v-2a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v2"/><circle cx="9" cy="7" r="4"/><path d="M22 21v-2a4 4 0 0 0-3-3.87"/><path d="M16 3.13a4 4 0 0 1 0 7.75"/>`,
  plug: `<path d="M12 22v-5"/><path d="M9 8V2"/><path d="M15 8V2"/><path d="M18 8v5a6 6 0 0 1-12 0V8Z"/>`,
  "git-branch": `<line x1="6" x2="6" y1="3" y2="15"/><circle cx="18" cy="6" r="3"/><circle cx="6" cy="18" r="3"/><path d="M18 9a9 9 0 0 1-9 9"/>`,
  "circle-check": `<circle cx="12" cy="12" r="10"/><path d="m9 12 2 2 4-4"/>`,
  hammer: `<path d="m15 12-8.5 8.5a2.1 2.1 0 0 1-3-3L12 9"/><path d="m17.6 5.4 1.8-1.8"/><path d="m14 7 3 3"/><path d="M5 11 2 8l6-6 3 3"/>`,
  package: `<path d="m7.5 4.3 9 5.2"/><path d="M21 8a2 2 0 0 0-1-1.7l-7-4a2 2 0 0 0-2 0l-7 4A2 2 0 0 0 3 8v8a2 2 0 0 0 1 1.7l7 4a2 2 0 0 0 2 0l7-4a2 2 0 0 0 1-1.7Z"/><path d="m3.3 7 8.7 5 8.7-5"/><path d="M12 22V12"/>`,
};

const TOOL_ICON_NAMES = {
  read: "book-open",
  write: "file-plus",
  edit: "pencil",
  ctx_execute: "cpu",
  ctx_batch_execute: "cpu",
  ctx_execute_file: "cpu",
  ctx_search: "cpu",
  ctx_index: "cpu",
  ctx_fetch_and_index: "cpu",
  web_search: "globe",
  fetch_content: "search",
  get_search_content: "search",
  code_search: "braces",
  document_parse: "file-text",
  ask_user_question: "circle-question-mark",
  teams: "users",
  subagent: "users",
  mcp: "plug",
  git: "git-branch",
};

function normalizeToolName(name) {
  return String(name || "").trim().replace(/^functions\./, "").replace(/^multi_tool_use\./, "");
}

function commandText(message) {
  const chunks = [message?.args, message?.body].filter(Boolean).map(String);
  return chunks.join("\n").toLowerCase();
}

function commandIconName(text) {
  if (/\bgit\b|\bgh\b/.test(text)) return "git-branch";
  if (/\b(go test|bun test|npm test|pnpm test|yarn test|vitest|pytest|cargo test)\b/.test(text)) return "circle-check";
  if (/\b(bun|npm|pnpm|yarn)\s+(run\s+)?build\b|\b(go build|cargo build)\b/.test(text)) return "hammer";
  if (/\b(bun|npm|pnpm|yarn)\s+(add|install|i)\b|\bpip\s+install\b|\bgo\s+get\b/.test(text)) return "package";
  return undefined;
}

export function isShellTool(tool) {
  return ["bash", "shell", "sh", "zsh", "terminal"].includes(normalizeToolName(tool));
}

function toolIconName(message) {
  const tool = normalizeToolName(message?.tool);
  if (isShellTool(tool)) return commandIconName(commandText(message)) || "terminal";
  return TOOL_ICON_NAMES[tool];
}

export function toolIconHtml(message) {
  const iconName = toolIconName(message);
  const paths = iconName && TOOL_ICON_PATHS[iconName];
  if (!paths) return "●";
  return `<svg class="tc-icon" data-tool-icon="${iconName}" aria-hidden="true" viewBox="0 0 24 24">${paths}</svg>`;
}

function formatToolValue(value) {
  if (typeof value === "string") return value;
  if (value === null || value === undefined) return "";
  if (Array.isArray(value)) return `[${value.map(formatToolValue).join(", ")}]`;
  if (typeof value === "object") return "{…}";
  return String(value);
}

export function formatToolArgs(tool, args) {
  if (!args) return "";
  const parsed = typeof args === "object" ? args : parseToolArgs(args);
  if (parsed === undefined) {
    const raw = String(args).trim();
    return /^[{[]/.test(raw) ? "" : String(args);
  }
  if (!parsed) return "";
  if (typeof parsed === "string") return parsed;
  const preferredKeys = ["command", "path", "query", "url", "pattern", "prompt", "text"];
  const key = preferredKeys.find((candidate) => parsed[candidate] !== undefined);
  if (key) return formatToolValue(parsed[key]);
  return Object.entries(parsed)
    .slice(0, 3)
    .map(([name, value]) => `${name}: ${formatToolValue(value)}`)
    .join(" · ");
}

function parseToolArgs(args) {
  try {
    const parsed = JSON.parse(String(args));
    return parsed && typeof parsed === "object" ? parsed : parsed;
  } catch {
    return undefined;
  }
}

function latestToolCard(app, tool, id) {
  const cards = [...app.querySelectorAll(".tool-card")].reverse();
  if (id) return cards.find((item) => item.dataset.toolCallId === id);
  return cards.find((item) => item.dataset.tool === tool);
}

export const toolMessageMethods = {
  toolCard(message) {
    const card = document.createElement("details");
    card.className = `tool-card stream-card${isShellTool(message.tool) ? " command-card" : ""}`;
    card.open = true;
    card.dataset.tool = message.tool || "tool";
    card.dataset.toolCallId = message.toolCallId || message.id || message.key || "";
    card.dataset.status = message.status || "";
    card.innerHTML = this.toolCardTemplate(message);
    card.querySelector(".tc-name").textContent = message.tool || "tool";
    const args = formatToolArgs(message.tool, message.args);
    card.querySelector(".tc-args").textContent = args;
    card.querySelector(".tc-command").hidden = !args;
    card.querySelector(".tc-meta").innerHTML = this.toolStatus(message);
    if (message.body) updateToolBody(card.querySelector(".tc-body"), message.body);
    return card;
  },

  toolCardTemplate(message) {
    const locale = currentUiLocale();
    const shell = isShellTool(message.tool);
    const titleKey = shell ? "commandExecution" : "toolExecution";
    const outputLabel = escapeHtml(uiMessage(locale, "toolOutput"));
    return [
      '<summary class="tc-head stream-summary">',
      `<span class="tc-glyph">${toolIconHtml(message)}</span>`,
      `<span class="tc-title stream-label" data-i18n="${titleKey}">`,
      `${escapeHtml(uiMessage(locale, titleKey))}</span><span class="tc-name"></span>`,
      '<span class="tc-meta"></span></summary><div class="tc-content">',
      `<div class="tc-command"><span class="tc-prompt" aria-hidden="true">${shell ? "$" : "→"}</span>`,
      '<span class="tc-args"></span></div>',
      `<div class="tc-output-label" data-i18n="toolOutput">${outputLabel}</div>`,
      `<div class="tc-body" data-i18n-aria-label="toolOutput" aria-label="${outputLabel}"></div></div>`,
    ].join("");
  },

  patchToolCard(card, rendered, message) {
    card.dataset.status = rendered.dataset.status || "";
    card.querySelector(".tc-glyph").innerHTML = rendered.querySelector(".tc-glyph").innerHTML;
    card.querySelector(".tc-name").textContent = rendered.querySelector(".tc-name").textContent;
    card.querySelector(".tc-args").textContent = rendered.querySelector(".tc-args").textContent;
    card.querySelector(".tc-command").hidden = rendered.querySelector(".tc-command").hidden;
    card.querySelector(".tc-meta").innerHTML = rendered.querySelector(".tc-meta").innerHTML;
    updateToolBody(card.querySelector(".tc-body"), message.body || "");
  },

  toolStatus(message) {
    if (message.status === "running") return this.runningToolStatus();
    if (message.status === "err") return this.errorToolStatus(message);
    return this.completedToolStatus(message);
  },

  runningToolStatus() {
    const label = escapeHtml(uiMessage(currentUiLocale(), "running"));
    return `<span class="stream-state running" data-i18n="running">${label}</span>`;
  },

  errorToolStatus(message) {
    const label = escapeHtml(uiMessage(currentUiLocale(), "executionFailed"));
    const result = message.resultMeta ? `<span class="tc-result">${escapeHtml(message.resultMeta)}</span>` : "";
    return `<span class="stream-state err" data-i18n="executionFailed">${label}</span>${result}`;
  },

  completedToolStatus(message) {
    const label = escapeHtml(uiMessage(currentUiLocale(), "executionCompleted"));
    const duration = message.durationMs !== undefined ? `${message.durationMs} ms` : "";
    const result = [duration, message.resultMeta].filter(Boolean).join(" · ");
    const detail = result ? `<span class="tc-result">${escapeHtml(result)}</span>` : "";
    return `<span class="stream-state ok" data-i18n="executionCompleted">${label}</span>${detail}`;
  },

  updateToolArgs(payload) {
    if (!payload?.chunk) return;
    const card = latestToolCard(this, payload?.tool, payload?.id);
    if (!card) return;
    card.__toolArgsText = `${card.__toolArgsText || ""}${payload.chunk}`;
    const formatted = formatToolArgs(payload.tool, card.__toolArgsText);
    if (!formatted) return;
    card.querySelector(".tc-args").textContent = formatted;
    card.querySelector(".tc-command").hidden = false;
    card.querySelector(".tc-glyph").innerHTML = toolIconHtml({ tool: payload.tool, args: card.__toolArgsText });
  },

  appendToolOutput(payload) {
    if (!payload?.chunk) return;
    const card = latestToolCard(this, payload?.tool, payload?.id);
    if (!card) return;
    const body = card.querySelector(".tc-body");
    const fullBody = `${body?.__fullToolBody || body?.textContent || ""}${payload.chunk}`;
    updateToolBody(body, fullBody);
    this.renderTranscriptWindow?.({ stickToBottom: true });
    this.scrollTerm?.();
  },

  finishTool(message) {
    const card = latestToolCard(this, message?.tool, message?.id);
    if (!card) {
      this.appendMessage(message);
      return;
    }
    captureToolOutputScroll(card.querySelector(".tc-body"));
    const next = this.toolCard(message);
    next.open = card.open;
    const wrapper = card.closest?.(".transcript-item");
    const item = this.transcriptItems?.find((candidate) => String(candidate.id) === wrapper?.dataset?.transcriptItem);
    if (item) item.message = message;
    card.replaceWith(next);
    if (item) this.bindTranscriptCardState(item, wrapper);
  },
};
