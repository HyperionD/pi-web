import {
  getAuthProviders,
  getOAuthProviders,
  getWorkspaceModels,
  getWorkspaceSettings,
  logoutProvider,
  saveAPIKey,
  saveAppConfig,
  saveWorkspaceSettings,
  setSessionModel,
} from "../../lib/api";
import {
  SETTINGS_FIELDS,
  parseSettingsPatch,
  parseWorkspaceSettings,
  settingsScopeSchema,
} from "./settings-schema";
import { parseAppConfigResponse } from "./app-config-schema";

function valueAt(settings, path) {
  return path.split(".").reduce((value, part) => value?.[part], settings);
}

function setPatchValue(patch, path, value) {
  const parts = path.split(".");
  let target = patch;
  for (const part of parts.slice(0, -1)) {
    target[part] ??= {};
    target = target[part];
  }
  target[parts.at(-1)] = value;
}

function settingLabel(path) {
  return path.replace(/([A-Z])/g, " $1").replace(/\./g, " · ").replace(/^./, (char) => char.toUpperCase());
}

function describeEffective(value) {
  if (value === undefined) return "not set";
  if (typeof value === "boolean") return value ? "on" : "off";
  return String(value);
}

function customInputFor(control) {
  return control.closest(".settings-field")?.querySelector(`[data-custom-setting='${control.dataset.setting}']`);
}

const SETTINGS_FOCUSABLE_SELECTOR = [
  "a[href]",
  "button:not([disabled])",
  "input:not([disabled]):not([type='hidden'])",
  "select:not([disabled])",
  "textarea:not([disabled])",
  "summary",
  "[tabindex]:not([tabindex='-1'])",
].join(",");

function visibleSettingsFocusables(root) {
  return [...root.querySelectorAll(SETTINGS_FOCUSABLE_SELECTOR)]
    .filter((element): element is HTMLElement => (
      element instanceof HTMLElement && element.getClientRects().length > 0 && !element.closest("[hidden]")
    ));
}

function settingsValueChanged(scopedSettings, effective, path, value) {
  const explicitValue = valueAt(scopedSettings, path);
  if (explicitValue !== undefined) return explicitValue !== value;
  if (value === null) return false;
  const effectiveValue = valueAt(effective, path);
  return effectiveValue !== value;
}

export const settingsMethods = {
  async loadWorkspaceSettingsState(workspaceId = this.dataset.activeWorkspaceId) {
    if (!workspaceId || !this.apiConnected) return;
    const loadToken = Symbol(workspaceId);
    this.workspaceSettingsLoadToken = loadToken;
    const isCurrentLoad = () => (
      this.dataset.activeWorkspaceId === workspaceId && this.workspaceSettingsLoadToken === loadToken
    );
    try {
      const { settings } = await getWorkspaceSettings(workspaceId);
      if (!isCurrentLoad()) return;
      this.settingsState = parseWorkspaceSettings(settings);
    } catch (error) {
      if (!isCurrentLoad()) return;
      if (!this.recoverForbiddenWorkspace?.(error)) throw error;
    }
  },

  setAppConfigLoading(loading) {
    const control = this.querySelector("[data-app-setting='brand']");
    if (!control) return;
    control.disabled = loading;
    if (loading) control.placeholder = "loading…";
  },

  fillAppConfigFormState() {
    this.fillAppConfigForm?.();
    const path = this.querySelector("[data-app-config-path]");
    if (path) path.textContent = this.appConfigState?.path || "—";
  },

  async openSettingsModal() {
    const workspaceId = this.dataset.activeWorkspaceId;
    const settingsLoadToken = workspaceId ? Symbol(workspaceId) : undefined;
    if (settingsLoadToken) this.settingsModalLoadToken = settingsLoadToken;
    const isCurrentWorkspaceLoad = () => (
      !workspaceId
      || (this.dataset.activeWorkspaceId === workspaceId && this.settingsModalLoadToken === settingsLoadToken)
    );
    const activeElement = document.activeElement;
    if (activeElement instanceof HTMLElement && !this.settingsModal?.contains(activeElement)) {
      this.settingsReturnFocus = activeElement;
    }
    this.settingsModal?.removeAttribute("hidden");
    window.requestAnimationFrame(() => this.focusSettingsModal?.());
    if (!this.apiConnected) {
      this.setSettingsStatus("backend disconnected", true);
      return;
    }
    this.setSettingsStatus("loading settings…");
    this.setAppConfigLoading(true);
    if (!workspaceId) {
      const [appConfigResult] = await Promise.allSettled([this.loadAppConfigState()]);
      this.fillAppConfigFormState();
      this.setAppConfigLoading(false);
      if (appConfigResult.status === "rejected") {
        const message = appConfigResult.reason instanceof Error ? appConfigResult.reason.message : String(appConfigResult.reason);
        this.setSettingsStatus(message, true);
        this.setConnection("err");
      } else {
        this.setSettingsStatus("blank fields inherit from effective settings");
      }
      this.focusSettingsModal?.();
      return;
    }
    this.setModelControlsLoading("loading models…");
    const [appConfigResult, settingsResult, authResult, oauthResult, modelsResult] = await Promise.allSettled([
      this.loadAppConfigState(),
      getWorkspaceSettings(workspaceId),
      getAuthProviders(),
      getOAuthProviders(),
      getWorkspaceModels(workspaceId),
    ]);
    this.fillAppConfigFormState();
    this.setAppConfigLoading(false);
    if (!isCurrentWorkspaceLoad()) return;
    const errors = [];
    if (appConfigResult.status === "rejected") {
      errors.push(appConfigResult.reason instanceof Error ? appConfigResult.reason.message : String(appConfigResult.reason));
    }
    if (settingsResult.status === "rejected") {
      if (isCurrentWorkspaceLoad() && this.recoverForbiddenWorkspace?.(settingsResult.reason)) return;
      errors.push(settingsResult.reason instanceof Error ? settingsResult.reason.message : String(settingsResult.reason));
      this.setSettingsStatus(errors.join("; "), true);
      this.setConnection("err");
      this.focusSettingsModal?.();
      return;
    }
    this.settingsState = parseWorkspaceSettings(settingsResult.value.settings);
    if (authResult.status === "fulfilled") {
      this.authState = authResult.value;
      this.fillAuthForm();
    }
    if (oauthResult.status === "fulfilled") {
      this.oauthState = oauthResult.value;
      this.fillOAuthForm();
    }
    if (modelsResult.status === "fulfilled") {
      this.modelState = modelsResult.value;
      this.fillModelControls();
    } else {
      this.modelState = null;
      this.fillFallbackModelControls();
    }
    this.fillSettingsForm();
    errors.push(...[authResult, oauthResult, modelsResult]
      .filter((result) => result.status === "rejected")
      .map((result) => result.reason instanceof Error ? result.reason.message : String(result.reason)));
    if (modelsResult.status === "fulfilled" && modelsResult.value?.error) errors.push(modelsResult.value.error);
    this.setSettingsStatus(errors[0] || "blank fields inherit from effective settings", errors.length > 0);
    if (errors.length > 0) this.setConnection("err");
    this.focusSettingsModal?.();
  },

  closeSettingsModal() {
    const wasOpen = this.settingsModal && !this.settingsModal.hidden;
    this.settingsModal?.setAttribute("hidden", "");
    if (!wasOpen) return;
    const returnFocus = this.settingsReturnFocus;
    this.settingsReturnFocus = undefined;
    if (returnFocus?.isConnected) returnFocus.focus();
    else this.prompt?.focus?.();
  },

  focusSettingsModal() {
    if (!this.settingsModal || this.settingsModal.hidden) return;
    if (document.activeElement && this.settingsModal.contains(document.activeElement)) return;
    const target = this.settingsModal.querySelector("[data-action='close-settings']")
      || visibleSettingsFocusables(this.settingsModal)[0]
      || this.settingsModal.querySelector(".settings-dialog");
    target?.focus?.();
  },

  trapSettingsFocus(event) {
    if (!this.settingsModal || this.settingsModal.hidden || event.key !== "Tab") return;
    const focusables = visibleSettingsFocusables(this.settingsModal);
    if (!focusables.length) {
      event.preventDefault();
      this.settingsModal.querySelector(".settings-dialog")?.focus?.();
      return;
    }
    const first = focusables[0];
    const last = focusables.at(-1);
    const activeElement = document.activeElement;
    if (!this.settingsModal.contains(activeElement)) {
      event.preventDefault();
      (event.shiftKey ? last : first).focus();
    } else if (event.shiftKey && activeElement === first) {
      event.preventDefault();
      last.focus();
    } else if (!event.shiftKey && activeElement === last) {
      event.preventDefault();
      first.focus();
    }
  },

  setModelControlsLoading(message = "loading models…") {
    for (const control of this.querySelectorAll("[data-setting='defaultProvider'], [data-setting='defaultModel']")) {
      const option = document.createElement("option");
      option.value = "inherit";
      option.textContent = message;
      control.replaceChildren(option);
      control.value = "inherit";
      control.disabled = true;
      const customInput = customInputFor(control);
      if (customInput) {
        customInput.value = "";
        customInput.hidden = true;
      }
    }
  },

  fillModelControls() {
    const providerControl = this.querySelector("[data-setting='defaultProvider']");
    const modelControl = this.querySelector("[data-setting='defaultModel']");
    if (!providerControl || !modelControl || !this.modelState?.providers) return;
    const providers = this.modelState.providers;
    this.replaceSelectOptions(providerControl, providers.map((provider) => provider.id));
    const selectedProvider = providerControl.value && providerControl.value !== "inherit" && providerControl.value !== "custom"
      ? providerControl.value
      : providers[0]?.id;
    const providerModels = providers.find((provider) => provider.id === selectedProvider)?.models;
    const models = providerModels || providers.flatMap((provider) => provider.models || []);
    this.replaceSelectOptions(modelControl, models.map((model) => model.id));
  },

  fillFallbackModelControls() {
    const effective = this.settingsState?.effective || {};
    const provider = typeof effective.defaultProvider === "string" ? effective.defaultProvider : undefined;
    const model = typeof effective.defaultModel === "string" ? effective.defaultModel : undefined;
    const providerControl = this.querySelector("[data-setting='defaultProvider']");
    const modelControl = this.querySelector("[data-setting='defaultModel']");
    if (providerControl) this.replaceSelectOptions(providerControl, provider ? [provider] : []);
    if (modelControl) this.replaceSelectOptions(modelControl, model ? [model] : []);
  },

  replaceSelectOptions(control, values) {
    const previousValue = control.value;
    const options = ["inherit", ...values, "custom"];
    control.replaceChildren(...options.map((value) => {
      const option = document.createElement("option");
      option.value = value;
      option.textContent = value === "custom" ? "custom…" : value;
      return option;
    }));
    control.disabled = false;
    control.value = options.includes(previousValue) ? previousValue : "inherit";
    this.syncCustomSettingInput(control);
  },

  syncCustomSettingInput(control) {
    const customInput = customInputFor(control);
    if (!customInput) return;
    const custom = control.value === "custom";
    customInput.hidden = !custom;
    if (custom) customInput.focus?.();
  },

  fillSettingsForm() {
    const form = this.querySelector("[data-settings-form]");
    if (!form || !this.settingsState) return;
    const scope = form.querySelector("[name='scope']")?.value || "project";
    const scopedSettings = this.settingsState[scope] || {};
    const effective = this.settingsState.effective || {};
    this.querySelector("[data-settings-path]").textContent = this.settingsState.paths?.[scope] || "—";
    for (const field of SETTINGS_FIELDS) {
      const control = form.querySelector(`[data-setting='${field.path}']`);
      if (!control) continue;
      const explicitValue = valueAt(scopedSettings, field.path);
      const effectiveValue = valueAt(effective, field.path);
      const hint = control.closest(".settings-field")?.querySelector("small");
      const effectiveLabel = describeEffective(effectiveValue);
      hint?.replaceChildren(`effective: ${effectiveLabel}`);
      this.fillSettingsControl(control, field, explicitValue, effectiveValue);
      if (field.path === "defaultProvider") this.fillModelControls();
    }
  },

  fillSettingsControl(control, field, explicitValue, effectiveValue) {
    control.dataset.settingType = field.type;
    if (field.type === "providerSelect" || field.type === "modelSelect") {
      const value = explicitValue === undefined ? "inherit" : String(explicitValue);
      const customInput = customInputFor(control);
      if ([...control.options].some((option) => option.value === value)) {
        control.value = value;
        if (customInput) customInput.value = "";
      } else {
        control.value = "custom";
        if (customInput) customInput.value = value;
      }
      this.syncCustomSettingInput(control);
      return;
    }
    if (field.type === "text" || field.type === "numberText") {
      control.value = explicitValue === undefined ? "" : String(explicitValue);
      control.placeholder = field.type === "password" && effectiveValue ? "set" : describeEffective(effectiveValue);
      return;
    }
    if (field.type === "boolean") {
      control.value = explicitValue === undefined ? "inherit" : String(explicitValue);
      return;
    }
    if (field.type === "checkbox") {
      control.checked = explicitValue === undefined ? effectiveValue === true : explicitValue === true;
      return;
    }
    control.value = explicitValue === undefined ? "inherit" : String(explicitValue);
  },

  fillAuthForm() {
    const providerSelect = this.querySelector("[data-auth-provider]");
    const status = this.querySelector("[data-auth-status]");
    if (!providerSelect || !this.authState?.providers) return;
    const previousValue = providerSelect.value;
    const providerOptions = this.authState.providers.map((provider) => {
      const option = document.createElement("option");
      option.value = provider.id;
      option.textContent = provider.configured ? `${provider.name} ✓` : provider.name;
      return option;
    });
    providerSelect.replaceChildren(...providerOptions);
    if (previousValue && this.authState.providers.some((provider) => provider.id === previousValue)) {
      providerSelect.value = previousValue;
    }
    this.setAuthStatus(status?.textContent || "API key is stored in ~/.pi/agent/auth.json");
  },

  async saveAuthForm(event) {
    event?.preventDefault();
    const form = this.querySelector("[data-auth-form]");
    if (!form || !this.apiConnected) return;
    const apiKeyControl = form.querySelector("[data-auth-api-key]");
    const provider = form.querySelector("[data-auth-provider]")?.value;
    const apiKey = apiKeyControl?.value?.trim();
    if (!provider || !apiKey) {
      this.setAuthStatus("provider and API key are required", true);
      return;
    }
    this.setAuthStatus("saving API key…");
    try {
      await saveAPIKey(provider, apiKey);
      apiKeyControl.value = "";
      this.authState = await getAuthProviders();
      this.fillAuthForm();
      this.setAuthStatus("API key saved");
      void this.loadRuntimeStatus?.(this.dataset.activeWorkspaceId);
    } catch (error) {
      this.setAuthStatus(error instanceof Error ? error.message : String(error), true);
      this.setConnection("err");
    }
  },

  async logoutAuthProvider() {
    const provider = this.querySelector("[data-auth-provider]")?.value;
    if (!provider || !this.apiConnected) return;
    this.setAuthStatus("removing credential…");
    try {
      await logoutProvider(provider);
      this.authState = await getAuthProviders();
      this.fillAuthForm();
      this.setAuthStatus("credential removed");
      void this.loadRuntimeStatus?.(this.dataset.activeWorkspaceId);
    } catch (error) {
      this.setAuthStatus(error instanceof Error ? error.message : String(error), true);
      this.setConnection("err");
    }
  },

  setAuthStatus(message, error = false) {
    const status = this.querySelector("[data-auth-status]");
    if (!status) return;
    status.textContent = message;
    status.classList.toggle("err", error);
  },

  async saveSettingsForm(event) {
    event?.preventDefault();
    const form = this.querySelector("[data-settings-form]");
    const workspaceId = this.dataset.activeWorkspaceId;
    if (!form || !this.apiConnected) return;
    const scopeValue = form.querySelector("[name='scope']")?.value || "project";
    this.setSettingsStatus("saving…");
    try {
      const appConfigPatch = this.appConfigPatchFromForm?.() || {};
      const scope = settingsScopeSchema.parse(scopeValue);
      const patch = workspaceId ? parseSettingsPatch(this.settingsPatchFromForm(form)) : {};
      const appConfigRequest = Object.keys(appConfigPatch).length
        ? saveAppConfig(appConfigPatch)
        : Promise.resolve(undefined);
      const settingsRequest = workspaceId && Object.keys(patch).length
        ? saveWorkspaceSettings(workspaceId, scope, patch)
        : Promise.resolve(undefined);
      const [appConfigResult, settingsResult] = await Promise.allSettled([appConfigRequest, settingsRequest]);
      const errors = [];
      if (appConfigResult.status === "fulfilled" && appConfigResult.value) {
        try {
          this.appConfigState = parseAppConfigResponse(appConfigResult.value);
          this.applyBrand(this.appConfigState.config.brand);
          this.fillAppConfigFormState();
        } catch (error) {
          errors.push(error instanceof Error ? error.message : String(error));
        }
      } else if (appConfigResult.status === "rejected") {
        errors.push(appConfigResult.reason instanceof Error ? appConfigResult.reason.message : String(appConfigResult.reason));
      }
      if (settingsResult.status === "fulfilled" && settingsResult.value) {
        try {
          this.settingsState = parseWorkspaceSettings(settingsResult.value.settings);
          this.fillSettingsForm();
          void this.loadRuntimeStatus?.(workspaceId);
          if ("defaultProvider" in patch || "defaultModel" in patch) void this.applyEffectiveModelToActiveSession();
        } catch (error) {
          errors.push(error instanceof Error ? error.message : String(error));
        }
      } else if (settingsResult.status === "rejected") {
        errors.push(settingsResult.reason instanceof Error ? settingsResult.reason.message : String(settingsResult.reason));
      }
      if (errors.length > 0) {
        this.setSettingsStatus(errors.join("; "), true);
        this.setConnection("err");
        return;
      }
      this.setSettingsStatus("saved");
    } catch (error) {
      this.setSettingsStatus(error instanceof Error ? error.message : String(error), true);
      this.setConnection("err");
    }
  },

  async applyEffectiveModelToActiveSession() {
    const sessionId = this.dataset.activeSessionId;
    if (!sessionId) return;
    const effective = this.settingsState?.effective || {};
    const provider = typeof effective.defaultProvider === "string" && effective.defaultProvider !== "inherit"
      ? effective.defaultProvider
      : "";
    const model = typeof effective.defaultModel === "string" && effective.defaultModel !== "inherit"
      ? effective.defaultModel
      : "";
    if (!provider || !model) return;
    try {
      await setSessionModel(sessionId, provider, model);
      void this.loadRuntimeStatus?.();
    } catch {
      this.setSettingsStatus("模型已保存，但当前会话切换失败：模型不可用", true);
    }
  },

  settingsPatchFromForm(form) {
    const patch = {};
    const scope = form.querySelector("[name='scope']")?.value || "project";
    const scopedSettings = this.settingsState?.[scope] || {};
    const effective = this.settingsState?.effective || {};
    const addChangedValue = (path, value) => {
      if (settingsValueChanged(scopedSettings, effective, path, value)) setPatchValue(patch, path, value);
    };
    for (const field of SETTINGS_FIELDS) {
      const control = form.querySelector(`[data-setting='${field.path}']`);
      if (!control) continue;
      addChangedValue(field.path, this.settingValueFromControl(control, field));
    }
    return patch;
  },

  settingValueFromControl(control, field) {
    if (field.type === "text" || field.type === "password") {
      const value = control.value.trim();
      return value ? value : null;
    }
    if (field.type === "numberText") {
      const value = control.value.trim();
      return value ? Number(value) : null;
    }
    if (field.type === "providerSelect" || field.type === "modelSelect") {
      if (control.value === "inherit") return null;
      if (control.value === "custom") {
        const value = customInputFor(control)?.value?.trim();
        return value ? value : null;
      }
      return control.value;
    }
    if (field.type === "checkbox") return control.checked === true;
    if (control.value === "inherit") return null;
    if (field.type === "boolean") return control.value === "true";
    if (field.type === "numberSelect") return Number(control.value);
    return control.value;
  },

  setSettingsStatus(message, error = false) {
    const status = this.querySelector("[data-settings-status]");
    if (!status) return;
    status.textContent = message;
    status.classList.toggle("err", error);
  },

  settingsFieldLabel(path) {
    return settingLabel(path);
  },
};
