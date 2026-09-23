import { escapeHtml, renderBannerBody, renderPiBody, renderUserBody } from "../../lib/renderers";
import {
  parseFallbackChoiceAnswer,
  parseFallbackChoices,
  parseFallbackInputAnswer,
  parseFallbackInputs,
  stripFallbackChoices,
} from "../input/fallback-choices";
import { TERMINAL_SPINNER_HTML } from "../constants";
import { createTranscriptStateFromMessages } from "../sessions/transcript-state";
import { calculateOutputRate, estimateIncrementalOutputTokens, type OutputTokenEstimate } from "./output-rate";

function responseErrorText(detail) {
  if (typeof detail === "string") return detail.trim();
  if (!detail || typeof detail !== "object") return String(detail || "").trim();
  return String(detail.message || detail.error || "").trim();
}

export const messageMethods = {
  renderMessages(messages) {
    if (!this.termInner) return;
    this.streamingRows = {};
    this.responseErrorMessageShown = false;
    this.termInner.replaceChildren();
    this.resetTranscriptWindow();
    this.deferTranscriptRender = false;
    this.answeredChoiceIds = this.answeredChoiceIdsFrom(messages);
    if (!this.transcriptState || (!messages.length && this.transcriptState.sessionId === "legacy")) {
      this.transcriptState = createTranscriptStateFromMessages(
        this.dataset.activeSessionId || "legacy",
        messages,
        "idle",
        "0",
      );
      this.durableTranscriptIds = undefined;
    }
    this.transcriptItems = messages.map((message) => this.createTranscriptItem(message));
    this.renderTranscriptWindow({ stickToBottom: true });
    this.scrollTerm();
  },

  answeredChoiceIdsFrom(messages) {
    const choiceIds = new Set();
    for (const message of messages) {
      if (message.kind !== "user") continue;
      const choiceId = parseFallbackChoiceAnswer(message.text) || parseFallbackInputAnswer(message.text);
      if (choiceId) choiceIds.add(choiceId);
    }
    return choiceIds;
  },

  appendMessage(message) {
    if (!this.termInner || !message) return;
    if (this.isDuplicateMessage(message)) return;
    if (message.kind === "pi" || message.kind === "think") {
      this.removeCurrentStreamingMessage(message.kind);
    }
    this.removeLoadingMessage();
    if (this.transcriptState?.sessionId === "legacy") {
      const messages = (this.transcriptItems || []).map((item) => item.message).filter(Boolean);
      this.renderMessages([...messages, message]);
    } else {
      this.appendTranscriptMessage(message, { stickToBottom: true });
    }
    if (message.kind === "user") {
      this.disableAnsweredChoice(parseFallbackChoiceAnswer(message.text) || parseFallbackInputAnswer(message.text));
    }
    if (message.kind === "pi") this.notifyPiMessageCommitted?.(message);
    this.scrollTerm();
  },

  isDuplicateMessage(message) {
    if (message.optimistic) return false;
    if (message.kind === "tool") {
      const toolCallId = message.toolCallId || message.id;
      return !!toolCallId && (this.transcriptItems || []).some((item) => {
        const existing = item.message;
        return existing?.kind === "tool" && (existing.toolCallId || existing.id) === toolCallId;
      });
    }
    if (!["user", "pi", "think"].includes(message.kind)) return false;
    const messages = [...this.termInner.querySelectorAll(".msg:not(.loading):not(.streaming)")];
    const last = messages.at(-1);
    return last?.dataset.kind === message.kind && (last.dataset.rawText ?? last.querySelector(".body")?.textContent) === message.text;
  },

  resetOutputRate() {
    this.outputRateState = undefined;
    this.querySelectorAll("[data-output-rate]").forEach((indicator) => {
      indicator.hidden = true;
      indicator.textContent = "";
    });
  },

  addOutputRateIndicator(row) {
    const prefix = row.querySelector(".prefix");
    if (!prefix) return;
    const indicator = document.createElement("span");
    indicator.className = "output-rate";
    indicator.dataset.outputRate = "";
    indicator.hidden = true;
    prefix.append(indicator);
  },

  updateOutputRate(row, text, cumulative = false) {
    const now = Date.now();
    const state = this.outputRateState || { startedAt: now, text: "", estimate: undefined as OutputTokenEstimate | undefined };
    const nextText = cumulative ? text : `${state.text}${text}`;
    state.text = nextText;
    state.estimate = { text: nextText, tokens: estimateIncrementalOutputTokens(state.estimate, nextText) };
    this.outputRateState = state;
    const rate = calculateOutputRate(state.estimate.tokens, state.startedAt, now);
    const indicator = row?.querySelector("[data-output-rate]");
    if (!indicator) return;
    indicator.hidden = rate === null;
    indicator.textContent = rate === null ? "" : `${rate.toFixed(1)} t/s`;
  },

  syncOutputRateFromTranscript(messages) {
    const streamingMessage = [...(messages || [])].reverse().find((message) => message.kind === "pi" && message.status === "streaming");
    if (!streamingMessage || !streamingMessage.text) {
      if (this.outputRateState) this.resetOutputRate();
      return;
    }
    const rows = [...this.querySelectorAll(".msg.streaming[data-kind='pi']")];
    this.updateOutputRate(rows.at(-1), streamingMessage.text || "", true);
  },

  appendStreamingDelta(kind, delta) {
    if (!this.termInner || !delta || !["pi", "think"].includes(kind)) return;
    this.removeLoadingMessage();
    let row = this.currentStreamingRow(kind);
    if (!row) {
      row = this.messageNode({ kind, text: "", status: "streaming" });
      if (!row || row.nodeType !== Node.ELEMENT_NODE) return;
      this.streamingRows = { ...(this.streamingRows || {}), [kind]: row };
      this.appendStreamingNode(row);
    }
    const body = row.querySelector(".body");
    const nextText = `${row.dataset.rawText || ""}${delta}`;
    row.dataset.rawText = nextText;
    if (kind === "pi") {
      if (!body) return;
      body.innerHTML = renderPiBody(nextText);
      this.updateOutputRate(row, delta);
      this.notifyPiMessageCommitted?.({ kind, text: nextText });
    } else {
      const thinkingBlock = body?.querySelector(".thinking-block");
      if (!thinkingBlock) return;
      thinkingBlock.append(document.createTextNode(String(delta)));
    }
    this.scrollTerm();
  },

  removeCurrentStreamingMessage(kind) {
    this.streamingRowsForKind(kind).forEach((row) => this.removeStreamingNode(row));
    this.clearStreamingState(kind);
    if (kind === "pi") this.resetOutputRate();
  },

  currentStreamingRow(kind) {
    return this.streamingRowsForKind(kind)[0];
  },

  streamingRowsForKind(kind) {
    const selector = `.msg.streaming[data-kind='${kind}']`;
    return [...(this.streamingTail?.querySelectorAll(selector) || [])]
      .filter((row) => row.isConnected && this.streamingTail.contains(row) && row.matches(selector));
  },

  appendErrorMessage(detail) {
    const text = responseErrorText(detail);
    if (!text || this.responseErrorMessageShown) return;
    this.responseErrorMessageShown = true;
    this.appendTranscriptMessage({ kind: "error", text }, { stickToBottom: true });
    this.scrollTerm();
  },

  clearStreamingState(kind) {
    if (this.streamingRows?.[kind]) this.streamingRows = { ...(this.streamingRows || {}), [kind]: undefined };
  },

  notifyPiMessageCommitted(message) {
    if (message.kind !== "pi") return;
    const choices = parseFallbackChoices(message.text);
    const inputs = parseFallbackInputs(message.text);
    if (!this.deferTranscriptRender && (choices.length || inputs.length)) {
      this.notifyChoiceRequested?.();
      return;
    }
  },

  appendLoadingMessage() {
    if (!this.termInner || this.hasLoadingMessage()) return;
    const row = this.simpleMessage("pi loading", "agent >", "");
    row.querySelector(".body").innerHTML = `${TERMINAL_SPINNER_HTML}<span>waiting for response…</span>`;
    row.classList.add("loading");
    row.dataset.kind = "loading";
    this.loadingMessage = row;
    this.appendStreamingNode(row);
    this.scrollTerm();
  },

  hasLoadingMessage() {
    return !!this.loadingMessageNodes().length;
  },

  loadingMessageNodes() {
    const nodes = new Set(this.streamingTail?.querySelectorAll(".msg.loading") || []);
    const cached = this.loadingMessage;
    if (cached?.matches?.(".msg.loading") && this.streamingTail?.contains?.(cached)) nodes.add(cached);
    return [...nodes];
  },

  removeLoadingMessage() {
    const loadingNodes = this.loadingMessageNodes();
    loadingNodes.forEach((loading) => loading.remove?.());
    this.loadingMessage = undefined;
  },

  syncLoadingMessage() {
    if (!this.running) {
      this.removeLoadingMessage();
      return;
    }
    if (this.hasActiveTranscriptItem()) {
      this.removeLoadingMessage();
      return;
    }
    this.appendLoadingMessage();
  },

  hasActiveTranscriptItem() {
    const latestMessage = this.transcriptItems?.at(-1)?.message;
    const projectedActive = latestMessage?.kind === "pi" && latestMessage.status === "streaming";
    const selector = [
      ".msg.streaming[data-kind='pi']",
      ".fallback-choice-list",
      ".fallback-input-panel",
    ].join(", ");
    const historyActive = this.termInner?.querySelector?.(selector);
    const tailActive = this.streamingTail?.querySelector?.(selector);
    return !!(projectedActive || historyActive || tailActive);
  },

  commitStreamingMessages() {
    this.finalizeStreamingMessages();
  },

  finalizeStreamingMessages() {
    this.streamingTail?.querySelectorAll(".msg.streaming").forEach((row) => this.removeStreamingNode(row));
    this.streamingRows = {};
    this.resetOutputRate();
  },

  finishRunningTools({ status = "ok", resultMeta = "done", notifyHeight = true } = {}) {
    this.termInner?.querySelectorAll(".tool-card[data-status='running']").forEach((card) => {
      card.dataset.status = status;
      const meta = card.querySelector(".tc-meta");
      if (meta) meta.innerHTML = this.toolStatus({ status, resultMeta });
      if (notifyHeight) this.notifyTranscriptNodeHeightDidChange(card);
    });
  },

  messageNode(message) {
    if (message.kind === "banner") return this.bannerMessageNode(message);
    if (message.kind === "user") return this.userMessageNode(message);
    if (message.kind === "think") return this.thinkingMessageNode(message);
    if (message.kind === "pi") return this.assistantMessageNode(message);
    if (message.kind === "error") {
      const errorRow = this.simpleMessage("error", "error >", message.text);
      errorRow.classList.add("error");
      return errorRow;
    }
    if (message.kind === "tool") return this.toolCard(message);
    return this.simpleMessage(message.kind || "annotation", "agent >", message.text || JSON.stringify(message));
  },

  thinkingPrefix() {
    return this.runtimeStatus?.model || "—";
  },

  syncThinkingPrefixes() {
    const prefix = this.thinkingPrefix();
    this.termInner?.querySelectorAll(".prefix.think").forEach((node) => {
      node.textContent = prefix;
    });
  },

  bannerMessageNode(message) {
    const bannerElement = document.createElement("pre");
    bannerElement.className = "ascii-banner";
    bannerElement.innerHTML = renderBannerBody(message.text);
    return bannerElement;
  },

  thinkingMessageNode(message) {
    const messageRow = this.simpleMessage("think", this.thinkingPrefix(), "");
    if (message.status === "streaming") messageRow.classList.add("streaming");
    messageRow.querySelector(".body").innerHTML =
      `<div class="thinking-block"><span class="label">thinking</span>${escapeHtml(message.text)}</div>`;
    return messageRow;
  },

  assistantMessageNode(message) {
    const streaming = message.status === "streaming";
    const choices = streaming ? [] : parseFallbackChoices(message.text);
    const inputs = streaming ? [] : parseFallbackInputs(message.text);
    const text = stripFallbackChoices(message.text);
    const messageRow = this.simpleMessage("pi", "agent >", "");
    const body = messageRow.querySelector(".body");
    messageRow.dataset.rawText = message.text;
    body.classList.add("markdown-body");
    if (streaming) {
      messageRow.classList.add("streaming");
      this.addOutputRateIndicator(messageRow);
      body.innerHTML = renderPiBody(message.text || "");
      return messageRow;
    }
    body.innerHTML = renderPiBody(choices.length || inputs.length ? text : message.text);
    if (!choices.length && !inputs.length) return messageRow;

    const fragment = document.createDocumentFragment();
    fragment.append(messageRow);
    for (const choice of choices) fragment.append(this.choicePanel(choice));
    for (const input of inputs) fragment.append(this.inputPanel(input));
    return fragment;
  },

  userMessageNode(message) {
    const messageRow = this.simpleMessage("user", "您 >", "");
    const body = messageRow.querySelector(".body");
    messageRow.dataset.rawText = message.text;
    body.classList.add("markdown-body");
    body.innerHTML = renderUserBody(message.text);
    const images = (message.attachments || []).filter((item) => item.type === "image" && item.dataUrl);
    if (!images.length) return messageRow;
    const list = document.createElement("div");
    list.className = "msg-attachments";
    for (const image of images) {
      const preview = document.createElement("img");
      preview.className = "msg-image";
      preview.src = image.dataUrl;
      preview.alt = image.name || "attached image";
      list.append(preview);
    }
    body?.append(list);
    return messageRow;
  },

  choicePanel(choice) {
    const panel = document.createElement("div");
    panel.className = "fallback-choice-list";
    panel.dataset.choiceId = choice.id;
    panel.innerHTML = [
      `<div class="choice-head"><span>choice</span><strong></strong></div>`,
      `<div class="choice-options"></div>`,
    ].join("");
    panel.querySelector("strong").textContent = choice.question;
    const options = panel.querySelector(".choice-options");
    for (const option of choice.options) {
      const button = document.createElement("button");
      button.type = "button";
      button.dataset.action = "fallback-choice";
      button.dataset.choiceId = choice.id;
      button.dataset.choiceValue = option.value;
      button.innerHTML = `<span class="choice-label"></span>${option.description ? `<small></small>` : ""}`;
      button.querySelector(".choice-label").textContent = option.label;
      if (option.description) button.querySelector("small").textContent = option.description;
      options.append(button);
    }
    const custom = document.createElement("div");
    custom.className = "choice-custom";
    custom.innerHTML = [
      `<input type="text" placeholder="Type a custom answer" data-choice-custom-input>`,
      `<button type="button" data-action="fallback-choice-custom"`,
      ` data-choice-id="${escapeHtml(choice.id)}">Send</button>`,
    ].join("");
    panel.append(custom);
    return panel;
  },

  disableAnsweredChoice(choiceId) {
    if (!choiceId) return;
    this.answeredChoiceIds ??= new Set();
    this.answeredChoiceIds.add(choiceId);
    this.disableRenderedChoice(choiceId);
  },

  syncAnsweredChoices() {
    for (const choiceId of this.answeredChoiceIds || []) this.disableRenderedChoice(choiceId);
  },

  inputPanel(input) {
    const panel = document.createElement("div");
    panel.className = "fallback-input-panel";
    panel.dataset.inputId = input.id;
    panel.innerHTML = [
      `<div class="choice-head"><span>input</span><strong></strong></div>`,
      `<textarea rows="4" data-fallback-input placeholder="${escapeHtml(input.placeholder)}"></textarea>`,
      `<button type="button" data-action="fallback-input-submit" data-input-id="${escapeHtml(input.id)}">Send</button>`,
    ].join("");
    panel.querySelector("strong").textContent = input.question;
    panel.querySelector("textarea")?.setAttribute("aria-label", input.question);
    if (!input.multiline) panel.querySelector("textarea").rows = 1;
    return panel;
  },

  disableRenderedChoice(choiceId) {
    const panels = [...this.termInner?.querySelectorAll(".fallback-choice-list, .fallback-input-panel") ?? []];
    const panel = panels.find((item) => item.dataset.choiceId === choiceId || item.dataset.inputId === choiceId);
    panel?.classList.add("answered");
    panel?.querySelectorAll("button, input, textarea").forEach((item) => item.disabled = true);
  },

  async copyCodeBlock(button) {
    const code = button?.closest?.(".code-block")?.querySelector?.("pre code")?.textContent ?? "";
    if (!code) return;
    button.disabled = true;
    try {
      await this.copyTextToClipboard(code);
      this.markCodeCopyButton(button, "copied");
    } catch {
      this.markCodeCopyButton(button, "failed");
    } finally {
      button.disabled = false;
    }
  },

  async copyTextToClipboard(text) {
    if (navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(text);
      return;
    }
    const textarea = document.createElement("textarea");
    textarea.value = text;
    textarea.setAttribute("readonly", "");
    textarea.style.position = "fixed";
    textarea.style.opacity = "0";
    document.body.append(textarea);
    textarea.select();
    const copied = document.execCommand?.("copy");
    textarea.remove();
    if (copied === false) throw new Error("copy failed");
  },

  markCodeCopyButton(button, status) {
    if (!button) return;
    if (button.copyResetTimer) window.clearTimeout(button.copyResetTimer);
    button.dataset.copyStatus = status;
    button.textContent = status === "copied" ? "copied" : "failed";
    button.copyResetTimer = window.setTimeout(() => {
      button.dataset.copyStatus = "";
      button.textContent = "copy";
      button.copyResetTimer = undefined;
    }, 1400);
  },

  simpleMessage(kind, prefix, text) {
    const row = document.createElement("div");
    row.className = "msg";
    row.dataset.kind = kind.split(" ")[0];
    row.dataset.rawText = text;
    row.innerHTML = `<div class="prefix ${kind}"></div><div class="body"></div>`;
    row.querySelector(".prefix").textContent = prefix;
    row.querySelector(".body").textContent = text;
    return row;
  },

};
