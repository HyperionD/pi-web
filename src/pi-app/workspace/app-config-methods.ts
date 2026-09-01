import { getAppConfig } from "../../lib/api";
import type { AppConfigPatch } from "../../lib/api";
import { DEFAULT_APP_BRAND, parseAppConfigResponse } from "./app-config-schema";

function normalizedBrand(value: unknown) {
  if (typeof value !== "string") return DEFAULT_APP_BRAND;
  const brand = value.trim();
  return brand || DEFAULT_APP_BRAND;
}

function allowedRootsFromState(state) {
  const roots = state?.config?.workspace?.allowedRoots;
  return Array.isArray(roots) ? roots.filter((root) => typeof root === "string") : [];
}

export const appConfigMethods = {
  async loadAppConfigState() {
    const state = parseAppConfigResponse(await getAppConfig());
    this.appConfigState = state;
    this.applyBrand(state.config.brand);
    return state;
  },

  applyBrand(brand) {
    const value = normalizedBrand(brand);
    this.querySelectorAll("[data-brand]").forEach((element) => {
      element.textContent = value;
    });
  },

  bindAllowedRootControls() {
    if (this.allowedRootControlsBound) return;
    const addButton = this.querySelector("[data-allowed-root-add]");
    const list = this.querySelector("[data-allowed-roots-list]");
    addButton?.addEventListener("click", () => this.browseFolder?.("allowed-root"));
    list?.addEventListener("click", (event) => {
      const button = event.target.closest("[data-allowed-root-remove]");
      if (button) this.removeAllowedRootRow(button.closest("[data-allowed-root-row]"));
    });
    this.allowedRootControlsBound = true;
  },

  fillAppConfigForm() {
    const control = this.querySelector("[data-app-setting='brand']");
    if (control) {
      control.value = normalizedBrand(this.appConfigState?.config?.brand);
      if (this.appConfigState?.config?.workspace !== undefined) {
        control.dataset.initialAppBrand = control.value;
      } else {
        delete control.dataset.initialAppBrand;
      }
    }
    const roots = allowedRootsFromState(this.appConfigState);
    const rootsControl = this.querySelector("[data-app-setting='allowed-roots']");
    if (rootsControl) {
      rootsControl.value = roots.join("\n");
      rootsControl.dataset.initialAllowedRoots = JSON.stringify(roots);
    }
    this.bindAllowedRootControls();
    this.renderAllowedRootRows(roots);
  },

  renderAllowedRootRows(roots) {
    const list = this.querySelector("[data-allowed-roots-list]");
    if (!list) return;
    list.replaceChildren();
    roots.forEach((root, index) => {
      const row = document.createElement("div");
      row.dataset.allowedRootRow = "";
      const label = document.createElement("label");
      const input = document.createElement("input");
      const inputId = `allowed-root-${index + 1}`;
      label.htmlFor = inputId;
      label.textContent = `目录 ${index + 1}`;
      input.id = inputId;
      input.type = "text";
      input.value = root;
      input.dataset.allowedRootInput = "";
      input.autocomplete = "off";
      const remove = document.createElement("button");
      remove.type = "button";
      remove.dataset.allowedRootRemove = "";
      remove.textContent = "移除";
      row.append(label, input, remove);
      list.append(row);
    });
    const hint = this.querySelector("[data-allowed-roots-hint]");
    if (hint) hint.textContent = roots.length ? "仅允许访问上列目录" : "不限制工作区目录";
    const control = this.querySelector("[data-app-setting='allowed-roots']");
    if (control) {
      control.value = roots.join("\n");
      control.dataset.renderedAllowedRoots = JSON.stringify(roots);
    }
  },

  collectAllowedRoots() {
    return [...this.querySelectorAll("[data-allowed-root-row]")]
      .map((row) => row.querySelector("[data-allowed-root-input]")?.value.trim())
      .filter(Boolean);
  },

  addAllowedRootRow() {
    const roots = this.collectAllowedRoots();
    this.renderAllowedRootRows([...roots, ""]);
    this.querySelector("[data-allowed-root-row]:last-child input")?.focus();
  },

  removeAllowedRootRow(row) {
    if (!row) return;
    row.remove();
    const roots = this.collectAllowedRoots();
    this.renderAllowedRootRows(roots);
  },

  addAllowedRootFromFolder(path) {
    const root = String(path || "").trim();
    if (!root) return;
    const roots = this.collectAllowedRoots();
    if (!roots.includes(root)) this.renderAllowedRootRows([...roots, root]);
    this.restoreAllowedRootPicker();
  },

  restoreAllowedRootPicker() {
    const returnRoute = this.allowedRootPickerReturnRoute;
    if (!returnRoute) return;
    delete this.allowedRootPickerReturnRoute;
    const settingsWasOpen = this.allowedRootPickerSettingsWasOpen;
    delete this.allowedRootPickerSettingsWasOpen;
    this.folderBrowsePurpose = "workspace";
    this.querySelector("[data-folder-browser]")?.setAttribute("hidden", "");
    this.dataset.route = returnRoute;
    this.querySelector('[data-view="picker"]')?.toggleAttribute("hidden", returnRoute !== "picker");
    this.querySelector('[data-view="workspace"]')?.toggleAttribute("hidden", returnRoute !== "workspace");
    if (settingsWasOpen) this.settingsModal?.removeAttribute("hidden");
  },

  appConfigPatchFromForm() {
    const brandControl = this.querySelector("[data-app-setting='brand']");
    const brand = brandControl?.value.trim();
    const initialBrand = brandControl?.dataset.initialAppBrand;
    const patch: AppConfigPatch = {};
    if (brandControl && (initialBrand === undefined || brand !== initialBrand)) patch.brand = brand;
    const rootsControl = this.querySelector("[data-app-setting='allowed-roots']");
    if (!rootsControl) return patch;
    const roots = this.collectAllowedRoots();
    const initial = rootsControl.dataset.initialAllowedRoots;
    const initialRoots = initial ? JSON.parse(initial) : [];
    if (roots.length || JSON.stringify(roots) !== JSON.stringify(initialRoots)) {
      patch.workspace = { allowedRoots: roots };
    }
    return patch;
  },
};
