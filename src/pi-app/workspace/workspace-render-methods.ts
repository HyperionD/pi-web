import { currentUiLocale } from "../../i18n/client";
import { uiMessage } from "../../i18n/ui";
import { escapeHtml } from "../../lib/renderers";
import { appendGroupedSessionRows } from "../sessions/session-hierarchy";

const LUCIDE_PLUS_ICON = `<svg xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M5 12h14"></path><path d="M12 5v14"></path></svg>`;
const LUCIDE_X_ICON = `<svg xmlns="http://www.w3.org/2000/svg" width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M18 6 6 18"></path><path d="m6 6 12 12"></path></svg>`;
const LUCIDE_CORNER_DOWN_LEFT_ICON = `<svg xmlns="http://www.w3.org/2000/svg" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M20 4v7a4 4 0 0 1-4 4H4"></path><path d="m9 10-5 5 5 5"></path></svg>`;
const LUCIDE_CHEVRON_RIGHT_ICON = `<svg xmlns="http://www.w3.org/2000/svg" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="m9 18 6-6-6-6"></path></svg>`;

export const workspaceRenderMethods = {
  renderWorkspaces(workspaces) {
    const count = this.querySelector("[data-workspace-count]");
    if (count) count.textContent = `${workspaces.length} 个工作区`;
    this.renderRecentWorkspaces(workspaces);
    this.renderSidebarWorkspaces(workspaces);
    this.renderWorkspaceOptions(workspaces);
    this.syncActiveWorkspaceRows();
    this.syncUnreadCompletedSessions?.();
    this.syncBackgroundSessionWatches?.();
  },

  renderRecentWorkspaces(workspaces) {
    const recentCard = this.querySelector("[data-recent-workspaces]");
    if (!recentCard) return;
    recentCard.querySelectorAll(".recent-row").forEach((row) => row.remove());
    for (const workspace of workspaces.slice(0, 4)) recentCard.append(this.createRecentWorkspace(workspace));
  },

  renderSidebarWorkspaces(workspaces) {
    const section = this.querySelector(".sidebar .sb-section");
    if (!section) return;
    section.querySelectorAll(".workspace-group").forEach((group) => group.remove());
    let anchor = section.querySelector(".sb-head");
    for (const workspace of workspaces) {
      const group = this.createWorkspaceGroup(workspace);
      anchor.insertAdjacentElement("afterend", group);
      anchor = group;
    }
  },

  renderWorkspaceOptions(workspaces) {
    const options = this.querySelector("[data-workspace-options]");
    if (!options) return;
    options.replaceChildren();
    for (const workspace of workspaces) {
      const row = document.createElement("div");
      row.className = "workspace-option-row";
      const button = document.createElement("button");
      button.type = "button";
      button.className = "workspace-option";
      button.dataset.action = "switch-workspace";
      button.dataset.workspace = workspace.id;
      button.innerHTML = `<span class="workspace-info"><strong></strong><small></small></span>
        <span class="workspace-check" aria-hidden="true">✓</span>`;
      button.querySelector("strong").textContent = workspace.name;
      button.querySelector("small").textContent = workspace.path;
      row.append(button);
      if (document.body.dataset.accountRole !== "user") row.append(this.createWorkspaceRemoveButton(workspace));
      options.append(row);
    }
    if (!workspaces.length) {
      const empty = document.createElement("div");
      empty.className = "workspace-menu-empty";
      empty.textContent = "暂无工作区";
      options.append(empty);
    }
  },

  createWorkspaceRemoveButton(workspace) {
    const button = document.createElement("button");
    button.type = "button";
    button.className = "workspace-remove";
    button.dataset.action = "close-workspace";
    button.dataset.workspace = workspace.id;
    button.title = "从列表移除工作区";
    button.setAttribute("aria-label", `从列表移除 ${workspace.name}`);
    button.innerHTML = LUCIDE_X_ICON;
    return button;
  },

  closeWorkspaceSwitcher(restoreFocus = false) {
    const switcher = this.querySelector("[data-workspace-switcher]");
    if (!switcher?.open) return;
    switcher.open = false;
    if (restoreFocus) switcher.querySelector("summary")?.focus();
  },

  async switchWorkspace(workspaceId) {
    const group = this.findWorkspaceGroup(workspaceId);
    if (!group) return;
    this.closeWorkspaceSwitcher(true);
    if (workspaceId === this.dataset.activeWorkspaceId) return;
    const session = group.querySelector(".session-row.selected[data-session]")
      || group.querySelector(".session-row.active[data-session]")
      || group.querySelector(".session-row[data-session]");
    try {
      if (session) await this.pickSession(session);
      else {
        this.activateWorkspaceForSession(workspaceId);
        this.clearActiveSession(this.dataset.activeSessionId);
        this.markSelectedSessionRow("");
        this.activateWorkspaceForSession(workspaceId, { loadContext: true, forceLoadContext: true });
      }
      this.route("workspace");
    } catch (error) {
      this.showSystemToast?.("error", "切换工作区失败", String(error));
    }
  },

  createRecentWorkspace(workspace) {
    const row = document.createElement("button");
    row.type = "button";
    row.className = "recent-row";
    row.dataset.workspace = workspace.id;
    row.setAttribute("aria-label", `打开 ${workspace.name}`);
    row.innerHTML = [
      `<span class="glyph">${LUCIDE_CHEVRON_RIGHT_ICON}</span>`,
      "<span class=\"ws-info\"><span class=\"name\"></span><span class=\"path\"></span></span>",
      "<span class=\"ws-stat\"></span>",
      `<span class="open-cta">打开 ${LUCIDE_CORNER_DOWN_LEFT_ICON}</span>`,
    ].join("");
    row.querySelector(".name").textContent = workspace.name;
    row.querySelector(".path").textContent = workspace.path;
    row.querySelector(".ws-stat").innerHTML = this.workspaceRecentStatus(workspace);
    return row;
  },

  workspaceRecentStatus(workspace) {
    if (workspace.live) {
      return `<span class="live">● 在线</span><span class="lbl">${workspace.sessionCount} 个会话</span>`;
    }
    const lastUsed = escapeHtml(workspace.lastUsed || "—");
    return `<span>${lastUsed}</span><span class="lbl">${workspace.sessionCount} 个会话</span>`;
  },

  createWorkspaceGroup(workspace) {
    const group = document.createElement("div");
    group.className = "workspace-group";
    group.dataset.workspaceGroup = workspace.id;
    group.dataset.workspaceLive = workspace.live ? "true" : "false";
    const open = workspace.id === this.dataset.activeWorkspaceId;
    group.classList.toggle("active", open);
    group.classList.toggle("has-active-session", this.workspaceHasActiveSession(workspace));
    group.innerHTML = this.workspaceGroupTemplate(workspace, open);
    this.fillWorkspaceGroup(group, workspace);
    return group;
  },

  workspaceHasActiveSession(workspace) {
    return (workspace.sessions || []).some((session) => session.live);
  },

  workspaceGroupTemplate(workspace, open) {
    const active = workspace.id === this.dataset.activeWorkspaceId;
    const hasActiveSession = this.workspaceHasActiveSession(workspace);
    const runningLabel = uiMessage(currentUiLocale(), "running");
    const sessionCountLabel = uiMessage(currentUiLocale(), "sessionCountLabel");
    const runningHidden = hasActiveSession || workspace.live ? "" : " hidden";
    const stateClasses = [open && "open", active && "active", hasActiveSession && "has-active-session"]
      .filter(Boolean)
      .join(" ");
    return [
      // Keep workspace metadata for existing session operations, outside the visible session list.
      "<div class=\"workspace-shell\" hidden>",
      `<button type="button" class="ws-row ${stateClasses}"`,
      ` data-action="toggle-workspace" data-workspace="${escapeHtml(workspace.id)}"`,
      ` aria-expanded="${open}" aria-current="${active ? "true" : "false"}">`,
      "<span class=\"ws-stack\"><span class=\"ws-name\"><span class=\"label\"></span></span>",
      "<span class=\"ws-path\"></span></span>",
      "<span class=\"ws-meta\">",
      `<span class="ws-count">${workspace.sessionCount}</span>`,
      `<span class=\"ws-count-label\" data-i18n=\"sessionCountLabel\">${sessionCountLabel}</span>`,
      `<span class="ws-status" data-i18n="running"${runningHidden}>${runningLabel}</span>`,
      "</span></button>",
      document.body.dataset.accountRole === "user" ? "" : [
        `<button type="button" class="row-action danger" data-action="close-workspace"`,
        ` data-workspace="${escapeHtml(workspace.id)}" title="关闭工作区" aria-label="关闭工作区">${LUCIDE_X_ICON}</button>`,
      ].join(""),
      `</div><div class="sessions"${open ? "" : " hidden"}></div>`,
    ].join("");
  },

  fillWorkspaceGroup(group, workspace) {
    group.querySelector(".label").textContent = workspace.name;
    group.querySelector(".ws-path").textContent = workspace.path;
    const status = group.querySelector(".ws-status");
    if (status) {
      const running = !!workspace.live || this.workspaceHasActiveSession(workspace);
      status.hidden = !running;
      status.textContent = running ? uiMessage(currentUiLocale(), "running") : "";
    }
    const sessions = group.querySelector(".sessions");
    appendGroupedSessionRows(sessions, workspace.id, workspace.sessions, this.createSessionRow.bind(this));
    const empty = document.createElement("div");
    empty.className = "sessions-empty";
    empty.dataset.sessionsEmpty = "";
    empty.textContent = "暂无会话，开始一段新对话";
    empty.hidden = Boolean(workspace.sessions?.length);
    sessions.append(empty, this.createNewSessionRow(workspace.id));
  },

  createNewSessionRow(workspaceId) {
    const row = document.createElement("button");
    row.type = "button";
    row.className = "session-row new-session-row";
    row.dataset.action = "new-session";
    row.dataset.workspace = workspaceId;
    row.setAttribute("aria-label", "新建会话");
    row.title = "新建会话";
    row.innerHTML = LUCIDE_PLUS_ICON;
    const label = document.createElement("span");
    label.textContent = "新建会话";
    row.append(label);
    return row;
  },

  toggleWorkspace(id) {
    const targetSessions = this.findWorkspaceGroup?.(id)?.querySelector(".sessions");
    const shouldOpen = !!targetSessions?.hidden;
    this.activateWorkspaceForSession(id);
    this.querySelectorAll("[data-workspace-group]").forEach((group) => {
      const sessions = group.querySelector(".sessions");
      const row = group.querySelector(".ws-row");
      const open = group.dataset.workspaceGroup === id ? shouldOpen : false;
      if (sessions) sessions.hidden = !open;
      group.classList.toggle("open", open);
      row?.classList.toggle("open", open);
      row?.setAttribute("aria-expanded", String(open));
    });
    this.syncActiveWorkspaceRows();
  },

  syncActiveWorkspaceRows() {
    const activeWorkspaceId = this.dataset.activeWorkspaceId;
    this.querySelectorAll("[data-workspace-group]").forEach((group) => {
      const row = group.querySelector(".ws-row");
      const active = group.dataset.workspaceGroup === activeWorkspaceId;
      const hasLiveSession = group.querySelector(".session-row.active");
      const hasActiveSession = !!hasLiveSession || group.dataset.workspaceLive === "true";
      group.hidden = !active;
      group.querySelector(".sessions")?.toggleAttribute("hidden", !active);
      const empty = group.querySelector("[data-sessions-empty]");
      if (empty) empty.hidden = Boolean(group.querySelector(".session-row[data-session]"));
      group.classList.toggle("active", active);
      group.classList.toggle("has-active-session", hasActiveSession);
      row?.classList.toggle("active", active);
      row?.classList.toggle("has-active-session", hasActiveSession);
      row?.setAttribute("aria-current", active ? "true" : "false");
      const status = row?.querySelector(".ws-status");
      if (status) {
        status.hidden = !hasActiveSession;
        status.textContent = hasActiveSession ? uiMessage(currentUiLocale(), "running") : "";
      }
    });
    this.querySelectorAll("[data-action='switch-workspace']").forEach((option) => {
      option.setAttribute("aria-current", String(option.dataset.workspace === activeWorkspaceId));
    });
    this.updateActiveWorkspaceLabel(activeWorkspaceId);
  },
};
