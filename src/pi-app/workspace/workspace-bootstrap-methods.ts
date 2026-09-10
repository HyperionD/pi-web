import {
  getGitStatus,
  getSession,
  getWorkspaceCommands,
  getWorkspaceFiles,
  getWorkspaces,
} from "../../lib/api";
import { clearStoredActiveSession, readStoredActiveSession, storeActiveSession } from "../sessions/session-storage";
import { isForbiddenWorkspaceError, requestErrorMessage } from "./workspace-methods";

const SESSION_MESSAGE_PAGE_SIZE = 120;

function normalizedPath(path) {
  const value = String(path || "").trim().replace(/\/+$/, "");
  return value || "/";
}

function pathIsWithinRoot(path, root) {
  const normalizedPathValue = normalizedPath(path);
  const normalizedRoot = normalizedPath(root);
  return normalizedRoot === "/"
    ? normalizedPathValue.startsWith("/")
    : normalizedPathValue === normalizedRoot || normalizedPathValue.startsWith(`${normalizedRoot}/`);
}

function findStoredSession(workspaces, stored) {
  if (!stored?.sessionId) return undefined;
  const preferred = workspaces.find((workspace) => workspace.id === stored.workspaceId);
  const candidates = preferred
    ? [preferred, ...workspaces.filter((workspace) => workspace !== preferred)]
    : workspaces;

  for (const workspace of candidates) {
    const session = workspace.sessions?.find((item) => item.id === stored.sessionId);
    if (session) return { workspace, session };
  }
  return undefined;
}

export const workspaceBootstrapMethods = {
  filterAllowedWorkspaces(workspaces) {
    if (document.body.dataset.accountRole) return workspaces;
    const roots = this.appConfigState?.config?.workspace?.allowedRoots;
    if (!Array.isArray(roots) || !roots.length) return workspaces;
    return workspaces.filter((workspace) => roots.some((root) => pathIsWithinRoot(workspace.path, root)));
  },

  recoverForbiddenWorkspace(error) {
    if (!isForbiddenWorkspaceError(error)) return false;
    const message = requestErrorMessage(error);
    this.bootstrapGeneration = Symbol("forbidden-workspace");
    clearStoredActiveSession();
    this.closeSettingsModal?.();
    this.clearActiveWorkspaceView();
    this.showSystemToast?.("error", "工作区不可访问", message, `workspace-forbidden:${message}`);
    this.resetFolderPopover?.();
    this.route("workspace");
    this.showEmptyMain?.();
    return true;
  },

  renderFilteredWorkspaces(workspaces = []) {
    const workspaceList = this.filterAllowedWorkspaces(workspaces || []);
    this.renderWorkspaces(workspaceList);
    return workspaceList;
  },

  async bootstrapAPI() {
    const bootstrapGeneration = Symbol("workspace-bootstrap");
    const bootstrapWorkspaceId = this.dataset.activeWorkspaceId || "";
    const bootstrapSessionId = this.dataset.activeSessionId || "";
    this.bootstrapGeneration = bootstrapGeneration;
    const isCurrentBootstrap = () => this.bootstrapGeneration === bootstrapGeneration;
    const ownsBootstrapState = () => (
      (this.dataset.activeWorkspaceId || "") === bootstrapWorkspaceId
      && (this.dataset.activeSessionId || "") === bootstrapSessionId
    );
    const matchesBootstrapState = (workspaceId = "", sessionId = "") => (
      isCurrentBootstrap()
      && (this.dataset.activeWorkspaceId || "") === workspaceId
      && (this.dataset.activeSessionId || "") === sessionId
    );
    const appConfigRequest = Promise.resolve()
      .then(() => this.loadAppConfigState?.(bootstrapGeneration))
      .catch((error) => {
        if (isCurrentBootstrap()) this.reportAppConfigLoadFailure(error);
        return undefined;
      });
    const adminPasswordRequest = Promise.resolve()
      .then(() => this.loadAdminPasswordState?.())
      .catch((error) => {
        if (isCurrentBootstrap()) this.reportAppConfigLoadFailure(error);
        return undefined;
      });
    try {
      const [{ workspaces }] = await Promise.all([getWorkspaces(), appConfigRequest, adminPasswordRequest]);
      if (!isCurrentBootstrap() || !ownsBootstrapState()) return;
      this.apiConnected = true;
      this.setConnection("ok");

      const workspaceList = this.renderFilteredWorkspaces(workspaces || []);
      const stored = readStoredActiveSession();
      const storedSession = findStoredSession(workspaceList, stored);
      if (stored?.sessionId && !storedSession) clearStoredActiveSession();
      const activeWorkspace = storedSession?.workspace || workspaceList[0];
      const activeSession = storedSession?.session || activeWorkspace?.sessions?.[0];

      if (!isCurrentBootstrap()) return;
      if (!activeWorkspace) {
        this.clearActiveWorkspaceView();
        this.route("workspace");
        this.showEmptyMain?.();
        return;
      }
      this.activateBootstrapWorkspace(activeWorkspace);
      if (activeSession) this.activateBootstrapSession(activeSession);
      else {
        clearStoredActiveSession();
        this.dataset.activeSessionId = "";
        this.resetActiveSessionDisplay();
      }
      if (!matchesBootstrapState(activeWorkspace.id, activeSession?.id || "")) return;

      if (activeSession) {
        this.route("workspace");
        this.showSessionMain?.();
      } else {
        this.route("workspace");
        this.showEmptyMain?.();
      }

      if (activeWorkspace) {
        if (!matchesBootstrapState(activeWorkspace.id, activeSession?.id || "")) return;
        void this.loadWorkspaceCommands(activeWorkspace.id);
        void this.loadRuntimeStatus(activeWorkspace.id, { bootstrapGeneration });
        if (document.body.dataset.accountRole !== "user") {
          void (this.loadWorkspaceSettingsState?.(activeWorkspace.id))?.catch?.(() => undefined);
        }
        if (!matchesBootstrapState(activeWorkspace.id, activeSession?.id || "")) return;
        await this.loadWorkspaceMeta(activeWorkspace.id, {
          bootstrapGeneration,
          bootstrapSessionId: activeSession?.id || "",
        });
        if (!matchesBootstrapState(activeWorkspace.id, activeSession?.id || "")) return;
      }
      if (activeSession) {
        if (!matchesBootstrapState(activeWorkspace?.id || "", activeSession.id)) return;
        await this.loadSession(activeSession.id, { bootstrapGeneration });
        if (!matchesBootstrapState(activeWorkspace?.id || "", activeSession.id)) return;
      }
    } catch (error) {
      if (!isCurrentBootstrap()) return;
      if (this.recoverForbiddenWorkspace(error)) return;
      this.apiConnected = false;
      this.setConnection("err");
    }
  },

  clearActiveWorkspaceView() {
    clearStoredActiveSession();
    this.dataset.activeWorkspaceId = "";
    this.dataset.activeSessionId = "";
    this.querySelectorAll(".session-row.active").forEach((row) => {
      this.markSessionRunning?.(row, false);
      this.updateSessionMeta?.(row, false);
    });
    this.markSelectedSessionRow?.("");
    this.invalidateEventStream();
    this.backgroundSessionWatches?.forEach((watch) => watch.source?.close?.());
    this.backgroundSessionWatches?.clear?.();
    this.sessionLoadToken = Symbol("cleared-workspace");
    this.runtimeStatusRequestToken = Symbol("cleared-workspace");
    this.workspaceMetaLoadToken = Symbol("cleared-workspace");
    this.workspaceCommandsLoadToken = Symbol("cleared-workspace");
    this.workspaceFiles = [];
    this.workspaceFileStatuses = {};
    window.dispatchEvent(new CustomEvent("pi-workspace-tree:update", {
      detail: { files: [], statusMap: {}, selectedPath: "" },
    }));
    this.resetActiveSessionState?.();
    this.renderMessages?.([]);
    this.resetActiveSessionDisplay();
    this.syncActiveWorkspaceRows?.();
    this.showEmptyMain?.();
  },

  resetActiveSessionDisplay() {
    const workspace = this.querySelector("[data-active-workspace]");
    if (workspace) workspace.textContent = "工作区";
    const emptyWorkspace = this.querySelector("[data-empty-workspace]");
    if (emptyWorkspace) emptyWorkspace.textContent = "工作区";
    this.syncEmptyWorkspaceState();
    const session = this.querySelector("[data-active-session-title]");
    if (session) {
      session.textContent = "无会话";
      session.title = "无会话";
    }
  },

  reportAppConfigLoadFailure(error) {
    const message = error instanceof Error ? error.message : String(error);
    if (this.showSystemToast) {
      this.showSystemToast("error", "应用配置加载失败", message, `app-config-load:${message}`);
      return;
    }
    this.setConnection?.("err");
  },

  activateBootstrapWorkspace(workspace) {
    this.dataset.activeWorkspaceId = workspace.id;
    this.syncEmptyWorkspaceState();
    const label = this.querySelector("[data-active-workspace]");
    if (label) label.textContent = workspace.name;
    this.syncActiveWorkspaceRows?.();
  },

  activateBootstrapSession(session) {
    this.dataset.activeSessionId = session.id;
    const title = this.querySelector("[data-active-session-title]");
    if (title) {
      title.textContent = session.title;
      title.title = `${session.title} · ${session.id}`;
    }
  },

  async loadWorkspaceMeta(workspaceId, options: any = {}) {
    const loadToken = Symbol(workspaceId);
    const bootstrapGeneration = options.bootstrapGeneration || this.bootstrapGeneration;
    const bootstrapSessionId = options.bootstrapSessionId;
    this.workspaceMetaLoadToken = loadToken;
    const isCurrentLoad = () => (
      this.dataset.activeWorkspaceId === workspaceId
      && this.workspaceMetaLoadToken === loadToken
      && (!bootstrapGeneration || this.bootstrapGeneration === bootstrapGeneration)
      && (!bootstrapSessionId || (this.dataset.activeSessionId || "") === bootstrapSessionId)
    );
    try {
      const [{ files }, git] = await Promise.all([getWorkspaceFiles(workspaceId), getGitStatus(workspaceId)]);
      if (!isCurrentLoad()) return;
      this.renderWorkspaceTree(files, git?.files || {}, options.selectedPath);
      this.renderGitStatus(git);
    } catch (error) {
      if (isCurrentLoad()) this.recoverForbiddenWorkspace(error);
    }
  },

  renderWorkspaceTree(files, statusMap = {}, selectedPathOverride = undefined) {
    if (!files) return;
    this.workspaceFiles = files;
    this.workspaceFileStatuses = statusMap;
    const selectedPath = selectedPathOverride ?? (this.filePreview?.file?.path || "");
    window.dispatchEvent(new CustomEvent("pi-workspace-tree:update", {
      detail: { files, statusMap, selectedPath },
    }));
  },

  renderGitStatus(git) {
    const status = this.querySelector("[data-git-status]");
    if (status && git) status.textContent = `${git.branch} · ${git.dirty} ✱`;
    if (git?.branch) this.updatePromptMeta({ currentBranch: git.branch });
  },

  async loadWorkspaceCommands(workspaceId, bootstrapGeneration) {
    const loadToken = Symbol(workspaceId);
    const expectedBootstrapGeneration = bootstrapGeneration || this.bootstrapGeneration;
    this.workspaceCommandsLoadToken = loadToken;
    const isCurrentLoad = () => (
      this.dataset.activeWorkspaceId === workspaceId
      && this.workspaceCommandsLoadToken === loadToken
      && (!expectedBootstrapGeneration || this.bootstrapGeneration === expectedBootstrapGeneration)
    );
    try {
      const { commands } = await getWorkspaceCommands(workspaceId);
      if (!isCurrentLoad()) return;
      this.renderSlashCommands(commands || []);
    } catch (error) {
      if (isCurrentLoad()) this.recoverForbiddenWorkspace(error);
    }
  },

  async refreshWorkspaces() {
    if (!this.apiConnected) return;
    const refreshToken = Symbol("workspace-refresh");
    const refreshGeneration = Symbol("workspace-refresh");
    const openGeneration = this.workspaceOpenGeneration;
    const expectedWorkspaceId = this.dataset.activeWorkspaceId || "";
    const expectedSessionId = this.dataset.activeSessionId || "";
    this.workspaceRefreshToken = refreshToken;
    this.bootstrapGeneration = refreshGeneration;
    const isCurrentRefresh = () => (
      this.workspaceRefreshToken === refreshToken
      && this.bootstrapGeneration === refreshGeneration
      && this.workspaceOpenGeneration === openGeneration
      && (this.dataset.activeWorkspaceId || "") === expectedWorkspaceId
      && (this.dataset.activeSessionId || "") === expectedSessionId
    );
    const button = this.querySelector("[data-action='refresh-workspaces']");
    if (button) button.disabled = true;
    try {
      const { workspaces } = await getWorkspaces();
      if (!isCurrentRefresh()) return;
      const workspaceList = this.renderFilteredWorkspaces(workspaces || []);
      if (!isCurrentRefresh()) return;
      const activeWorkspaceId = this.dataset.activeWorkspaceId;
      const activeSessionId = this.dataset.activeSessionId;
      const activeWorkspace = workspaceList.find((workspace) => workspace.id === activeWorkspaceId);
      const activeSession = activeWorkspace?.sessions?.find((session) => session.id === activeSessionId);
      const missingActiveState = !workspaceList.length
        || (activeWorkspaceId && !activeWorkspace)
        || (activeSessionId && !activeSession);
      if (missingActiveState) this.clearActiveWorkspaceView();
      else if (activeWorkspaceId) this.openActiveWorkspaceGroup(activeWorkspaceId);
    } catch (error) {
      if (isCurrentRefresh() && !this.recoverForbiddenWorkspace(error)) this.setConnection("err");
    } finally {
      if (this.workspaceRefreshToken === refreshToken && button) button.disabled = false;
    }
  },

  async refreshTree() {
    const workspaceId = this.dataset.activeWorkspaceId;
    if (!workspaceId || !this.apiConnected) return;
    const button = this.querySelector("[data-action='refresh-tree']");
    if (button) button.disabled = true;
    try {
      await this.loadWorkspaceMeta(workspaceId);
    } finally {
      if (button) button.disabled = false;
    }
  },

  async loadSession(sessionId, options: any = {}) {
    const loadToken = Symbol(sessionId);
    const { bootstrapGeneration } = options;
    const workspaceId = this.findSessionRow(sessionId)?.dataset.workspace || this.dataset.activeWorkspaceId;
    this.sessionLoadToken = loadToken;
    const isCurrentResource = () => (
      this.sessionLoadToken === loadToken
      && (!bootstrapGeneration || this.bootstrapGeneration === bootstrapGeneration)
      && (bootstrapGeneration
        ? this.dataset.activeSessionId === sessionId && this.dataset.activeWorkspaceId === workspaceId
        : (!this.dataset.activeSessionId || this.dataset.activeSessionId === sessionId)
          && (!workspaceId || !this.dataset.activeWorkspaceId || this.dataset.activeWorkspaceId === workspaceId))
    );
    this.invalidateEventStream?.();
    this.showSessionSwitchLoading(sessionId);
    try {
      const loaded = await getSession(sessionId, { limit: SESSION_MESSAGE_PAGE_SIZE });
      if (!isCurrentResource()) return;
      this.applyLoadedSession(loaded.session, loaded.messages || [], loaded.status, loaded);
    } catch (error) {
      if (this.sessionLoadToken !== loadToken) return;
      if (isForbiddenWorkspaceError(error) && !isCurrentResource()) return;
      if (!this.recoverForbiddenWorkspace(error)) this.setConnection("err");
    }
  },

  showSessionSwitchLoading(sessionId) {
    if (!this.termInner) return;
    this.resetActiveSessionState?.();
    this.termInner.replaceChildren();
    this.resetTranscriptWindow?.();
    const row = this.simpleMessage("session loading", "agent >", "");
    row.classList.add("session-switch-loading");
    row.dataset.kind = "loading";
    const label = document.createElement("span");
    label.className = "session-switch-label";
    label.textContent = `正在加载 ${this.findSessionRow(sessionId)?.dataset.title || "会话"}…`;
    const skeleton = document.createElement("span");
    skeleton.className = "session-switch-skeleton";
    skeleton.setAttribute("aria-hidden", "true");
    row.querySelector(".body")?.replaceChildren(label, skeleton);
    this.appendTranscriptNode?.(row, { stickToBottom: false });
  },

  applyLoadedSession(session, messages, status = "idle", page: any = {}) {
    this.dataset.activeSessionId = session.id;
    this.resetActiveSessionState?.();
    const workspaceId = session.workspaceId
      || this.findSessionRow(session.id)?.dataset.workspace;
    if (workspaceId) this.activateWorkspaceForSession(workspaceId, { loadContext: true });
    this.markSelectedSessionRow?.(session.id);
    storeActiveSession(workspaceId || this.dataset.activeWorkspaceId, session.id);
    this.activateBootstrapSession(session);
    this.sessionHistoryCursor = page.cursor || "";
    this.sessionHistoryHasMore = !!page.hasMore;
    this.sessionHistoryLoading = false;
    this.renderMessages(messages);
    this.setMode(status || "idle");
    this.connectEvents(session.id, { replay: status === "running" ? "active" : false });
  },

  async loadOlderSessionMessages() {
    const sessionId = this.dataset.activeSessionId;
    if (!sessionId || !this.sessionHistoryHasMore || this.sessionHistoryLoading) return;
    const loadToken = Symbol(sessionId);
    const workspaceId = this.findSessionRow(sessionId)?.dataset.workspace || this.dataset.activeWorkspaceId;
    this.sessionHistoryLoadToken = loadToken;
    this.sessionHistoryLoading = true;
    const cursor = this.sessionHistoryCursor;
    const isCurrentLoad = () => (
      this.dataset.activeSessionId === sessionId
      && (!workspaceId || !this.dataset.activeWorkspaceId || this.dataset.activeWorkspaceId === workspaceId)
      && this.sessionHistoryCursor === cursor
      && this.sessionHistoryLoadToken === loadToken
    );
    try {
      const loaded = await getSession(sessionId, { limit: SESSION_MESSAGE_PAGE_SIZE, before: cursor });
      if (!isCurrentLoad()) return;
      this.prependLoadedMessages(loaded.messages || []);
      this.sessionHistoryCursor = loaded.cursor || "";
      this.sessionHistoryHasMore = !!loaded.hasMore;
    } catch (error) {
      if (isCurrentLoad() && !this.recoverForbiddenWorkspace(error)) this.setConnection("err");
    } finally {
      if (this.dataset.activeSessionId === sessionId && this.sessionHistoryLoadToken === loadToken) {
        this.sessionHistoryLoading = false;
      }
    }
  },

  prependLoadedMessages(messages) {
    if (!messages.length) return;
    this.answeredChoiceIds = new Set([
      ...(this.answeredChoiceIds || []),
      ...this.answeredChoiceIdsFrom(messages),
    ]);
    this.transcriptItems = [
      ...messages.map((message) => this.createTranscriptItem(message)),
      ...(this.transcriptItems || []),
    ];
    this.renderTranscriptWindow({ stickToBottom: false, preservePrepend: true });
  },

  findSessionRow(sessionId) {
    return [...this.querySelectorAll("[data-session]")].find((row) => row.dataset.session === sessionId);
  },

  findWorkspaceGroup(workspaceId) {
    return [...this.querySelectorAll("[data-workspace-group]")]
      .find((group) => group.dataset.workspaceGroup === workspaceId);
  },

  activateWorkspaceForSession(workspaceId, { loadContext = false, forceLoadContext = false } = {}) {
    const changed = this.dataset.activeWorkspaceId !== workspaceId;
    if (changed) {
      this.beginInspectorRequest?.();
      this.closeGitDetail?.(false);
      if (!this.hasDirtyFilePreview?.()) this.closeFilePreview?.(true);
    }
    this.dataset.activeWorkspaceId = workspaceId;
    this.syncEmptyWorkspaceState();
    this.openActiveWorkspaceGroup(workspaceId);
    this.updateActiveWorkspaceLabel(workspaceId);
    this.syncActiveWorkspaceRows?.();
    const shouldLoadContext = loadContext && (changed || forceLoadContext);
    if (this.apiConnected && shouldLoadContext) this.loadWorkspaceContext(workspaceId);
  },

  loadWorkspaceContext(workspaceId) {
    void this.loadWorkspaceCommands(workspaceId);
    void this.loadRuntimeStatus(workspaceId);
    void (this.loadWorkspaceSettingsState?.(workspaceId))?.catch?.(() => undefined);
    void this.loadWorkspaceMeta(workspaceId);
  },

  openActiveWorkspaceGroup(workspaceId) {
    this.querySelectorAll("[data-workspace-group]").forEach((group) => {
      const open = group.dataset.workspaceGroup === workspaceId;
      const sessions = group.querySelector(".sessions");
      const row = group.querySelector(".ws-row");
      if (sessions) sessions.hidden = !open;
      row?.classList.toggle("open", open);
      row?.setAttribute("aria-expanded", String(open));
    });
  },

  syncEmptyWorkspaceState() {
    const hasWorkspace = Boolean(this.dataset.activeWorkspaceId);
    this.querySelector("[data-empty-no-workspace]")?.toggleAttribute("hidden", hasWorkspace);
    this.updatePrompt?.();
  },

  updateActiveWorkspaceLabel(workspaceId) {
    const group = this.findWorkspaceGroup(workspaceId);
    const name = group?.querySelector(".label")?.textContent || workspaceId;
    const label = this.querySelector("[data-active-workspace]");
    const empty = this.querySelector("[data-empty-workspace]");
    if (label) label.textContent = name;
    if (empty) empty.textContent = name;
  },
};
