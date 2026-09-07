// @ts-nocheck
import { vi } from "vitest";
import "./index";

const nativeFetch = globalThis.fetch;

function memoryStorage() {
  const items = new Map();
  return {
    clear: vi.fn(() => items.clear()),
    getItem: vi.fn((key) => items.get(String(key)) || null),
    removeItem: vi.fn((key) => items.delete(String(key))),
    setItem: vi.fn((key, value) => items.set(String(key), String(value))),
  };
}

export function installPiAppFixture() {
  vi.stubGlobal("localStorage", memoryStorage());
  document.body.innerHTML = `
    <pi-app data-tree="on" data-sidebar="open">
      <span class="brand"><span data-brand>pi.dev / web</span><span class="cursor-block"></span></span>
      <button
        class="hamburger"
        type="button"
        data-action="open-drawer"
        aria-label="open sidebar"
        aria-expanded="false"
      >≡</button>
      <div class="folder-popover" data-folder-popover role="dialog" aria-label="选择工作区" hidden>
        <div class="folder-popover-head"><strong>选择工作区</strong><button type="button" data-action="folder-close" aria-label="关闭目录选择器">×</button></div>
        <div class="folder-browser" data-folder-browser>
          <div class="folder-list" data-folder-list role="tree" aria-label="文件夹列表"></div>
          <span data-folder-loading hidden aria-live="polite">加载中…</span>
          <button type="button" class="folder-confirm" data-action="folder-open-current">选择此文件夹</button>
        </div>
      </div>
      <section class="app-body" data-view="workspace">
        <div class="sidebar-wrap">
          <aside class="sidebar">
            <div class="sb-section">
              <div class="sb-head">
                <span>workspaces</span>
                <span class="sb-head-actions">
                  <button class="add" type="button" data-action="route-picker" aria-haspopup="dialog" aria-expanded="false">+ open</button>
                  <button
                    class="refresh"
                    type="button"
                    data-action="refresh-workspaces"
                    title="refresh workspaces"
                    aria-label="refresh workspaces"
                  >↻</button>
                </span>
              </div>
            </div>
          <aside class="tree">
            <div class="tree-head">
              <span class="tree-tabs"><button class="tree-tab on" type="button" data-action="show-file-tree">files</button><button class="tree-tab" type="button" data-action="show-git-history">git</button></span>
              <span class="tree-head-actions"><button type="button" data-action="tree-root-menu">+</button><button type="button" data-action="refresh-tree">↻</button></span>
            </div>
          </aside>
          <div class="sb-footer">
            <div class="sb-footer-status"><span class="status-dot"></span><span>connecting</span></div>
          </div>
          </aside>
        </div>
        <main data-main="session"><div class="term"><div class="term-inner"></div><div class="streaming-tail" data-streaming-tail></div></div></main>
        <main data-main="empty" hidden><div class="empty-shell">new session</div></main>
        <div class="file-preview-panel" data-file-preview hidden>
          <div class="fp-dialog">
            <div class="fp-head"><div><strong class="fp-path">file</strong><small></small></div><div class="fp-actions"><div class="fp-save-slot"><button type="button" data-action="save-file-preview" hidden>save</button></div><div class="fp-mode-actions"><button type="button" data-action="toggle-file-preview-mode" hidden>text</button><button type="button" data-action="close-file-preview">×</button></div></div></div>
            <div class="fp-body"></div>
          </div>
        </div>
        <div class="git-detail-panel" data-git-detail-panel hidden></div>
        <div class="scrim" data-action="close-drawer"></div>
      </section>
      <button class="sb-expand-btn"></button>
      <div class="prompt-region">
        <div class="slash-pop" hidden>
          <div class="slash-list">
            <button class="slash-item selected" data-slash="/model">/model</button>
          </div>
        </div>
        <div class="attach-chips" hidden></div>
        <textarea class="prompt-textarea"></textarea>
        <button class="stop-btn" hidden>stop</button>
        <button class="send-btn" disabled>send</button>
        <button class="attach-btn">attach</button>
        <input data-file-input type="file" />
        <div class="prompt-meta"><span data-prompt-meta></span><button type="button" data-action="compact-session" disabled>压缩上下文</button></div>
      </div>
      <div class="admin-password-gate" data-admin-password-gate hidden>
        <form data-admin-password-setup-form>
          <h2>设置管理员密码</h2>
          <p>首次使用前必须设置管理员密码。</p>
          <input data-admin-password type="password" autocomplete="new-password" />
          <input data-admin-password-confirmation type="password" autocomplete="new-password" />
          <span data-admin-password-status></span>
          <button type="submit">继续</button>
        </form>
      </div>
      <div class="settings-modal" data-settings-modal hidden>
        <form data-settings-form>
          <select name="uiLanguage" data-ui-language>
            <option value="zh-CN">简体中文</option>
          </select>
          <strong data-i18n="settingsTitle">pi settings</strong>
          <select name="scope" data-settings-scope>
            <option value="project">project</option>
            <option value="global">global</option>
          </select>
          <span data-settings-path></span>
          <section class="app-settings-section">
            <label class="settings-field app-settings-upload-limit"><span>单文件上传上限（MiB）</span><input data-app-setting="max-upload-mib" type="number" /></label>
            <label>pi-web 应用设置 <input data-app-setting="brand" /></label>
            <small data-app-config-path>—</small>
            <textarea data-app-setting="allowed-roots"></textarea>
            <div data-allowed-roots-list></div>
            <button type="button" data-allowed-root-add aria-haspopup="dialog" aria-expanded="false">add root</button>
            <small data-allowed-roots-hint></small>
          </section>
          <div data-auth-form>
            <select data-auth-provider></select>
            <input data-auth-api-key type="password" />
            <button type="button" data-action="save-auth-provider">save key</button>
            <button type="button" data-action="logout-auth-provider">logout</button>
            <span data-auth-status></span>
            <div data-oauth-fields>
              <select data-oauth-provider></select>
              <a data-oauth-link href="#" hidden></a>
              <input data-oauth-input hidden />
              <button type="button" data-action="start-oauth-login">oauth</button>
              <button type="button" data-action="send-oauth-input" hidden>send oauth</button>
              <span data-oauth-status></span>
            </div>
          </div>
          ${settingsSelect("defaultProvider", ["inherit", "custom"], true)}
          ${settingsSelect("defaultModel", ["inherit", "custom"], true)}
          ${settingsSelect("defaultThinkingLevel", ["inherit", "high"])}
          ${settingsSelect("compaction.enabled", ["inherit", "true", "false"])}
          ${settingsSelect("enableSkillCommands", ["inherit", "true", "false"])}
          ${settingsSelect("steeringMode", ["inherit", "all"])}
          ${settingsSelect("followUpMode", ["inherit", "all"])}
          ${settingsSelect("hideThinkingBlock", ["inherit", "true", "false"])}
          ${settingsSelect("warnings.anthropicExtraUsage", ["inherit", "true", "false"])}
          <span data-settings-status></span>
          <button type="submit">save</button>
        </form>
      </div>
    </pi-app>
  `;
}

function settingsField(tag, name) {
  if (tag === "input") return `<label class="settings-field"><input data-setting="${name}" /><small></small></label>`;
  return "";
}

function settingsSelect(name, values, custom = false, secure = false) {
  const options = values.map((value) => `<option value="${value}">${value}</option>`).join("");
  const customInput = custom ? `<input data-custom-setting="${name}" hidden />` : "";
  const secureOnly = secure ? " data-secure-context-only=\"https\" hidden" : "";
  return `<label class="settings-field"${secureOnly}><select data-setting="${name}">${options}</select>${customInput}<small></small></label>`;
}


export function installEmptySessionFixture() {
  vi.stubGlobal("localStorage", memoryStorage());
  document.body.innerHTML = `
    <pi-app data-active-session-id="s1" data-session="empty">
      <main data-main="session" hidden><div class="term"><div class="term-inner"></div><div class="streaming-tail" data-streaming-tail></div></div></main>
      <main data-main="empty"><div class="empty-shell">new session</div></main>
      <div class="prompt-region">
        <div class="slash-pop" hidden></div>
        <div class="attach-chips" hidden></div>
        <textarea class="prompt-textarea"></textarea>
        <button class="stop-btn" hidden>stop</button>
        <button class="send-btn">send</button>
        <button class="attach-btn">attach</button>
        <input data-file-input type="file" />
      </div>
    </pi-app>
  `;
}

export async function connectPiApp() {
  const app = document.querySelector("pi-app");
  await customElements.whenDefined("pi-app");
  app.connectedCallback();
  return app;
}

export function cleanupPiAppFixture() {
  document.querySelectorAll("pi-app").forEach((app) => app.remove());
  localStorage?.clear?.();
  vi.restoreAllMocks();
  delete globalThis.PI_WEB_API_BASE;
  globalThis.fetch = nativeFetch;
  document.body.innerHTML = "";
}
