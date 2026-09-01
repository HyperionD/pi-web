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
    const roots = this.appConfigState?.config?.workspace?.allowedRoots;
    if (!Array.isArray(roots) || !roots.length) return workspaces;
    return workspaces.filter((workspace) => roots.some((root) => pathIsWithinRoot(workspace.path, root)));
  },

  recoverForbiddenWorkspace(error) {
    if (!isForbiddenWorkspaceError(error)) return false;
    const message = requestErrorMessage(error);
    clearStoredActiveSession();
    this.closeSettingsModal?.();
    this.dataset.activeWorkspaceId = "";
    this.dataset.activeSessionId = "";
    this.sessionLoadToken = Symbol("forbidden-workspace");
    this.eventSource?.close?.();
    this.eventSource = null;
    this.backgroundSessionWatches?.forEach((watch) => watch.source?.close?.());
    this.backgroundSessionWatches?.clear?.();
    this.running = false;
    this.compacting = false;
    this.syncActiveWorkspaceRows?.();
    this.showSystemToast?.("error", "工作区不可访问", message, `workspace-forbidden:${message}`);
    this.route("picker");
    return true;
  },

  renderFilteredWorkspaces(workspaces = []) {
    const workspaceList = this.filterAllowedWorkspaces(workspaces || []);
    this.renderWorkspaces(workspaceList);
    return workspaceList;
  },

  async bootstrapAPI() {
    const appConfigRequest = Promise.resolve()
      .then(() => this.loadAppConfigState?.())
      .catch((error) => {
        this.reportAppConfigLoadFailure(error);
        return undefined;
      });
    try {
      const [{ workspaces }] = await Promise.all([getWorkspaces(), appConfigRequest]);
      this.apiConnected = true;
      this.setConnection("ok");

      const workspaceList = this.renderFilteredWorkspaces(workspaces || []);
      const storedSession = findStoredSession(workspaceList, readStoredActiveSession());
      const activeWorkspace = storedSession?.workspace || workspaceList[0];
      const activeSession = storedSession?.session || activeWorkspace?.sessions?.[0];

      if (activeWorkspace) this.activateBootstrapWorkspace(activeWorkspace);
      if (activeSession) this.activateBootstrapSession(activeSession);

      if (activeSession) this.route("workspace");

      if (activeWorkspace) {
        void this.loadWorkspaceCommands(activeWorkspace.id);
        void this.loadRuntimeStatus(activeWorkspace.id);
        void (this.loadWorkspaceSettingsState?.(activeWorkspace.id))?.catch?.(() => undefined);
        await this.loadWorkspaceMeta(activeWorkspace.id);
      }
      if (activeSession) await this.loadSession(activeSession.id);
    } catch (error) {
      if (this.recoverForbiddenWorkspace(error)) return;
      this.apiConnected = false;
      this.setConnection("err");
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
    this.workspaceMetaLoadToken = loadToken;
    const isCurrentLoad = () => (
      this.dataset.activeWorkspaceId === workspaceId && this.workspaceMetaLoadToken === loadToken
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

  async loadWorkspaceCommands(workspaceId) {
    const loadToken = Symbol(workspaceId);
    this.workspaceCommandsLoadToken = loadToken;
    const isCurrentLoad = () => (
      this.dataset.activeWorkspaceId === workspaceId && this.workspaceCommandsLoadToken === loadToken
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
    const button = this.querySelector("[data-action='refresh-workspaces']");
    if (button) button.disabled = true;
    try {
      const { workspaces } = await getWorkspaces();
      const workspaceList = this.renderFilteredWorkspaces(workspaces || []);
      const activeWorkspaceId = this.dataset.activeWorkspaceId;
      if (activeWorkspaceId) this.openActiveWorkspaceGroup(activeWorkspaceId);
    } catch (error) {
      if (!this.recoverForbiddenWorkspace(error)) this.setConnection("err");
    } finally {
      if (button) button.disabled = false;
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

  async loadSession(sessionId) {
    const loadToken = Symbol(sessionId);
    const workspaceId = this.findSessionRow(sessionId)?.dataset.workspace || this.dataset.activeWorkspaceId;
    this.sessionLoadToken = loadToken;
    const isCurrentResource = () => (
      (!this.dataset.activeSessionId || this.dataset.activeSessionId === sessionId)
      && (!workspaceId || !this.dataset.activeWorkspaceId || this.dataset.activeWorkspaceId === workspaceId)
    );
    this.showSessionSwitchLoading(sessionId);
    try {
      const loaded = await getSession(sessionId, { limit: SESSION_MESSAGE_PAGE_SIZE });
      if (this.sessionLoadToken !== loadToken || !isCurrentResource()) return;
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
    const row = this.simpleMessage("session loading", "pi >", "");
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
    this.connectEvents(session.id, { replay: false });
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

  updateActiveWorkspaceLabel(workspaceId) {
    const group = this.findWorkspaceGroup(workspaceId);
    const name = group?.querySelector(".label")?.textContent || workspaceId;
    const label = this.querySelector("[data-active-workspace]");
    const empty = this.querySelector("[data-empty-workspace]");
    if (label) label.textContent = name;
    if (empty) empty.textContent = name;
  },
};
