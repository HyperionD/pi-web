import {
  getWorkspaceSkills,
  setWorkspaceSkillEnabled,
  type WorkspaceSkill,
} from "../../lib/api";

const SKILL_SCOPE_ORDER = ["global", "project", "path"] as const;
const SKILL_SCOPE_LABELS = { global: "全局", project: "项目", path: "自定义路径" };

type SkillManagerState = {
  skills: WorkspaceSkill[];
  selectedPath: string;
  available: boolean;
  loading: boolean;
  savingPath: string;
  error: string;
  notice: string;
};

function initialSkillManagerState(): SkillManagerState {
  return {
    skills: [], selectedPath: "", available: true, loading: false,
    savingPath: "", error: "", notice: "",
  };
}

function skillManagerState(app): SkillManagerState {
  app.skillManagerState ??= initialSkillManagerState();
  return app.skillManagerState;
}

function skillManagerElement(app, selector: string): HTMLElement | null {
  return app.querySelector(`[data-skill-manager] ${selector}`);
}

function setSkillText(app, selector: string, value: string): void {
  const element = skillManagerElement(app, selector);
  if (element) element.textContent = value;
}

function skillError(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

function selectedSkill(app): WorkspaceSkill | undefined {
  const state = skillManagerState(app);
  return state.skills.find((skill) => skill.path === state.selectedPath);
}

function canEditSkill(skill: WorkspaceSkill): boolean {
  return skill.scope === "project" || document.body.dataset.accountRole !== "user";
}

function skillScopeLabel(scope: WorkspaceSkill["scope"]): string {
  return SKILL_SCOPE_LABELS[scope] || scope;
}

export const skillMethods = {
  syncSkillManagerButton() {
    const button = this.querySelector("[data-action='open-skill-manager']") as HTMLButtonElement | null;
    if (button) button.disabled = !this.apiConnected || !this.dataset.activeWorkspaceId;
  },

  bindSkillManagerControls() {
    if (this.skillManagerControlsBound) return;
    const root = this.querySelector("[data-skill-manager]");
    if (!root) return;
    root.querySelector("[data-skill-close]")?.addEventListener("click", () => this.closeSkillManager());
    root.querySelector("[data-skill-overlay]")?.addEventListener("click", () => this.closeSkillManager());
    root.querySelector("[data-skill-refresh]")?.addEventListener("click", () => void this.loadWorkspaceSkills());
    root.querySelector("[data-skill-list]")?.addEventListener("click", (event) => {
      const target = (event.target as HTMLElement).closest("[data-skill-path]") as HTMLElement | null;
      if (!target) return;
      const state = skillManagerState(this);
      state.selectedPath = target.dataset.skillPath || "";
      this.renderSkillList();
      this.renderSelectedSkill();
    });
    root.querySelector("[data-skill-toggle]")?.addEventListener("change", () => void this.toggleSelectedSkill());
    this.skillManagerControlsBound = true;
  },

  async openSkillManager() {
    const workspaceId = this.dataset.activeWorkspaceId;
    const root = this.querySelector("[data-skill-manager]");
    if (!root || !workspaceId) return;
    this.bindSkillManagerControls();
    this.skillManagerReturnFocus = document.activeElement;
    root.removeAttribute("hidden");
    const dialog = root.querySelector(".skill-manager-dialog") as HTMLElement | null;
    dialog?.focus();
    const workspacePath = this.querySelector(".ws-row.open .ws-path")?.textContent
      || this.querySelector("[data-active-workspace]")?.textContent || workspaceId;
    setSkillText(this, "[data-skill-workspace]", `当前工作区：${workspacePath}`);
    await this.loadWorkspaceSkills();
  },

  closeSkillManager() {
    const root = this.querySelector("[data-skill-manager]");
    if (!root || root.hidden) return;
    root.setAttribute("hidden", "");
    const returnFocus = this.skillManagerReturnFocus;
    this.skillManagerReturnFocus = undefined;
    if (returnFocus?.isConnected) returnFocus.focus();
  },

  async loadWorkspaceSkills() {
    const workspaceId = this.dataset.activeWorkspaceId;
    const root = this.querySelector("[data-skill-manager]");
    if (!workspaceId || !root || root.hidden) return;
    const state = skillManagerState(this);
    const token = Symbol(workspaceId);
    this.skillManagerLoadToken = token;
    state.loading = true;
    state.error = "";
    state.notice = "";
    setSkillText(this, "[data-skill-sidebar-status]", "加载中…");
    try {
      const response = await getWorkspaceSkills(workspaceId);
      if (this.skillManagerLoadToken !== token || this.dataset.activeWorkspaceId !== workspaceId) return;
      state.skills = response.skills || [];
      state.available = response.available !== false;
      state.error = response.available === false ? response.error || "Pi 不可用" : response.error || "";
      state.notice = response.notice || "";
      if (!state.skills.some((skill) => skill.path === state.selectedPath)) {
        state.selectedPath = state.skills[0]?.path || "";
      }
      this.renderSkillList();
      this.renderSelectedSkill();
      this.renderSkillManagerStatus();
    } catch (error) {
      if (this.skillManagerLoadToken !== token) return;
      state.error = skillError(error);
      state.skills = [];
      state.selectedPath = "";
      this.renderSkillList();
      this.renderSelectedSkill();
      this.renderSkillManagerStatus();
    } finally {
      if (this.skillManagerLoadToken === token) {
        state.loading = false;
        this.renderSkillManagerStatus();
      }
    }
  },

  renderSkillList() {
    const list = skillManagerElement(this, "[data-skill-list]");
    if (!list) return;
    const state = skillManagerState(this);
    list.replaceChildren();
    if (state.loading) return;
    for (const scope of SKILL_SCOPE_ORDER) {
      const skills = state.skills.filter((skill) => skill.scope === scope);
      if (!skills.length) continue;
      const group = document.createElement("div");
      group.className = "skill-manager-group";
      const heading = document.createElement("strong");
      heading.className = "skill-manager-group-title";
      heading.textContent = SKILL_SCOPE_LABELS[scope];
      group.append(heading);
      for (const skill of skills) {
        const item = document.createElement("button");
        item.type = "button";
        item.className = "skill-manager-item";
        item.dataset.skillPath = skill.path;
        item.classList.toggle("is-selected", skill.path === state.selectedPath);
        const dot = document.createElement("span");
        dot.className = "skill-manager-status-dot";
        dot.classList.toggle("is-disabled", !skill.enabled);
        const name = document.createElement("span");
        name.className = "skill-manager-item-name";
        name.textContent = skill.name;
        item.append(dot, name);
        group.append(item);
      }
      list.append(group);
    }
    if (!state.skills.length && !state.error) {
      const empty = document.createElement("span");
      empty.className = "skill-manager-list-empty";
      empty.textContent = "当前工作区没有可用 skill";
      list.append(empty);
    }
  },

  renderSelectedSkill() {
    const state = skillManagerState(this);
    const skill = selectedSkill(this);
    const empty = skillManagerElement(this, "[data-skill-empty]");
    const detail = skillManagerElement(this, "[data-skill-detail-content]");
    if (!skill) {
      empty?.removeAttribute("hidden");
      detail?.setAttribute("hidden", "");
      return;
    }
    empty?.setAttribute("hidden", "");
    detail?.removeAttribute("hidden");
    setSkillText(this, "[data-skill-scope]", skillScopeLabel(skill.scope));
    setSkillText(this, "[data-skill-name]", skill.name);
    setSkillText(this, "[data-skill-description]", skill.description);
    setSkillText(this, "[data-skill-path]", skill.path);
    setSkillText(this, "[data-skill-state]", skill.enabled ? "已启用" : "已禁用");
    const toggle = skillManagerElement(this, "[data-skill-toggle]") as HTMLInputElement | null;
    if (toggle) {
      toggle.checked = skill.enabled;
      toggle.disabled = !state.available || state.savingPath === skill.path || !canEditSkill(skill);
    }
    const permission = skillManagerElement(this, "[data-skill-permission]");
    const editable = canEditSkill(skill);
    if (permission) {
      permission.hidden = editable;
      permission.textContent = "全局和自定义路径 skill 需要管理员修改";
    }
  },

  renderSkillManagerStatus() {
    const state = skillManagerState(this);
    setSkillText(this, "[data-skill-count]", `${state.skills.length} 个技能`);
    setSkillText(this, "[data-skill-sidebar-status]", state.loading ? "加载中…" : state.error || (state.skills.length ? "已加载" : "无技能"));
    setSkillText(this, "[data-skill-status]", state.error || state.notice);
    const status = skillManagerElement(this, "[data-skill-status]");
    status?.classList.toggle("is-error", Boolean(state.error));
  },

  async toggleSelectedSkill() {
    const skill = selectedSkill(this);
    const state = skillManagerState(this);
    if (!skill || !canEditSkill(skill) || !state.available || state.savingPath) return;
    const toggle = skillManagerElement(this, "[data-skill-toggle]") as HTMLInputElement | null;
    if (!toggle) return;
    const enabled = toggle.checked;
    state.savingPath = skill.path;
    state.error = "";
    this.renderSelectedSkill();
    try {
      const response = await setWorkspaceSkillEnabled(this.dataset.activeWorkspaceId, skill.path, enabled);
      const updated = response.skill as WorkspaceSkill;
      state.skills = state.skills.map((item) => item.path === updated.path ? updated : item);
      this.renderSkillList();
      this.renderSelectedSkill();
    } catch (error) {
      toggle.checked = skill.enabled;
      state.error = skillError(error);
      this.renderSelectedSkill();
      this.renderSkillManagerStatus();
    } finally {
      state.savingPath = "";
      this.renderSelectedSkill();
    }
  },
};
