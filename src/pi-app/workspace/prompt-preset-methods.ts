import {
  createPromptPreset,
  deletePromptPreset,
  getPromptPresets,
  updatePromptPreset,
  type PromptPreset,
  type PromptPresetInput,
} from "../../lib/api";
import {
  formatPromptPreset,
  parsePromptPresetResponse,
  parsePromptPresetsResponse,
  promptPresetSkillOptions,
  visiblePromptPresets,
} from "../input/prompt-presets";

function presetError(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

function presetState(app) {
  app.promptPresets ??= [];
  app.promptPresetState ??= { selectedId: "", draft: null, loading: false, saving: false, error: "" };
  return app.promptPresetState;
}

function editorValue(app, selector: string): string {
  return app.querySelector(selector)?.value || "";
}

export const promptPresetMethods = {
  async loadPromptPresets() {
    try {
      const response = await getPromptPresets();
      this.promptPresets = parsePromptPresetsResponse(response);
      this.renderPromptPresetButtons?.();
      if (this.promptPresetManager && !this.promptPresetManager.hidden) this.renderPromptPresetManager?.();
    } catch (error) {
      this.promptPresets = [];
      this.renderPromptPresetButtons?.();
      this.showToast?.("error", presetError(error));
    }
  },

  renderPromptPresetButtons() {
    const root = this.querySelector("[data-prompt-presets]");
    const list = this.querySelector("[data-prompt-preset-list]");
    if (!root || !list) return;
    list.replaceChildren();
    for (const preset of visiblePromptPresets(this.promptPresets || [])) {
      const button = document.createElement("button");
      button.type = "button";
      button.dataset.action = "prompt-preset";
      button.dataset.promptPresetId = preset.id;
      button.textContent = preset.label;
      button.title = formatPromptPreset(preset);
      list.append(button);
    }
    root.hidden = !(this.promptPresets || []).some((preset) => preset.enabled);
  },

  bindPromptPresetManager() {
    if (this.promptPresetManagerBound) return;
    const root = this.querySelector("[data-prompt-preset-manager]");
    if (!root) return;
    root.querySelector("[data-prompt-preset-close]")?.addEventListener("click", () => this.closePromptPresetManager());
    root.querySelector("[data-prompt-preset-cancel]")?.addEventListener("click", () => this.closePromptPresetManager());
    root.querySelector("[data-prompt-preset-overlay]")?.addEventListener(
      "click", () => this.closePromptPresetManager(),
    );
    root.querySelector("[data-prompt-preset-new]")?.addEventListener("click", () => this.newPromptPreset());
    root.querySelector("[data-prompt-preset-delete]")?.addEventListener(
      "click", () => void this.deleteSelectedPromptPreset(),
    );
    root.querySelector("[data-prompt-preset-list]")?.addEventListener("click", (event) => {
      const target = event.target.closest("[data-prompt-preset-id]");
      if (target) this.selectPromptPreset(target.dataset.promptPresetId);
    });
    root.querySelector("[data-prompt-preset-form]")?.addEventListener(
      "submit", (event) => void this.savePromptPreset(event),
    );
    this.promptPresetManagerBound = true;
  },

  async openPromptPresetManager() {
    if (document.body.dataset.userManagement === "true" && document.body.dataset.accountRole !== "admin") return;
    const root = this.querySelector("[data-prompt-preset-manager]");
    if (!root) return;
    this.bindPromptPresetManager();
    this.promptPresetManager = root;
    this.promptPresetReturnFocus = document.activeElement;
    root.hidden = false;
    await this.loadPromptPresetSkills();
    root.querySelector("[data-prompt-preset-form]")?.focus();
    this.renderPromptPresetManager();
  },

  async loadPromptPresetSkills() {
    const workspaceId = this.dataset.activeWorkspaceId;
    if (workspaceId && this.apiConnected && !this.slashCommandsLoadedAt) {
      await this.loadWorkspaceCommands?.(workspaceId);
    }
    this.renderPromptPresetSkillOptions();
  },

  closePromptPresetManager() {
    const root = this.promptPresetManager || this.querySelector("[data-prompt-preset-manager]");
    if (!root || root.hidden) return;
    root.hidden = true;
    const returnFocus = this.promptPresetReturnFocus;
    this.promptPresetReturnFocus = undefined;
    if (returnFocus?.isConnected) returnFocus.focus();
  },

  renderPromptPresetManager() {
    const root = this.promptPresetManager || this.querySelector("[data-prompt-preset-manager]");
    if (!root) return;
    const state = presetState(this);
    const list = root.querySelector("[data-prompt-preset-list]");
    const empty = root.querySelector("[data-prompt-preset-empty]");
    list?.replaceChildren();
    for (const preset of this.promptPresets || []) list?.append(this.renderPromptPresetItem(preset, state.selectedId));
    if (empty) empty.hidden = Boolean((this.promptPresets || []).length);
    this.renderPromptPresetSkillOptions();
    this.renderPromptPresetEditor();
  },

  renderPromptPresetSkillOptions() {
    const select = this.querySelector("[data-prompt-preset-skill]");
    if (!select) return;
    const state = presetState(this);
    const selectedSkill = state.draft?.skill || "";
    const options = promptPresetSkillOptions(this.slashCommands || []);
    select.replaceChildren(new Option("无 Skill", ""));
    for (const option of options) {
      const element = new Option(option.label, option.value);
      element.title = option.description;
      select.append(element);
    }
    if (selectedSkill && !options.some((option) => option.value === selectedSkill)) {
      select.append(new Option(`${selectedSkill}（当前不可用）`, selectedSkill));
    }
    select.value = selectedSkill;
  },

  renderPromptPresetItem(preset: PromptPreset, selectedId: string) {
    const button = document.createElement("button");
    button.type = "button";
    button.dataset.promptPresetId = preset.id;
    button.className = "prompt-preset-item";
    button.classList.toggle("selected", preset.id === selectedId);
    const label = document.createElement("span");
    label.textContent = preset.label;
    const status = document.createElement("small");
    status.textContent = preset.enabled ? "显示中" : "已隐藏";
    button.append(label, status);
    return button;
  },

  renderPromptPresetEditor() {
    const root = this.promptPresetManager || this.querySelector("[data-prompt-preset-manager]");
    const state = presetState(this);
    const editor = root?.querySelector("[data-prompt-preset-editor]");
    const empty = root?.querySelector("[data-prompt-preset-editor-empty]");
    const save = root?.querySelector("[data-prompt-preset-save]");
    if (!state.draft) {
      if (editor) editor.hidden = true;
      if (empty) empty.hidden = false;
      if (save) save.disabled = true;
      return;
    }
    if (editor) editor.hidden = false;
    if (empty) empty.hidden = true;
    if (save) save.disabled = false;
    const label = root.querySelector("[data-prompt-preset-label]");
    const skill = root.querySelector("[data-prompt-preset-skill]");
    const prompt = root.querySelector("[data-prompt-preset-prompt]");
    const enabled = root.querySelector("[data-prompt-preset-enabled]");
    if (label) label.value = state.draft.label;
    if (skill) skill.value = state.draft.skill;
    if (prompt) prompt.value = state.draft.prompt;
    if (enabled) enabled.checked = state.draft.enabled;
  },

  selectPromptPreset(id: string) {
    const preset = (this.promptPresets || []).find((item) => item.id === id);
    if (!preset) return;
    const state = presetState(this);
    state.selectedId = id;
    state.draft = { ...preset };
    this.renderPromptPresetManager();
  },

  newPromptPreset() {
    const state = presetState(this);
    state.selectedId = "";
    state.draft = {
      label: "", skill: "", prompt: "", enabled: true,
      order: (this.promptPresets || []).length * 10 + 10,
    };
    this.renderPromptPresetManager();
    this.querySelector("[data-prompt-preset-label]")?.focus();
  },

  async savePromptPreset(event) {
    event?.preventDefault();
    const state = presetState(this);
    if (!state.draft || state.saving) return;
    const input: PromptPresetInput = {
      label: editorValue(this, "[data-prompt-preset-label]").trim(),
      skill: editorValue(this, "[data-prompt-preset-skill]").trim(),
      prompt: editorValue(this, "[data-prompt-preset-prompt]").trim(),
      enabled: Boolean(this.querySelector("[data-prompt-preset-enabled]")?.checked),
      order: state.draft.order,
    };
    if (!input.label || !input.prompt) {
      this.setPromptPresetStatus("名称和提示词不能为空", true);
      return;
    }
    state.saving = true;
    this.setPromptPresetStatus("保存中…");
    try {
      const password = await this.requestAdminPassword?.();
      if (password === undefined) {
        this.setPromptPresetStatus("需要管理员密码", true);
        return;
      }
      const result = state.selectedId
        ? await updatePromptPreset(state.selectedId, input, password)
        : await createPromptPreset(input, password);
      const saved = parsePromptPresetResponse(result);
      this.promptPresets = state.selectedId
        ? this.promptPresets.map((item) => item.id === saved.id ? saved : item)
        : [...this.promptPresets, saved];
      state.selectedId = saved.id;
      state.draft = { ...saved };
      this.renderPromptPresetButtons();
      this.renderPromptPresetManager();
      this.setPromptPresetStatus("已保存");
    } catch (error) {
      this.setPromptPresetStatus(presetError(error), true);
    } finally {
      state.saving = false;
    }
  },

  async deleteSelectedPromptPreset() {
    const state = presetState(this);
    if (!state.selectedId || state.saving) return;
    if (!window.confirm("确定删除这个预设提示词吗？")) return;
    state.saving = true;
    try {
      const password = await this.requestAdminPassword?.();
      if (password === undefined) {
        this.setPromptPresetStatus("需要管理员密码", true);
        return;
      }
      await deletePromptPreset(state.selectedId, password);
      this.promptPresets = this.promptPresets.filter((item) => item.id !== state.selectedId);
      state.selectedId = "";
      state.draft = null;
      this.renderPromptPresetButtons();
      this.renderPromptPresetManager();
      this.setPromptPresetStatus("已删除");
    } catch (error) {
      this.setPromptPresetStatus(presetError(error), true);
    } finally {
      state.saving = false;
    }
  },

  setPromptPresetStatus(message, error = false) {
    const status = this.querySelector("[data-prompt-preset-status]");
    if (!status) return;
    status.textContent = message;
    status.classList.toggle("is-error", error);
  },
};
