import { getGitCommit, getGitHistory } from "../../lib/api";
import { escapeHtml } from "../../lib/renderers";

const LUCIDE_LIST_PLUS_ICON = `<svg xmlns="http://www.w3.org/2000/svg" width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M11 12H3"></path><path d="M16 6H3"></path><path d="M16 18H3"></path><path d="M18 9v6"></path><path d="M21 12h-6"></path></svg>`;
const LUCIDE_X_ICON = `<svg xmlns="http://www.w3.org/2000/svg" width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M18 6 6 18"></path><path d="m6 6 12 12"></path></svg>`;
const GIT_HISTORY_PAGE_SIZE = 30;
const MAX_GIT_HISTORY_COMMITS = 200;

export const gitHistoryMethods = {
  async showGitHistory() {
    if (!this.dataset.activeWorkspaceId) return;
    this.ensureGitPanel();
    this.setGitPanelMode("loading");
    try {
      this.gitHistoryLimit = GIT_HISTORY_PAGE_SIZE;
      const { commits } = await getGitHistory(this.dataset.activeWorkspaceId, this.gitHistoryLimit);
      this.gitHistoryCommits = commits || [];
      this.gitHistoryHasMore = this.gitHistoryCommits.length >= this.gitHistoryLimit
        && this.gitHistoryLimit < MAX_GIT_HISTORY_COMMITS;
      this.renderGitHistory(this.gitHistoryCommits);
    } catch (error) {
      this.renderGitHistoryError(error?.message || "Git 历史不可用");
    }
  },

  async refreshGitHistory() {
    if (!this.gitPanelOpen()) return;
    await this.showGitHistory();
  },

  gitPanelOpen() {
    return !this.querySelector("[data-git-panel]")?.hidden;
  },

  ensureGitPanel() {
    const panel = this.querySelector("[data-git-panel]");
    const fileTree = this.querySelector(".tree-list");
    if (!panel || !fileTree) return;
    panel.hidden = false;
    fileTree.hidden = true;
    this.querySelector("[data-action='show-git-history']")?.classList.add("on");
    this.querySelector("[data-action='show-file-tree']")?.classList.remove("on");
  },

  showFileTreePanel() {
    this.querySelector("[data-git-panel]")?.setAttribute("hidden", "");
    this.querySelector(".tree-list")?.removeAttribute("hidden");
    this.querySelector("[data-action='show-git-history']")?.classList.remove("on");
    this.querySelector("[data-action='show-file-tree']")?.classList.add("on");
  },

  setGitPanelMode(mode) {
    const panel = this.querySelector("[data-git-panel]");
    if (!panel) return;
    panel.dataset.mode = mode;
    if (mode === "loading") panel.innerHTML = `<div class="git-empty">正在加载 Git 历史…</div>`;
  },

  renderGitHistoryError(message) {
    const panel = this.querySelector("[data-git-panel]");
    if (!panel) return;
    panel.innerHTML = `<div class="git-empty err">${escapeHtml(message)}</div>`;
  },

  renderGitHistory(commits) {
    const panel = this.querySelector("[data-git-panel]");
    if (!panel) return;
    if (!commits.length) {
      panel.innerHTML = `<div class="git-empty">未找到提交记录</div>`;
      return;
    }
    panel.innerHTML = [
      `<div class="git-history-grid" data-git-history-grid>`,
      `<div class="git-history-toolbar">`,
      `<button type="button" data-action="load-more-git-history"`,
      ` ${this.gitHistoryHasMore ? "" : "disabled"}>${LUCIDE_LIST_PLUS_ICON}<span>再加载 30 条</span></button>`,
      `</div>`,
      `<div class="git-commit-scroll" data-git-commit-scroll>`,
      `<div class="git-commit-list" data-git-commit-list></div>`,
      `</div>`,
      `</div>`,
    ].join("");
    const list = panel.querySelector("[data-git-commit-list]");
    for (const commit of commits) list.append(this.createGitCommitRow(commit));
  },

  async loadMoreGitHistory() {
    if (!this.dataset.activeWorkspaceId || !this.gitHistoryHasMore) return;
    const nextLimit = Math.min((this.gitHistoryLimit || GIT_HISTORY_PAGE_SIZE) + GIT_HISTORY_PAGE_SIZE, MAX_GIT_HISTORY_COMMITS);
    const button = this.querySelector("[data-action='load-more-git-history']");
    if (button) button.disabled = true;
    try {
      const scroll = this.querySelector("[data-git-commit-scroll]");
      const scrollTop = scroll?.scrollTop || 0;
      const { commits } = await getGitHistory(this.dataset.activeWorkspaceId, nextLimit);
      this.gitHistoryLimit = nextLimit;
      this.gitHistoryCommits = commits || [];
      this.gitHistoryHasMore = this.gitHistoryCommits.length >= nextLimit && nextLimit < MAX_GIT_HISTORY_COMMITS;
      this.renderGitHistory(this.gitHistoryCommits);
      const nextScroll = this.querySelector("[data-git-commit-scroll]");
      if (nextScroll) nextScroll.scrollTop = scrollTop;
    } catch (error) {
      this.renderGitHistoryError(error?.message || "Git 历史不可用");
    }
  },


  createGitCommitRow(commit) {
    const row = document.createElement("button");
    row.type = "button";
    row.className = "git-commit-row";
    row.dataset.action = "select-git-commit";
    row.dataset.hash = commit.hash;
    row.innerHTML = [
      `<span class="git-commit-main">`,
      `<span class="git-subject"></span>`,
      `<span class="git-meta"><code>${escapeHtml(commit.shortHash)}</code> · ${escapeHtml(commit.authorName || "未知")} · ${formatGitDate(commit.date)}</span>`,
      `<span class="git-stats"><span class="add">+${commit.additions || 0}</span><span class="del">-${commit.deletions || 0}</span><span>${(commit.files || []).length} 个文件</span></span>`,
      `</span>`,
    ].join("");
    row.querySelector(".git-subject").textContent = commit.subject || commit.shortHash;
    return row;
  },

  async selectGitCommit(hash) {
    const workspaceId = this.dataset.activeWorkspaceId;
    if (!hash || !workspaceId || !this.confirmCleanFilePreview?.()) return;
    this.closeFilePreview?.(true);
    const inspectorRequestId = this.beginInspectorRequest?.();
    this.querySelectorAll(".git-commit-row").forEach((row) => row.classList.toggle("selected", row.dataset.hash === hash));
    const detail = this.ensureGitDetail();
    detail.innerHTML = gitDetailMessageTemplate("正在加载提交…");
    this.querySelector("[data-git-detail-panel] .git-detail-close")?.focus();
    try {
      const response = await getGitCommit(workspaceId, hash);
      if (!this.inspectorRequestIsCurrent?.(inspectorRequestId, workspaceId)) return;
      this.renderGitCommitDetail(response);
    } catch (error) {
      if (!this.inspectorRequestIsCurrent?.(inspectorRequestId, workspaceId)) return;
      detail.innerHTML = gitDetailMessageTemplate(error?.message || "提交不可用", true);
      this.querySelector("[data-git-detail-panel] .git-detail-close")?.focus();
    }
  },

  ensureGitDetail() {
    const panel = this.querySelector("[data-git-detail-panel]");
    let detail = panel?.querySelector("[data-git-detail]");
    if (!detail) {
      detail = document.createElement("div");
      detail.className = "git-detail";
      detail.dataset.gitDetail = "";
      const dialog = panel?.querySelector(".git-detail-dialog") || panel;
      dialog?.append(detail);
    }
    this.toggleGitDetailPanel?.(true);
    return detail;
  },

  closeGitDetail(invalidateRequest = true) {
    if (invalidateRequest) this.beginInspectorRequest?.();
    this.querySelector("[data-git-detail-panel] [data-git-detail]")?.remove();
    this.toggleGitDetailPanel?.(false);
    this.querySelectorAll(".git-commit-row").forEach((row) => row.classList.remove("selected"));
  },

  renderGitCommitDetail(detail) {
    const target = this.ensureGitDetail();
    const commit = detail.commit || {};
    const files = commit.files || [];
    target.innerHTML = [
      `<div class="git-detail-head">`,
      gitDetailCloseButtonTemplate(),
      `<strong>${escapeHtml(commit.subject || commit.shortHash || "提交")}</strong>`,
      `<span><code>${escapeHtml(commit.shortHash || "")}</code> · ${escapeHtml(commit.authorName || "未知")} · ${formatGitDate(commit.date)}</span>`,
      refsTemplate(commit.refs),
      `</div>`,
      `<div class="git-file-list">${files.map(fileTemplate).join("") || `<span class="git-empty-inline">暂无文件统计</span>`}</div>`,
      `<pre class="git-message">${escapeHtml(detail.body || commit.subject || "")}</pre>`,
      detail.truncated ? `<div class="git-truncated">为保证性能，diff 已截断</div>` : "",
      `<pre class="git-diff">${escapeHtml(detail.diff || "无 diff")}</pre>`
    ].join("");
    target.querySelector(".git-detail-close")?.focus();
  },
};

function gitDetailCloseButtonTemplate() {
  return `<button type="button" class="git-detail-close" data-action="close-git-detail" aria-label="关闭提交详情" title="关闭提交详情">${LUCIDE_X_ICON}</button>`;
}

function gitDetailMessageTemplate(message, isError = false) {
  const errorClass = isError ? " err" : "";
  return [
    `<div class="git-detail-head">${gitDetailCloseButtonTemplate()}<strong>提交详情</strong></div>`,
    `<div class="git-empty${errorClass}">${escapeHtml(message)}</div>`,
  ].join("");
}

function fileTemplate(file) {
  const oldPath = file.oldPath ? `<small>${escapeHtml(file.oldPath)} →</small>` : "";
  return `<div class="git-file"><span class="status ${escapeHtml(file.status || "modified")}">${escapeHtml(file.status || "modified")}</span><span class="path">${oldPath}${escapeHtml(file.path || "")}</span><span class="nums"><span class="add">+${file.additions || 0}</span><span class="del">-${file.deletions || 0}</span></span></div>`;
}

function refsTemplate(refs = []) {
  if (!refs.length) return "";
  return `<span class="git-refs">${refs.map((ref) => `<em>${escapeHtml(ref)}</em>`).join("")}</span>`;
}

function formatGitDate(value) {
  if (!value) return "—";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return escapeHtml(value);
  return date.toLocaleString(undefined, { month: "short", day: "2-digit", hour: "2-digit", minute: "2-digit" });
}
