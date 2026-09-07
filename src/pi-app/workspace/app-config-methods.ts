import { getAdminPasswordStatus, getAppConfig, setupAdminPassword } from "../../lib/api";
import type { AppConfigPatch } from "../../lib/api";
import { DEFAULT_APP_BRAND, parseAppConfigResponse } from "./app-config-schema";

export const DEFAULT_MAX_UPLOAD_MIB = 100;
const BYTES_PER_MIB = 1024 * 1024;
const MAX_UPLOAD_MIB_ERROR = "上传上限必须是 1–2048 MiB 的整数";

export function maxUploadBytesToMiB(bytes: unknown) {
  if (bytes === undefined || bytes === null) return DEFAULT_MAX_UPLOAD_MIB;
  const mib = typeof bytes === "number" && Number.isInteger(bytes) ? bytes / BYTES_PER_MIB : NaN;
  return Number.isInteger(mib) && mib >= 1 && mib <= 2048 ? mib : DEFAULT_MAX_UPLOAD_MIB;
}

export function maxUploadMiBToBytes(value: unknown) {
  const normalized = typeof value === "string" ? value.trim() : value;
  const mib = normalized === "" ? NaN : Number(normalized);
  if (!Number.isInteger(mib) || mib < 1 || mib > 2048) throw new Error(MAX_UPLOAD_MIB_ERROR);
  return mib * BYTES_PER_MIB;
}

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
  async loadAdminPasswordState() {
    const status = await getAdminPasswordStatus();
    if (typeof status?.configured !== "boolean") return;
    this.adminPasswordConfigured = status.configured;
    this.syncAdminPasswordGate();
  },

  syncAdminPasswordGate() {
    const gate = this.querySelector("[data-admin-password-gate]");
    if (!gate) return;
    gate.hidden = this.adminPasswordConfigured !== false;
    if (this.adminPasswordConfigured === false) {
      this.querySelector("[data-admin-password]")?.focus?.();
    }
  },

  showAdminPasswordGate() {
    this.adminPasswordConfigured = false;
    this.syncAdminPasswordGate();
  },

  setAdminPasswordStatus(message, error = false) {
    const status = this.querySelector("[data-admin-password-status]");
    if (!status) return;
    status.textContent = message;
    status.classList.toggle("err", error);
  },

  async submitAdminPasswordSetup(event) {
    event?.preventDefault();
    const password = this.querySelector("[data-admin-password]")?.value || "";
    const confirmation = this.querySelector("[data-admin-password-confirmation]")?.value || "";
    this.setAdminPasswordStatus("正在设置…");
    try {
      await setupAdminPassword(password, confirmation);
      this.adminPasswordConfigured = true;
      this.querySelector("[data-admin-password]").value = "";
      this.querySelector("[data-admin-password-confirmation]").value = "";
      this.setAdminPasswordStatus("");
      this.syncAdminPasswordGate();
    } catch (error) {
      this.setAdminPasswordStatus(error instanceof Error ? error.message : String(error), true);
    }
  },

  async requestAdminPassword() {
    if (this.adminPasswordConfigured !== true) return "";
    const password = window.prompt("请输入管理员密码");
    return password || undefined;
  },

  async loadAppConfigState(bootstrapGeneration) {
    const state = parseAppConfigResponse(await getAppConfig());
    if (bootstrapGeneration && this.bootstrapGeneration !== bootstrapGeneration) return undefined;
    this.appConfigState = state;
    this.applyBrand(state.config.brand);
    return state;
  },

  applyBrand(brand) {
    const value = normalizedBrand(brand);
    this.querySelectorAll("[data-brand]").forEach((element) => {
      element.textContent = value;
    });
    document.title = value;
  },

  bindAllowedRootControls() {
    if (this.allowedRootControlsBound) return;
    const addButton = this.querySelector("[data-allowed-root-add]");
    const list = this.querySelector("[data-allowed-roots-list]");
    addButton?.addEventListener("click", (event) => this.browseFolder?.("allowed-root", event));
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
      control.dataset.initialAppBrand = control.value;
    }
    const uploadControl = this.querySelector("[data-app-setting='max-upload-mib']");
    if (uploadControl) {
      uploadControl.value = String(maxUploadBytesToMiB(this.appConfigState?.config?.workspace?.maxUploadBytes));
      uploadControl.dataset.initialMaxUploadBytes = String(maxUploadMiBToBytes(uploadControl.value));
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
    this.route(returnRoute === "picker" ? "workspace" : returnRoute);
    if (settingsWasOpen) {
      this.settingsModal?.removeAttribute("hidden");
      this.querySelector("[data-allowed-root-add]")?.focus();
    }
  },

  appConfigPatchFromForm() {
    const brandControl = this.querySelector("[data-app-setting='brand']");
    const brand = brandControl?.value.trim();
    const initialBrand = brandControl?.dataset.initialAppBrand;
    const patch: AppConfigPatch = {};
    if (brandControl && initialBrand !== undefined && brand !== initialBrand) patch.brand = brand;
    const workspacePatch: NonNullable<AppConfigPatch["workspace"]> = {};
    const uploadControl = this.querySelector("[data-app-setting='max-upload-mib']");
    const initialMaxUploadBytes = uploadControl?.dataset.initialMaxUploadBytes;
    if (uploadControl && initialMaxUploadBytes !== undefined) {
      const maxUploadBytes = maxUploadMiBToBytes(uploadControl.value);
      if (maxUploadBytes !== Number(initialMaxUploadBytes)) workspacePatch.maxUploadBytes = maxUploadBytes;
    }
    const rootsControl = this.querySelector("[data-app-setting='allowed-roots']");
    if (rootsControl) {
      const roots = this.collectAllowedRoots();
      const initial = rootsControl.dataset.initialAllowedRoots;
      const initialRoots = initial ? JSON.parse(initial) : [];
      if (JSON.stringify(roots) !== JSON.stringify(initialRoots)) workspacePatch.allowedRoots = roots;
    }
    if (Object.keys(workspacePatch).length) patch.workspace = workspacePatch;
    return patch;
  },
};
