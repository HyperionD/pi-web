import { applyUiLocale, currentUiLocale, setUiLocale } from "../i18n/client";
import { normalizeUiLocale } from "../i18n/locales";
import { escapeHtml } from "../lib/renderers";
import {
  compareEventCursors,
  normalizeEventCursor,
  shouldResyncAfterGap,
} from "./sessions/session-recovery";
import { SPINNER_FRAME_COUNT } from "./constants";
import { filePreviewMethods } from "./editor/file-preview-methods";
import { attachmentMethods } from "./input/attachment-methods";
import { inputMethods } from "./input/input-methods";
import { promptDraftMethods } from "./input/prompt-draft-methods";
import { messageMethods } from "./messages/message-methods";
import { toolMessageMethods } from "./messages/tool-message-methods";
import { sessionMethods } from "./sessions/session-methods";
import { layoutMethods } from "./status/layout-methods";
import { compactionMethods } from "./status/compaction-methods";
import { runtimeStatusMethods } from "./status/runtime-status-methods";
import { toastMethods } from "./status/toast-methods";
import { transcriptWindowMethods } from "./transcript/transcript-window-methods";
import { oauthMethods } from "./workspace/oauth-methods";
import { appConfigMethods } from "./workspace/app-config-methods";
import { settingsMethods } from "./workspace/settings-methods";
import { workspaceMethods } from "./workspace/workspace-methods";

class PiApp extends HTMLElement {
  connectedCallback() {
    if (this.bound) return;
    this.bound = true;
    this.prompt = this.querySelector(".prompt-textarea");
    this.sendButton = this.querySelector(".send-btn");
    this.stopButton = this.querySelector(".stop-btn");
    this.attachButton = this.querySelector(".attach-btn");
    this.fileInput = this.querySelector("[data-file-input]");
    this.attachments = this.querySelector(".attach-chips");
    this.slashPopover = this.querySelector(".slash-pop");
    this.settingsModal = this.querySelector("[data-settings-modal]");
    this.termInner = this.querySelector(".term-inner");
    this.streamingTail = this.querySelector("[data-streaming-tail]");
    this.initTranscriptWindow();
    this.sessionEventCursors = new Map();
    this.sessionViewGeneration = Symbol("initial-session-view");
    this.sessionResync = undefined;
    this.apiConnected = false;
    this.currentFolder = "~";
    this.running = false;
    this.compacting = false;
    this.attachmentContents = [];
    this.spinnerIndex = 0;
    this.runtimeStatus = {};
    applyUiLocale(currentUiLocale(), this);
    this.restorePromptDraft();
    this.installViewportSizing();
    this.installFilePreviewUnloadGuard();
    this.bindDomEvents();
    this.restoreSidebar();
    this.syncUnreadCompletedSessions?.();
    this.updatePrompt();
    this.updatePromptMeta();
    this.syncCompactionControl?.();
    this.scrollTerm();
    this.startSpinners();
    this.startRuntimeStatusPolling();
    this.bootstrapAPI();
  }
  disconnectedCallback() {
    this.destroyFilePreviewEditor?.();
    this.stopDurableStream?.();
    this.backgroundSessionWatches?.forEach((watch) => {
      watch.client?.stop?.();
    });
    this.backgroundSessionWatches?.clear?.();
    if (this.spinnerTimer) clearInterval(this.spinnerTimer);
    if (this.runtimeStatusTimer) clearInterval(this.runtimeStatusTimer);
    if (this.connectionErrorTimer) clearTimeout(this.connectionErrorTimer);
    this.toastDismissObserver?.disconnect?.();
    this.clearStreamingTail?.();
    this.durableTranscriptRenderScheduler?.reset?.();
    if (this.scrollFrame != null) {
      window.cancelAnimationFrame(this.scrollFrame);
      this.scrollFrame = undefined;
    }
    this.destroyTranscriptVirtualScroller?.();
    this.uninstallViewportSizing?.();
    this.uninstallFilePreviewUnloadGuard?.();
  }
  installViewportSizing() {
    const applyViewportHeight = () => {
      const viewport = window.visualViewport;
      const height = viewport?.height || window.innerHeight;
      if (height > 0) this.style.setProperty("--app-viewport-height", `${height}px`);
    };
    const viewport = window.visualViewport;
    applyViewportHeight();
    window.addEventListener("resize", applyViewportHeight);
    viewport?.addEventListener("resize", applyViewportHeight);
    viewport?.addEventListener("scroll", applyViewportHeight);
    this.uninstallViewportSizing = () => {
      window.removeEventListener("resize", applyViewportHeight);
      viewport?.removeEventListener("resize", applyViewportHeight);
      viewport?.removeEventListener("scroll", applyViewportHeight);
    };
  }

  startRuntimeStatusPolling() {
    if (this.runtimeStatusTimer) return;
    this.runtimeStatusTimer = setInterval(() => this.loadRuntimeStatus?.(), 15000);
  }

  startSpinners() {
    if (this.spinnerTimer) return;
    this.spinnerTimer = setInterval(() => this.tickSpinners(), 150);
  }

  tickSpinners() {
    this.spinnerIndex = (this.spinnerIndex + 1) % SPINNER_FRAME_COUNT;
    this.querySelectorAll(".spinner").forEach((spinner) => {
      spinner.dataset.frame = String(this.spinnerIndex);
    });
  }

  bindDomEvents() {
    this.addEventListener("click", (event) => (this as any).handleAppClick(event));
    this.querySelector("[data-shell-form]")?.addEventListener("submit", (event) => this.submitShellCommand(event));
    this.querySelector("[data-settings-form]")?.addEventListener("submit", (event) => this.saveSettingsForm(event));
    this.querySelector("[data-admin-password-setup-form]")?.addEventListener("submit", (event) => this.submitAdminPasswordSetup(event));
    this.querySelector("[data-settings-scope]")?.addEventListener("change", () => this.fillSettingsForm());
    this.querySelector("[data-settings-location]")?.addEventListener("change", () => this.fillSettingsForm());
    this.querySelector("[data-ui-language]")?.addEventListener("change", (event) => {
      const locale = normalizeUiLocale((event.currentTarget as HTMLSelectElement).value);
      if (locale) setUiLocale(locale, this);
    });
    this.querySelector("[data-setting='defaultProvider']")?.addEventListener("change", (event) => {
      this.syncCustomSettingInput(event.currentTarget);
      this.fillModelControls();
    });
    this.querySelector("[data-setting='defaultModel']")?.addEventListener("change", (event) => {
      this.syncCustomSettingInput(event.currentTarget);
    });
    this.sendButton?.addEventListener("click", () => this.submitPrompt());
    this.stopButton?.addEventListener("click", () => this.cancelActiveSession());
    this.prompt?.addEventListener("input", () => this.updatePrompt());
    this.prompt?.addEventListener("paste", (event) => void this.handlePromptPaste(event));
    this.prompt?.addEventListener("keydown", (event) => {
      const isEnter = event.key === "Enter";
      const shouldNavigateSlashCommands = this.slashPopover
        && !this.slashPopover.hidden
        && !event.shiftKey
        && !event.isComposing
        && ["ArrowDown", "ArrowUp", "Enter"].includes(event.key);
      if (shouldNavigateSlashCommands) {
        this.navigateList(event, ".slash-item", (item) => this.pickSlash(item.dataset.slash));
        return;
      }
      if (isEnter && !event.shiftKey && !event.isComposing) {
        event.preventDefault();
        void this.submitPrompt();
        return;
      }
      if ((event.metaKey || event.ctrlKey) && isEnter) {
        event.preventDefault();
        void this.submitPrompt();
      }
    });
    this.attachButton?.addEventListener("click", () => this.fileInput?.click());
    this.fileInput?.addEventListener("change", () => this.addFiles(this.fileInput.files));
    this.querySelector(".sb-resizer")?.addEventListener("pointerdown", (event) => this.startResize(event));
    this.querySelectorAll(".fp-resizer").forEach((resizer) => {
      resizer.addEventListener("pointerdown", (event) => this.startInspectorResize(event));
    });
    window.addEventListener("pi-workspace-tree:refresh", (event) => {
      const workspaceId = this.dataset.activeWorkspaceId;
      const selectedPath = (event as CustomEvent)?.detail?.selectedPath || "";
      if (workspaceId) void this.loadWorkspaceMeta(workspaceId, { selectedPath });
    });
    window.addEventListener("pi-workspace-file:open", (event) => {
      const path = (event as CustomEvent)?.detail?.path;
      if (path) void this.openFilePath?.(path);
    });
    window.addEventListener("keydown", (event) => this.shortcut(event));
    window.addEventListener("click", (event) => {
      if (!event.target?.closest?.(".folder-popover, [data-action='folder-open'], [data-action='route-picker'], [data-allowed-root-add]")) this.closeFolderPopover();
      if (!event.target?.closest?.(".session-menu, .session-menu-button")) this.closeSessionMenus();
    });
    window.addEventListener("message", (event) => {
      if (event.data?.type === "__activate_edit_mode") this.querySelector("[data-tweaks]")?.removeAttribute("hidden");
      if (event.data?.type === "__deactivate_edit_mode") {
        this.querySelector("[data-tweaks]")?.setAttribute("hidden", "");
      }
    });
    window.parent?.postMessage({ type: "__edit_mode_available" }, "*");
  }

  requestSessionResync(sessionId: string) {
    if (!sessionId || this.dataset.activeSessionId !== sessionId) return Promise.resolve();
    const generation = this.sessionViewGeneration;
    if (this.sessionResync?.sessionId === sessionId) return this.sessionResync.promise;
    const promise = Promise.resolve(this.loadSession(sessionId, { resync: true, viewGeneration: generation }))
      .finally(() => {
        if (this.sessionResync?.promise === promise) this.sessionResync = undefined;
      });
    this.sessionResync = { sessionId, generation, promise };
    return promise;
  }

  setSessionEventCursor(sessionId: string, eventID: unknown) {
    const cursor = normalizeEventCursor(eventID);
    if (!sessionId || !cursor || cursor === "0") return;
    this.sessionEventCursors ??= new Map();
    const current = this.sessionEventCursors.get(sessionId) || "0";
    if (compareEventCursors(cursor, current) > 0) this.sessionEventCursors.set(sessionId, cursor);
  }

  applyEvent(event) {
    if (!this.isCurrentSessionEvent(event)) return;
    if (shouldResyncAfterGap(event)) {
      const sessionId = this.dataset.activeSessionId;
      if (sessionId) void this.requestSessionResync(sessionId);
      return;
    }
    if (event.type === "heartbeat") return;
    this.setSessionEventCursor(event.sessionId, event.id);
    if (event.type === "session.renamed") {
      this.updateSessionTitle(event.payload);
      return;
    }
    if (event.type === "session.status") {
      const status = event.payload?.status;
      if (status) this.setMode(status);
      if (status === "cancelled") this.notifySessionCompleted?.();
      return;
    }
    if (event.type === "session.message") {
      this.appendMessage(event.payload);
      return;
    }
    if (event.type === "session.delta") {
      const kind = event.payload?.kind === "think" ? "think" : "pi";
      const message = { kind, text: event.payload?.delta || "" };
      this.appendMessage(message);
      return;
    }
    if (event.type === "tool.started") {
      this.appendMessage({ kind: "tool", ...event.payload, status: "running" });
      return;
    }
    if (event.type === "tool.args.delta") {
      this.updateToolArgs(event.payload);
      return;
    }
    if (event.type === "tool.output") {
      this.appendToolOutput(event.payload);
      return;
    }
    if (event.type === "tool.finished") {
      this.finishTool({ kind: "tool", ...event.payload, status: event.payload?.status || "ok" });
      return;
    }
    if (event.type === "error") {
      const detail = event.payload?.error || event.payload?.message;
      this.appendErrorMessage(detail);
      this.notifyResponseFailure?.(detail);
    }
  }

  confirmConnection() {
    if (this.connectionErrorTimer) clearTimeout(this.connectionErrorTimer);
    this.connectionErrorTimer = undefined;
    this.setConnection("ok");
  }

  setConnection(status) {
    const indicator = this.querySelector(".statusbtn");
    if (status === "ok") this.apiConnected = true;
    this.syncCompactionControl?.();
    if (!indicator) return;
    const styles = {
      ok: ["var(--accent)", "已连接"],
      reconnecting: ["var(--fg-3)", "正在重新连接事件流"],
      err: ["var(--danger)", "后端连接已断开"],
    };
    const [color, title] = styles[status] || styles.err;
    indicator.style.color = color;
    indicator.title = title;
  }

  isCurrentSessionEvent(event) {
    const eventSessionId = event?.sessionId;
    const activeSessionId = this.dataset.activeSessionId;
    return !eventSessionId || !activeSessionId || eventSessionId === activeSessionId;
  }

  setMode(mode) {
    const sessionId = this.dataset.activeSessionId;
    if (mode === "compacting") {
      this.compacting = true;
      this.running = false;
      this.syncCurrentSessionRunState?.(false);
      this.stopButton?.setAttribute("hidden", "");
      this.syncCompactionControl?.();
      this.syncLoadingMessage?.();
      return;
    }
    this.compacting = false;
    if (mode === "idle" && this.isSessionCancellationPending(sessionId)) mode = "cancelled";
    const wasRunning = this.running;
    const willRun = ["running", "thinking"].includes(mode);
    if (!wasRunning && willRun) {
      this.responseFailureToastShown = false;
      this.responseCompletionToastShown = false;
      this.responseChoiceRequested = false;
      this.responseErrorMessageShown = false;
    }
    if (mode === "idle") this.finishRunningTools?.();
    if (mode === "cancelled") this.finishRunningTools?.({ status: "err", resultMeta: "cancelled" });
    this.running = willRun;
    if (wasRunning && mode === "idle" && !this.responseFailureToastShown) this.notifyResponseCompletedOnce?.();
    if (!willRun && mode === "cancelled") this.clearSessionCancellationPending(sessionId);
    this.syncCurrentSessionRunState?.(this.running);
    this.syncCompactionControl?.();
    this.stopButton?.toggleAttribute("hidden", !this.running);
    if (this.sendButton) this.updatePrompt();
    this.syncLoadingMessage?.();
    if (!this.running) void this.loadRuntimeStatus?.();
  }

  markSessionCancellationPending(sessionId) {
    if (!sessionId) return;
    this.cancelledSessionIds ??= new Set();
    this.cancelledSessionIds.add(sessionId);
  }

  isSessionCancellationPending(sessionId) {
    return !!sessionId && !!this.cancelledSessionIds?.has(sessionId);
  }

  clearSessionCancellationPending(sessionId) {
    if (sessionId) this.cancelledSessionIds?.delete(sessionId);
  }

  updatePromptMeta(status: any = {}) {
    const meta = this.querySelector("[data-prompt-meta]");
    this.runtimeStatus = {
      ...this.runtimeStatus,
      ...status,
      currentBranch: status.currentBranch || status.branch || this.runtimeStatus?.currentBranch,
    };
    const model = this.modelLabel(this.runtimeStatus.model || "—", this.runtimeStatus.thinkingLevel);
    const currentBranch = this.runtimeStatus.currentBranch || "—";
    const parts = [model];
    const fiveHour = this.quotaLabel("5h", this.runtimeStatus.fiveHourQuota);
    const weekly = this.quotaLabel("Week", this.runtimeStatus.weeklyQuota);
    const contextUsage = this.contextUsageLabel(this.runtimeStatus.contextUsage);
    if (fiveHour) parts.push(fiveHour);
    if (weekly) parts.push(weekly);
    parts.push(this.branchLabel(currentBranch));
    if (contextUsage) parts.push(contextUsage);
    if (meta) meta.innerHTML = parts.join(" | ");
    this.syncThinkingPrefixes?.();
  }

  branchLabel(branch) {
    return `<span class="prompt-meta-item prompt-meta-branch">${this.promptMetaIcon("git-branch")}<span>${escapeHtml(branch)}</span></span>`;
  }

  modelLabel(model, thinkingLevel) {
    const safeModel = escapeHtml(model);
    return thinkingLevel ? `${safeModel} (${escapeHtml(thinkingLevel)})` : safeModel;
  }

  quotaLabel(label, quota) {
    if (!Number.isFinite(quota)) return undefined;
    const percent = Math.max(0, Math.min(100, Math.round(quota)));
    const level = percent >= 70 ? "full" : percent >= 30 ? "medium" : "low";
    return `<span class="prompt-meta-item prompt-meta-battery prompt-meta-battery-${level}">${escapeHtml(label)} ${this.promptMetaIcon(`battery-${level}`)}(${percent}%)</span>`;
  }

  contextUsageLabel(usage) {
    const contextWindow = Number(usage?.contextWindow);
    if (!Number.isFinite(contextWindow) || contextWindow <= 0) return undefined;
    const rawTokens = usage?.tokens == null ? null : Number(usage.tokens);
    const used = Number.isFinite(rawTokens) && rawTokens >= 0 ? this.formatContextTokens(rawTokens) : "—";
    const rawPercent = usage?.percent == null ? null : Number(usage.percent);
    let percent = null;
    if (Number.isFinite(rawPercent)) {
      percent = Math.max(0, Math.min(100, Math.round(rawPercent)));
    }
    const level = percent == null || percent < 70 ? "safe" : percent < 90 ? "warning" : "danger";
    const percentText = percent == null ? "" : ` (${percent}%)`;
    const text = `Ctx ${used} / ${this.formatContextTokens(contextWindow)}${percentText}`;
    return `<span class="prompt-meta-item prompt-meta-context-${level}">${escapeHtml(text)}</span>`;
  }

  formatContextTokens(value) {
    if (value >= 1_000_000) return `${Number((value / 1_000_000).toFixed(1))}M`;
    if (value >= 1_000) return `${Number((value / 1_000).toFixed(1))}K`;
    return String(Math.round(value));
  }

  promptMetaIcon(name) {
    const icons = {
      "battery-full": `<svg xmlns="http://www.w3.org/2000/svg" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M10 10v4"></path><path d="M14 10v4"></path><path d="M22 14v-4"></path><path d="M6 10v4"></path><rect x="2" y="6" width="16" height="12" rx="2"></rect></svg>`,
      "battery-medium": `<svg xmlns="http://www.w3.org/2000/svg" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M10 14v-4"></path><path d="M22 14v-4"></path><path d="M6 14v-4"></path><rect x="2" y="6" width="16" height="12" rx="2"></rect></svg>`,
      "battery-low": `<svg xmlns="http://www.w3.org/2000/svg" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M22 14v-4"></path><path d="M6 14v-4"></path><rect x="2" y="6" width="16" height="12" rx="2"></rect></svg>`,
      "git-branch": `<svg xmlns="http://www.w3.org/2000/svg" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M15 6a9 9 0 0 0-9 9V3"></path><circle cx="18" cy="6" r="3"></circle><circle cx="6" cy="18" r="3"></circle></svg>`,
    };
    return icons[name] || "";
  }
}

Object.assign(
  PiApp.prototype,
  workspaceMethods,
  sessionMethods,
  messageMethods,
  toolMessageMethods,
  transcriptWindowMethods,
  promptDraftMethods,
  inputMethods,
  attachmentMethods,
  filePreviewMethods,
  layoutMethods,
  toastMethods,
  runtimeStatusMethods,
  compactionMethods,
  settingsMethods,
  appConfigMethods,
  oauthMethods,
);

if (!customElements.get("pi-app")) customElements.define("pi-app", PiApp);

export { PiApp };
