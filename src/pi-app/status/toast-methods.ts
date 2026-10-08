import { Notyf, NotyfEvent } from "notyf";
import { getSessionState } from "../../lib/api";
import { escapeHtml } from "../../lib/renderers";
import { currentUiLocale } from "../../i18n/client";
import { uiMessage } from "../../i18n/ui";
import { createSessionStreamClient } from "../sessions/session-stream-client";
import { parseFallbackChoices, parseFallbackInputs } from "../input/fallback-choices";

const TOAST_MESSAGES = {
  success: {
    title: "回答完成",
    detail: "会话回答已完成。"
  },
  choice: {
    title: "需要反馈",
    detail: "会话等待你的反馈。"
  },
  warning: {
    title: "警告",
    detail: "需要检查。"
  },
  error: {
    title: "回答失败",
    detail: "获取回答失败。"
  },
  connection: {
    title: "后端连接已断开",
    detail: "前端与后端的连接已断开，正在重新连接。"
  },
};

const TOAST_ICON_PATHS = {
  success: '<path d="m5 12 4 4L19 6"/>',
  choice: '<circle cx="12" cy="12" r="9"/><path d="M12 11v5m0-8h.01"/>',
  warning: '<path d="M10.3 4.3 2.6 18a2 2 0 0 0 1.8 3h15.2a2 2 0 0 0 1.8-3L13.7 4.3a2 2 0 0 0-3.4 0Z"/><path d="M12 9v4m0 4h.01"/>',
  error: '<circle cx="12" cy="12" r="9"/><path d="m9 9 6 6m0-6-6 6"/>',
  connection: '<circle cx="12" cy="12" r="9"/><path d="M12 11v5m0-8h.01"/>',
};

function isToastType(type) {
  return Object.prototype.hasOwnProperty.call(TOAST_MESSAGES, type);
}

const UNREAD_COMPLETED_SESSIONS_KEY = "piweb:unread-completed-sessions";
const LAST_SESSION_PROMPTS_KEY = "piweb:last-session-prompts";

const BACKEND_CONNECTION_ERROR_PATTERNS = [
  /failed to fetch/i,
  /fetch failed/i,
  /load failed/i,
  /network\s*error/i,
  /err_connection/i,
  /connection\s*(refused|reset|closed|aborted)/i,
  /backend\s*disconnected/i,
  /event\s*stream/i,
  /body\s*stream/i,
  /terminated/i,
];

const AUTH_CREDENTIAL_ERROR_PATTERNS = [
  /authentication\s*failed/i,
  /credentials?\s+may\s+have\s+expired/i,
  /no\s+api\s+key\s+found/i,
  /oauth/i,
  /unauthorized/i,
  /invalid_grant/i,
  /token\s+expired/i,
];

function detailMessage(detail) {
  return typeof detail === "string" ? detail : detail?.message || String(detail || "");
}

function isBackendConnectionError(detail) {
  const message = detailMessage(detail);
  return BACKEND_CONNECTION_ERROR_PATTERNS.some((pattern) => pattern.test(message));
}

function isAuthCredentialError(detail) {
  const message = detailMessage(detail);
  return AUTH_CREDENTIAL_ERROR_PATTERNS.some((pattern) => pattern.test(message));
}

function authWarningDetail(detail) {
  const message = detailMessage(detail).trim();
  return message
    ? `OAuth/API 密钥身份验证可能已断开。请在设置中重新登录。 (${message})`
    : "OAuth/API 密钥身份验证可能已断开。请在设置中重新登录。";
}

function toastText(kind, detail, context) {
  const message = isToastType(kind) ? TOAST_MESSAGES[kind] : TOAST_MESSAGES.success;
  const titleKey = kind === "connection" ? "connectionToastTitle" : undefined;
  const detailKey = kind === "connection" ? "connectionToastDetail" : undefined;
  const locale = currentUiLocale();
  const title = titleKey ? uiMessage(locale, titleKey) : message.title;
  const defaultDetail = detailKey ? uiMessage(locale, detailKey) : message.detail;
  const contextPrompt = toastContextPrompt(context);
  return {
    title,
    detail: detail || defaultDetail,
    workspace: toastContextWorkspace(context),
    session: toastContextSession(context),
    prompt: contextPrompt || detail || defaultDetail,
    titleI18nKey: titleKey,
    promptI18nKey: detailKey && !contextPrompt && !detail ? detailKey : undefined,
  };
}

function toastIconHtml(kind) {
  const iconPath = isToastType(kind) ? TOAST_ICON_PATHS[kind] : TOAST_ICON_PATHS.choice;
  return `<span class="toast-icon" aria-hidden="true"><svg viewBox="0 0 24 24">${iconPath}</svg></span>`;
}

function toastHtml(message, kind) {
  const titleI18n = message.titleI18nKey ? ` data-i18n="${message.titleI18nKey}"` : "";
  const promptI18n = message.promptI18nKey
    ? ` data-i18n="${message.promptI18nKey}" data-i18n-title="${message.promptI18nKey}"`
    : "";
  return [
    toastIconHtml(kind),
    `<span class="toast-copy"><strong${titleI18n}>${escapeHtml(message.title)}</strong>`,
    `<span class="toast-line toast-workspace" title="${escapeHtml(message.workspace)}">${escapeHtml(message.workspace)}</span>`,
    `<span class="toast-line toast-session" title="${escapeHtml(message.session)}">${escapeHtml(message.session)}</span>`,
    `<small class="toast-line toast-prompt" title="${escapeHtml(message.prompt)}"${promptI18n}>`,
    `${escapeHtml(message.prompt)}</small></span>`,
  ].join("");
}

function systemToastHtml(type, title, detail) {
  return [
    toastIconHtml(type),
    `<span class="toast-copy"><strong>${escapeHtml(title)}</strong>`,
    `<small class="toast-line toast-prompt" title="${escapeHtml(detail)}">${escapeHtml(detail)}</small></span>`,
  ].join("");
}

function toastContextWorkspace(context) {
  if (!context || typeof context === "string") return context || "无工作区";
  return context.workspaceName || context.workspace || context.label || "无工作区";
}

function toastContextSession(context) {
  if (!context || typeof context === "string") return "无会话";
  return context.sessionName || context.session || context.sessionId || "无会话";
}

function toastContextPrompt(context) {
  if (!context || typeof context === "string") return "";
  return context.prompt || "";
}

function toastContextSessionId(context) {
  if (!context || typeof context === "string") return undefined;
  return context.sessionId;
}

function displayName(name, id) {
  const label = String(name || "").trim();
  const value = String(id || "").trim();
  return label || value || "未知";
}

export const toastMethods = {
  ensureToastRegion() {
    if (!this.notyf) {
      this.notyf = new Notyf({
        duration: 0,
        dismissible: true,
        position: { x: "right", y: "top" },
        ripple: false,
        types: [
          { type: "success", icon: false, className: "session-toast success" },
          { type: "choice", icon: false, className: "session-toast choice" },
          { type: "warning", icon: false, className: "session-toast warning" },
          { type: "error", icon: false, className: "session-toast error" },
          { type: "connection", icon: false, className: "session-toast connection" },
        ],
      });
      this.toastDismissObserver = new MutationObserver(() => this.syncToastDismissAll());
    }
    return this.notyf;
  },

  handleToastClick() {},

  showToast(kind, detail, context = this.currentToastContext()) {
    const notyf = this.ensureToastRegion();
    const message = toastText(kind, detail, context);
    const sessionId = toastContextSessionId(context);
    const toastType = isToastType(kind) ? kind : "success";
    const notification = notyf.open({
      type: toastType,
      message: toastHtml(message, toastType),
    });
    notification.on(NotyfEvent.Click, (payload = {}) => {
      const event = (payload as { event?: Event }).event;
      const target = event?.target as Element | null | undefined;
      if (target?.closest?.(".notyf__dismiss")) return;
      this.activateToastSession(sessionId);
      this.dismissToast(notification);
    });
    this.syncToastDismissAll();
    return notification;
  },

  showSystemToast(type, title, detail, key = `${type}:${title}:${detail}`) {
    this.systemToastKeys ??= new Set();
    if (this.systemToastKeys.has(key)) return undefined;
    this.systemToastKeys.add(key);
    const toastType = isToastType(type) ? type : "warning";
    const notification = this.ensureToastRegion().open({
      type: toastType,
      message: systemToastHtml(toastType, title, detail),
    });
    this.syncToastDismissAll();
    return notification;
  },

  notifyRuntimeWarning(detail) {
    const message = authWarningDetail(detail);
    this.showSystemToast("warning", "身份验证警告", message, `auth:${message}`);
  },

  dismissToast(toast) {
    if (toast) this.notyf?.dismiss(toast);
    this.syncToastDismissAll();
  },

  dismissAllToasts() {
    this.notyf?.dismissAll();
    this.syncToastDismissAll();
  },

  syncToastDismissAll() {
    const region = document.querySelector(".notyf");
    if (!region) return;
    let button = region.querySelector(".toast-dismiss-all");
    if (!button) {
      button = document.createElement("button");
      button.type = "button";
      button.className = "toast-dismiss-all";
      button.textContent = "关闭全部";
      button.addEventListener("click", (event) => {
        event.stopPropagation();
        this.dismissAllToasts();
      });
      region.prepend(button);
      this.toastDismissObserver?.observe(region, { childList: true });
    }
    region.querySelectorAll(".notyf__dismiss-btn").forEach((dismissButton) => {
      dismissButton.setAttribute("data-i18n-aria-label", "dismissNotification");
      dismissButton.setAttribute("aria-label", uiMessage(currentUiLocale(), "dismissNotification"));
    });
    const activeToasts = [...region.querySelectorAll(".notyf__toast:not(.notyf__toast--disappear)")];
    activeToasts.forEach((toast, index) => {
      toast.style.setProperty("--toast-stack-index", String(index));
      toast.style.zIndex = String(index + 1);
    });
    button.hidden = activeToasts.length === 0;
  },

  activateToastSession(sessionId) {
    if (!sessionId) return;
    this.clearUnreadCompletedSession(sessionId);
    if (sessionId === this.dataset.activeSessionId) return;
    const row = [...this.querySelectorAll(".session-row[data-session]")]
      .find((item) => item.dataset.session === sessionId);
    if (row) this.pickSession(row);
  },

  currentToastContext() {
    const sessionId = this.dataset.activeSessionId;
    return {
      workspaceName: displayName(
        this.querySelector("[data-active-workspace]")?.textContent,
        this.dataset.activeWorkspaceId,
      ),
      sessionName: displayName(
        this.querySelector("[data-active-session-title]")?.textContent,
        sessionId,
      ),
      prompt: this.readLastSessionPrompt(sessionId),
      sessionId,
    };
  },

  notifySessionCompleted(context) {
    const sessionId = toastContextSessionId(context || this.currentToastContext());
    this.markUnreadCompletedSession(sessionId);
    this.showToast("success", undefined, context);
  },

  notifyResponseCompletedOnce(context) {
    if (this.responseCompletionToastShown || this.responseChoiceRequested) return;
    this.responseCompletionToastShown = true;
    this.notifySessionCompleted(context);
  },

  notifyChoiceRequested(context) {
    const sessionId = toastContextSessionId(context);
    const isActiveSession = !sessionId || !this.dataset.activeSessionId || sessionId === this.dataset.activeSessionId;
    if (isActiveSession && this.responseChoiceRequested) return;
    if (isActiveSession) this.responseChoiceRequested = true;
    this.showToast("choice", undefined, context);
  },

  notifyResponseFailure(detail, context) {
    if (this.responseFailureToastShown && !context) return;
    if (!context) this.responseFailureToastShown = true;
    if (isBackendConnectionError(detail)) {
      this.setConnection?.("err");
      this.showToast("connection", undefined, context);
      return;
    }
    if (isAuthCredentialError(detail)) {
      this.notifyRuntimeWarning(detail);
      return;
    }
    this.showToast("error", detail, context);
  },

  syncBackgroundSessionWatches() {
    const watched = this.backgroundSessionWatches || new Map();
    if (!this.apiConnected || typeof EventSource === "undefined") {
      watched.forEach((watch) => this.closeBackgroundSessionWatch(watch));
      watched.clear();
      this.backgroundSessionWatches = watched;
      return;
    }
    const rows = this.backgroundWatchRows();
    for (const [sessionId, watch] of watched) {
      if (rows.has(sessionId)) continue;
      this.closeBackgroundSessionWatch(watch);
      watched.delete(sessionId);
    }
    for (const [sessionId, row] of rows) {
      if (!watched.has(sessionId)) watched.set(sessionId, this.watchBackgroundSession(row));
    }
    this.backgroundSessionWatches = watched;
  },

  backgroundWatchRows() {
    const rows = new Map();
    this.querySelectorAll(".session-row.active[data-session]").forEach((row) => {
      if (row.dataset.session && row.dataset.session !== this.dataset.activeSessionId) rows.set(row.dataset.session, row);
    });
    return rows;
  },

  watchBackgroundSession(row) {
    const sessionId = row.dataset.session;
    const watch: any = { failed: false, row, wasRunning: true, stopped: false, appliedSeq: "0" };
    watch.client = createSessionStreamClient({
      sessionId,
      appliedSeq: () => watch.appliedSeq,
      onOpen: () => this.confirmConnection?.(),
      onError: () => undefined,
      onReset: () => void this.startDurableBackgroundWatch(watch),
      onEvent: (event) => {
        watch.appliedSeq = event.seq || watch.appliedSeq;
        this.handleDurableBackgroundSessionEvent(event, watch);
      },
    });
    void this.startDurableBackgroundWatch(watch);
    return watch;
  },

  async startDurableBackgroundWatch(watch) {
    if (watch.stopped) return;
    watch.client.stop();
    try {
      const state = await getSessionState(watch.row.dataset.session);
      if (watch.stopped || !this.backgroundSessionWatches?.has(watch.row.dataset.session)) return;
      watch.appliedSeq = state.seq || "0";
      watch.wasRunning = watch.wasRunning || ["running", "thinking"].includes(state.status);
      if (["idle", "cancelled"].includes(state.status)) {
        this.dismissBackgroundSessionWatch(watch.row.dataset.session, watch);
        if (watch.wasRunning && state.status === "idle" && !watch.failed) {
          this.notifySessionCompleted(this.toastContextForSessionRow(watch.row));
        }
        return;
      }
      watch.client.start();
    } catch (error) {
      if (!watch.stopped) this.notifyResponseFailure(error, this.toastContextForSessionRow(watch.row));
    }
  },

  closeBackgroundSessionWatch(watch) {
    watch.stopped = true;
    watch.client?.stop?.();

  },

  handleDurableBackgroundSessionEvent(event, watch) {
    if (!event || event.type === "heartbeat") return;
    const context = this.toastContextForSessionRow(watch.row);
    if (event.type === "run.failed") {
      watch.failed = true;
      this.notifyResponseFailure(event.payload?.error || "runtime failed", context);
      this.dismissBackgroundSessionWatch(event.sessionId, watch);
      return;
    }
    if (event.type === "assistant.segment.committed" && (
      parseFallbackChoices(event.payload?.text).length || parseFallbackInputs(event.payload?.text).length
    )) {
      this.notifyChoiceRequested(context);
      return;
    }
    if (event.type === "run.started") {
      watch.wasRunning = true;
      return;
    }
    if (!["run.finished", "run.cancelled", "run.interrupted"].includes(event.type)) return;
    this.dismissBackgroundSessionWatch(event.sessionId, watch);
    if (event.type === "run.finished" && watch.wasRunning && !watch.failed) {
      this.notifySessionCompleted(context);
    }
  },

  dismissBackgroundSessionWatch(sessionId, watch) {
    this.closeBackgroundSessionWatch(watch);
    this.backgroundSessionWatches?.delete(sessionId);
    this.markSessionRunning?.(watch.row, false);
    this.updateSessionMeta?.(watch.row, false);
    this.syncActiveWorkspaceRows?.();
  },

  readLastSessionPrompts() {
    try {
      const raw = localStorage.getItem(LAST_SESSION_PROMPTS_KEY);
      const parsed = JSON.parse(raw || "{}");
      return parsed && typeof parsed === "object" && !Array.isArray(parsed) ? parsed : {};
    } catch {
      return {};
    }
  },

  readLastSessionPrompt(sessionId) {
    if (!sessionId) return "";
    return String(this.readLastSessionPrompts()[sessionId] || "");
  },

  writeLastSessionPrompt(sessionId, prompt) {
    if (!sessionId || !prompt) return;
    const prompts = this.readLastSessionPrompts();
    prompts[sessionId] = String(prompt).trim();
    try {
      localStorage.setItem(LAST_SESSION_PROMPTS_KEY, JSON.stringify(prompts));
    } catch {
      // Ignore storage failures; prompt text will simply be absent from future toasts.
    }
  },

  readUnreadCompletedSessions() {
    try {
      const raw = localStorage.getItem(UNREAD_COMPLETED_SESSIONS_KEY);
      const parsed = JSON.parse(raw || "[]");
      return new Set(Array.isArray(parsed) ? parsed.filter(Boolean).map(String) : []);
    } catch {
      return new Set();
    }
  },

  writeUnreadCompletedSessions(sessions) {
    try {
      localStorage.setItem(UNREAD_COMPLETED_SESSIONS_KEY, JSON.stringify([...sessions]));
    } catch {
      // Ignore storage failures; visual state will still update for this page.
    }
  },

  markUnreadCompletedSession(sessionId) {
    if (!sessionId || sessionId === this.dataset.activeSessionId) return;
    const sessions = this.readUnreadCompletedSessions();
    sessions.add(String(sessionId));
    this.writeUnreadCompletedSessions(sessions);
    this.syncUnreadCompletedSessions();
  },

  clearUnreadCompletedSession(sessionId) {
    if (!sessionId) return;
    const sessions = this.readUnreadCompletedSessions();
    if (!sessions.delete(String(sessionId))) return;
    this.writeUnreadCompletedSessions(sessions);
    this.syncUnreadCompletedSessions();
  },

  syncUnreadCompletedSessions() {
    const sessions = this.readUnreadCompletedSessions();
    const visibleSessions = new Set();
    this.querySelectorAll(".session-row[data-session]").forEach((row) => {
      visibleSessions.add(row.dataset.session);
      row.classList.toggle("unread-completed", sessions.has(row.dataset.session));
    });
    let changed = false;
    for (const sessionId of sessions) {
      if (visibleSessions.has(sessionId)) continue;
      sessions.delete(sessionId);
      changed = true;
    }
    if (changed) this.writeUnreadCompletedSessions(sessions);
  },

  toastContextForSessionRow(row) {
    const workspaceId = row?.dataset.workspace;
    const workspaceName = this.querySelector(`[data-workspace-group='${workspaceId}'] .label`)?.textContent;
    const sessionId = row?.dataset.session;
    return {
      workspaceName: displayName(workspaceName, workspaceId),
      sessionName: displayName(row?.dataset.title, sessionId),
      prompt: this.readLastSessionPrompt(sessionId),
      sessionId,
    };
  },
};
