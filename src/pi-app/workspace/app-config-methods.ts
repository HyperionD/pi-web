import { getAppConfig } from "../../lib/api";
import { DEFAULT_APP_BRAND, parseAppConfigResponse } from "./app-config-schema";

function normalizedBrand(value: unknown) {
  if (typeof value !== "string") return DEFAULT_APP_BRAND;
  const brand = value.trim();
  return brand || DEFAULT_APP_BRAND;
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

  fillAppConfigForm() {
    const control = this.querySelector("[data-app-setting='brand']");
    if (control) control.value = normalizedBrand(this.appConfigState?.config?.brand);
  },

  appConfigPatchFromForm() {
    const control = this.querySelector("[data-app-setting='brand']");
    return control ? { brand: control.value.trim() } : {};
  },
};
