// @ts-nocheck
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { UI_LOCALE_STORAGE_KEY } from "../../i18n/locales";
import { PROMPT_DRAFT_STORAGE_KEY } from "../constants";
import { cleanupPiAppFixture, connectPiApp, installPiAppFixture } from "../test-helper";


describe("pi-app controls", () => {
  beforeEach(installPiAppFixture);
  afterEach(cleanupPiAppFixture);

  it("applies UI language selection immediately", async () => {
    const app = await connectPiApp();
    const selector = app.querySelector("[data-ui-language]");

    selector.value = "zh-CN";
    selector.dispatchEvent(new Event("change"));

    expect(document.documentElement.lang).toBe("zh-CN");
    expect(app.querySelector("[data-i18n='settingsTitle']").textContent).toBe("pi 设置");
    expect(localStorage.getItem("pi-web.uiLocale")).toBe("zh-CN");

    selector.value = "unsupported";
    selector.dispatchEvent(new Event("change"));
    expect(localStorage.getItem("pi-web.uiLocale")).toBe("zh-CN");
  });

  it("enables send and shows slash commands as the prompt changes", async () => {
    const app = await connectPiApp();
    app.dataset.activeWorkspaceId = "w1";
    app.updatePrompt();
    const prompt = app.querySelector(".prompt-textarea");
    prompt.value = "/mo";
    prompt.dispatchEvent(new Event("input"));
    expect(app.querySelector(".send-btn").disabled).toBe(false);
    expect(app.querySelector(".slash-pop").hidden).toBe(false);
  });

  it("refreshes stale slash commands when the palette opens", async () => {
    const app = await connectPiApp();
    app.apiConnected = true;
    app.dataset.activeWorkspaceId = "w1";
    app.dataset.activeSessionId = "s1";
    app.slashCommandsLoadedAt = Date.now() - 31_000;
    app.loadWorkspaceCommands = vi.fn();

    app.prompt.value = "/";
    app.updatePrompt();

    expect(app.loadWorkspaceCommands).toHaveBeenCalledWith("w1");
  });

  it("closes slash suggestions after command arguments begin", async () => {
    const app = await connectPiApp();
    app.dataset.activeWorkspaceId = "w1";
    const prompt = app.querySelector(".prompt-textarea");

    prompt.value = "/name my session";
    prompt.dispatchEvent(new Event("input"));

    expect(app.querySelector(".slash-pop").hidden).toBe(true);
  });

  it("disables prompt input until a workspace is opened", async () => {
    const app = await connectPiApp();
    const prompt = app.querySelector(".prompt-textarea");
    const send = app.querySelector(".send-btn");

    expect(prompt.disabled).toBe(true);
    expect(send.disabled).toBe(true);

    app.dataset.activeWorkspaceId = "w1";
    app.updatePrompt();
    expect(prompt.disabled).toBe(false);
    expect(send.disabled).toBe(true);

    prompt.value = "hello";
    app.updatePrompt();
    expect(send.disabled).toBe(false);

    app.dataset.activeWorkspaceId = "";
    app.updatePrompt();
    expect(prompt.disabled).toBe(true);
    expect(send.disabled).toBe(true);
  });

  it("restores unsent prompt text and clears the draft on send", async () => {
    const app = await connectPiApp();
    app.dataset.activeWorkspaceId = "w1";
    app.updatePrompt();
    const prompt = app.querySelector(".prompt-textarea");
    localStorage.setItem(PROMPT_DRAFT_STORAGE_KEY, "saved draft");
    app.restorePromptDraft();
    expect(prompt.value).toBe("saved draft");

    prompt.value = "new draft";
    prompt.dispatchEvent(new Event("input"));
    expect(localStorage.getItem(PROMPT_DRAFT_STORAGE_KEY)).toBe("new draft");

    prompt.value = "";
    prompt.dispatchEvent(new Event("input"));
    expect(localStorage.getItem(PROMPT_DRAFT_STORAGE_KEY)).toBeNull();

    prompt.value = "send me";
    prompt.dispatchEvent(new Event("input"));
    app.apiConnected = false;
    app.sendButton.click();
    expect(localStorage.getItem(PROMPT_DRAFT_STORAGE_KEY)).toBeNull();

    app.prompt = null;
    expect(() => app.restorePromptDraft()).not.toThrow();
    expect(() => app.savePromptDraft()).not.toThrow();
  });

  it("keeps send disabled until the current prompt state is refreshed", async () => {
    const app = await connectPiApp();
    app.dataset.activeWorkspaceId = "w1";
    app.updatePrompt();
    app.apiConnected = false;
    app.prompt.value = "send without input event";

    expect(app.sendButton.disabled).toBe(true);
    app.updatePrompt();
    app.sendButton.click();

    expect(app.prompt.value).toBe("");
    expect(app.sendButton.disabled).toBe(true);
    expect(app.sendButton.getAttribute("aria-disabled")).toBe("true");
  });

  it("keeps send and stop as separate running controls", async () => {
    const app = await connectPiApp();
    app.dataset.activeWorkspaceId = "w1";
    app.updatePrompt();
    const send = app.querySelector(".send-btn");
    const stop = app.querySelector(".stop-btn");

    app.setMode("running");
    expect(stop.hidden).toBe(false);
    expect(send.textContent).toBe("send");
    expect(send.disabled).toBe(true);
    expect(send.getAttribute("aria-disabled")).toBe("true");

    app.prompt.value = "one more thing";
    app.updatePrompt();
    expect(send.getAttribute("aria-disabled")).toBe("false");

    app.setMode("idle");
    expect(stop.hidden).toBe(true);
  });

  it("orders matching commands across source groups by match quality", async () => {
    const app = await connectPiApp();
    app.renderSlashCommands([
      { command: "/team", description: "model helper", source: "extension" },
      { command: "/model", description: "change model", source: "prompt" },
    ]);
    app.prompt.value = "/model";
    app.updatePrompt();

    expect([...app.querySelectorAll(".slash-item:not([hidden])")].map((item) => item.dataset.slash)).toEqual([
      "/model", "/team",
    ]);
  });

  it("orders visible slash commands by match quality", async () => {
    const app = await connectPiApp();
    app.renderSlashCommands([
      { command: "/team", description: "model helper", source: "prompt" },
      { command: "/model", description: "change model", source: "prompt" },
    ]);
    app.prompt.value = "/model";
    app.updatePrompt();

    expect([...app.querySelectorAll(".slash-item:not([hidden])")].map((item) => item.dataset.slash)).toEqual([
      "/model", "/team",
    ]);
  });

  it("hides unavailable built-in commands while a session is running", async () => {
    const app = await connectPiApp();
    app.running = true;
    app.renderSlashCommands([]);
    app.prompt.value = "/";
    app.updatePrompt();

    expect(app.querySelector("[data-slash='/compact']").hidden).toBe(true);
    expect(app.querySelector("[data-slash='/session']").hidden).toBe(false);
    expect(app.querySelector("[data-slash='/copy']").hidden).toBe(false);
  });

  it("shows an empty state when no slash command matches", async () => {
    const app = await connectPiApp();
    app.renderSlashCommands([{ command: "/review", description: "review", source: "prompt" }]);
    app.prompt.value = "/unknown";
    app.updatePrompt();

    expect(app.querySelector(".slash-empty").hidden).toBe(false);
  });

  it("groups slash commands by source", async () => {
    const app = await connectPiApp();
    app.renderSlashCommands([
      { command: "/review", description: "review", source: "prompt", scope: "project" },
      { command: "/team", description: "team", source: "extension", scope: "global" },
    ]);

    expect([...app.querySelectorAll(".slash-group")].map((group) => group.dataset.source)).toEqual([
      "builtin", "extension", "prompt",
    ]);
    expect(app.querySelector(".slash-group[data-source='extension'] .slash-group-count").textContent).toBe("1");
  });

  it("renders global and project slash commands", async () => {
    const app = await connectPiApp();
    app.renderSlashCommands([
      { command: "/team", description: "global team", source: "extension", scope: "global" },
      { command: "/review", description: "project review", source: "prompt", scope: "project" },
    ]);
    const items = [...app.querySelectorAll(".slash-item")];
    expect(items.map((item) => item.dataset.slash)).toEqual([
      "/compact", "/reload", "/name", "/session", "/copy", "/clone", "/team", "/review",
    ]);
    expect(items.find((item) => item.dataset.slash === "/team").querySelector(".sl-scope").textContent).toBe("global");
    expect(items.find((item) => item.dataset.slash === "/review").querySelector(".sl-scope").textContent).toBe("project");
  });

  it("renders the compact prompt status line from runtime status", async () => {
    const app = await connectPiApp();
    expect(app.querySelector("[data-prompt-meta]").textContent).toBe("— | —");
    app.updatePromptMeta({
      model: "GPT-5.5",
      thinkingLevel: "high",
      fiveHourQuota: 84,
      weeklyQuota: 14,
      currentBranch: "feature/ui",
    });
    expect(app.querySelector("[data-prompt-meta]").textContent).toBe(
      "GPT-5.5 (high) | 5h (84%) | Week (14%) | feature/ui",
    );
    expect(app.querySelector(".prompt-meta-battery-full svg")).not.toBeNull();
    expect(app.querySelector(".prompt-meta-battery-low svg")).not.toBeNull();
    expect(app.querySelector(".prompt-meta-branch svg")).not.toBeNull();
    app.updatePromptMeta({ fiveHourQuota: 20, weeklyQuota: 21 });
    expect(app.querySelector("[data-prompt-meta]").textContent).toBe(
      "GPT-5.5 (high) | 5h (20%) | Week (21%) | feature/ui",
    );
    expect(app.querySelector(".prompt-meta-battery-low svg")).not.toBeNull();
    app.runtimeStatus = {};
    app.updatePromptMeta({ model: "Claude", thinkingLevel: "off", currentBranch: "main" });
    expect(app.querySelector("[data-prompt-meta]").textContent).toBe("Claude (off) | main");
  });

  it("opens and saves workspace settings from the settings modal", async () => {
    globalThis.PI_WEB_API_BASE = "http://backend.test";
    globalThis.fetch = vi.fn(async (url, options = {}) => ({
      ok: true,
      status: 200,
      statusText: "OK",
      json: async () => {
        if (String(url).endsWith("/auth/providers")) {
          return { providers: [{ id: "anthropic", name: "Anthropic", configured: false }], path: "/home/me/.pi/agent/auth.json" };
        }
        if (String(url).endsWith("/auth/oauth/providers")) {
          return { providers: [{ id: "openai-codex", name: "ChatGPT Plus/Pro", configured: false }] };
        }
        if (String(url).endsWith("/models")) {
          return { providers: [{ id: "zai", models: [{ id: "gpt-5.5", provider: "zai" }] }] };
        }
        if (String(url).endsWith("/auth/oauth/start")) {
          return { session: { id: "oauth-1", provider: "openai-codex", status: "waiting", authUrl: "https://login.test", progress: [] } };
        }
        if (String(url).endsWith("/auth/oauth/sessions/oauth-1/input")) {
          return { session: { id: "oauth-1", provider: "openai-codex", status: "success", progress: [] } };
        }
        if (String(url).endsWith("/auth/oauth/sessions/oauth-1")) {
          return { session: { id: "oauth-1", provider: "openai-codex", status: "success", progress: [] } };
        }
        if (String(url).endsWith("/auth/api-key")) {
          return { provider: { id: "anthropic", name: "Anthropic", configured: true, source: "api_key" } };
        }
        if (String(url).endsWith("/settings") && options.method === "PUT") {
          return { settings: { project: JSON.parse(options.body).settings, effective: {}, paths: {} } };
        }
        if (String(url).endsWith("/settings")) {
          return {
            settings: {
              global: { theme: "dark" },
              project: { defaultProvider: "zai", defaultModel: "gpt-5.5" },
              effective: {
                theme: "dark",
                defaultProvider: "zai",
                defaultModel: "gpt-5.5",
                compaction: { enabled: true },
              },
              paths: { project: "/demo/.pi/settings.json", global: "/home/me/.pi/agent/settings.json" },
            },
          };
        }
        return { workspaces: [] };
      },
    }));
    const app = await connectPiApp();
    app.apiConnected = true;
    app.dataset.activeWorkspaceId = "w1";

    await app.openSettingsModal();
    expect(app.querySelector("[data-settings-modal]").hidden).toBe(false);
    expect(app.querySelector("[data-settings-path]").textContent).toBe("/demo/.pi/settings.json");
    expect(app.querySelector("[data-setting='defaultProvider']").value).toBe("zai");
    expect(app.querySelector("[data-setting='defaultModel']").value).toBe("gpt-5.5");
    expect(app.querySelector("[data-setting='theme']")).toBeNull();
    expect(app.querySelector("[data-setting='transport']")).toBeNull();
    expect(app.querySelector("[data-auth-provider]").value).toBe("anthropic");
    expect(app.querySelector("[data-oauth-provider]").value).toBe("openai-codex");

    await app.startOAuthLogin();
    expect(app.querySelector("[data-oauth-link]").href).toBe("https://login.test/");
    app.querySelector("[data-oauth-input]").value = "code";
    await app.sendOAuthInput();
    expect(app.querySelector("[data-oauth-status]").textContent).toBe("OAuth login saved");

    app.querySelector("[data-auth-api-key]").value = "sk-test";
    await app.saveAuthForm(new Event("submit"));
    const authCall = globalThis.fetch.mock.calls.find(([url]) => String(url).endsWith("/auth/api-key"));
    expect(JSON.parse(authCall[1].body)).toEqual({ provider: "anthropic", apiKey: "sk-test" });
    expect(app.querySelector("[data-auth-api-key]").value).toBe("");

    app.querySelector("[data-account-model]").value = JSON.stringify(["zai", "gpt-5.5"]);
    app.querySelector("[data-setting='compaction.enabled']").value = "false";
    await app.saveSettingsForm(new Event("submit"));
    const putCalls = globalThis.fetch.mock.calls.filter(([, options]) => options?.method === "PUT");
    expect(putCalls.map(([, options]) => JSON.parse(options.body))).toEqual(expect.arrayContaining([
      expect.objectContaining({ scope: "project", settings: { compaction: { enabled: false } } }),
      expect.objectContaining({ scope: "personal", location: "global", settings: {
        defaultProvider: "zai", defaultModel: "gpt-5.5",
      } }),
    ]));
  });

  it("lets regular users save model settings in their personal scope", async () => {
    globalThis.PI_WEB_API_BASE = "http://backend.test";
    globalThis.fetch = vi.fn(async (url, options = {}) => ({
      ok: true,
      status: 200,
      statusText: "OK",
      json: async () => {
        if (String(url).endsWith("/settings") && options.method === "PUT") {
          return { settings: { global: {}, project: {}, personalGlobal: JSON.parse(options.body).settings, personalProject: {}, effective: JSON.parse(options.body).settings, paths: {} } };
        }
        if (String(url).endsWith("/settings")) {
          return { settings: { global: {}, project: {}, personalGlobal: {}, personalProject: {}, effective: {}, paths: {} } };
        }
        if (String(url).endsWith("/models")) return { providers: [{ id: "zai", models: [{ id: "gpt-5.5", provider: "zai" }] }] };
        return { providers: [] };
      },
    }));
    const app = await connectPiApp();
    document.body.dataset.accountRole = "user";
    app.apiConnected = true;
    app.dataset.activeWorkspaceId = "w1";

    await app.openSettingsModal();
    expect(app.querySelector("[data-settings-scope]").value).toBe("personal");
    expect(globalThis.fetch.mock.calls.some(([url]) => String(url).endsWith("/auth/providers"))).toBe(false);
    expect(globalThis.fetch.mock.calls.some(([url]) => String(url).endsWith("/auth/oauth/providers"))).toBe(false);
    app.querySelector("[data-account-model]").value = JSON.stringify(["zai", "gpt-5.5"]);
    await app.saveSettingsForm(new Event("submit"));

    const putCall = globalThis.fetch.mock.calls.find(([, options]) => options?.method === "PUT");
    expect(JSON.parse(putCall[1].body)).toMatchObject({
      scope: "personal",
      location: "global",
      settings: { defaultProvider: "zai", defaultModel: "gpt-5.5" },
    });
  });

  it("sets app height from the visual viewport for mobile browser chrome", async () => {
    document.body.innerHTML = "";
    Object.defineProperty(window, "visualViewport", {
      configurable: true,
      value: {
        height: 567,
        addEventListener: vi.fn(),
        removeEventListener: vi.fn(),
      },
    });
    installPiAppFixture();
    const app = await connectPiApp();
    expect(app.style.getPropertyValue("--app-viewport-height")).toBe("567px");
  });

  it("toggles the mobile sidebar drawer from the hamburger button", async () => {
    const app = await connectPiApp();
    const hamburger = app.querySelector(".hamburger");
    const body = app.querySelector(".app-body");
    expect(hamburger.getAttribute("aria-expanded")).toBe("false");
    hamburger.click();
    expect(body.classList.contains("drawer-open")).toBe(true);
    expect(hamburger.getAttribute("aria-expanded")).toBe("true");
    expect(hamburger.getAttribute("aria-label")).toBe("关闭侧栏");
    hamburger.click();
    expect(body.classList.contains("drawer-open")).toBe(false);
    expect(hamburger.getAttribute("aria-expanded")).toBe("false");
    expect(hamburger.getAttribute("aria-label")).toBe("打开侧栏");
  });

  it("closes the drawer on Escape before cancelling a running session", async () => {
    const app = await connectPiApp();
    const body = app.querySelector(".app-body");
    const hamburger = app.querySelector(".hamburger");
    const focusTarget = document.createElement("button");
    app.querySelector(".sidebar").append(focusTarget);
    const cancel = vi.fn();
    app.cancelActiveSession = cancel;
    app.running = true;
    app.toggleDrawer(true);
    focusTarget.focus();

    const escape = new KeyboardEvent("keydown", { key: "Escape", cancelable: true });
    window.dispatchEvent(escape);

    expect(escape.defaultPrevented).toBe(true);
    expect(body.classList.contains("drawer-open")).toBe(false);
    expect(cancel).not.toHaveBeenCalled();
    expect(document.activeElement).toBe(hamburger);
    expect(hamburger.getAttribute("aria-expanded")).toBe("false");
  });

  it("renders a terminal dot spinner and advances its frame", async () => {
    const app = await connectPiApp();
    app.renderMessages([]);
    app.appendLoadingMessage();
    const spinner = app.querySelector(".spinner");
    expect(spinner.children).toHaveLength(6);
    app.tickSpinners();
    expect(spinner.dataset.frame).toBe("1");
  });

  it("keeps the main workspace route when picker navigation is requested", async () => {
    const app = await connectPiApp();
    app.route("picker");

    expect(app.dataset.route).toBe("workspace");
    expect(app.querySelector('[data-view="picker"]')).toBeNull();
    expect(app.querySelector('[data-view="workspace"]').hidden).toBe(false);
  });

  it("keeps the file tree visible when clicking outside it", async () => {
    const app = await connectPiApp();
    const body = app.querySelector(".app-body");
    const tree = app.querySelector(".tree");
    const main = app.querySelector("main");

    app.toggleTree(true);
    tree.click();
    expect(app.dataset.tree).toBe("on");

    main.click();
    expect(app.dataset.tree).toBe("on");
    expect(tree.hidden).toBe(false);
  });

  it("keeps workspace and branch details out of the file tree header", async () => {
    const app = await connectPiApp();

    expect(app.querySelector(".tree-title")).toBeNull();
    expect(app.querySelector(".tree-head [data-git-status]")).toBeNull();
  });

  it("keeps the file tree open when an inside click removes its target", async () => {
    const app = await connectPiApp();
    const tree = app.querySelector(".tree");
    const removable = document.createElement("button");
    removable.type = "button";
    removable.addEventListener("click", () => removable.remove());
    tree.append(removable);

    app.toggleTree(true);
    removable.click();

    expect(app.dataset.tree).toBe("on");
    expect(tree.hidden).toBe(false);
  });

  it("localizes the file preview loading placeholder and updates it after a language change", async () => {
    localStorage.setItem(UI_LOCALE_STORAGE_KEY, "en");
    const app = await connectPiApp();
    app.renderFilePreview({ path: "README.md", previewKind: "loading" });

    const label = app.querySelector("[data-file-preview] .fp-body [data-i18n='loadingPreview']");
    expect(label?.textContent).toBe("Loading preview…");

    const selector = app.querySelector("[data-ui-language]");
    selector.value = "zh-CN";
    selector.dispatchEvent(new Event("change", { bubbles: true }));
    expect(label?.textContent).toBe("正在加载预览…");
  });

  it("keeps the file tree in the sidebar and opens file content in a modal", async () => {
    const app = await connectPiApp();
    const body = app.querySelector(".app-body");
    const tree = app.querySelector(".tree");
    const preview = app.querySelector("[data-file-preview]");

    expect(tree.closest(".sidebar")).not.toBeNull();
    expect(preview.hidden).toBe(true);
    expect(app.dataset.tree).toBe("on");
    expect(body.style.gridTemplateColumns).toBe("286px 1fr");

    app.renderFilePreview({ path: "README.md", previewKind: "loading" });
    expect(preview.hidden).toBe(false);
    expect(app.dataset.preview).toBe("on");
    expect(body.style.gridTemplateColumns).toBe("286px 1fr");

    app.closeFilePreview();
    expect(preview.hidden).toBe(true);
    expect(app.dataset.preview).toBe("off");
    expect(body.style.gridTemplateColumns).toBe("286px 1fr");
  });

  it("keeps the sidebar visible when Git details open as a modal", async () => {
    const app = await connectPiApp();
    const body = app.querySelector(".app-body");

    app.toggleGitDetailPanel(true);
    expect(body.style.gridTemplateColumns).toBe("286px 1fr");
    expect(app.querySelector(".sidebar-wrap").hidden).toBe(false);
    expect(app.dataset.sidebar).toBe("open");
  });

  it("refreshes workspaces from the sidebar refresh button", async () => {
    globalThis.PI_WEB_API_BASE = "http://backend.test";
    globalThis.fetch = vi.fn(async () => ({
      ok: true,
      status: 200,
      statusText: "OK",
      json: async () => ({
        workspaces: [{ id: "w1", name: "demo", path: "/demo", sessionCount: 0, sessions: [] }],
      }),
    }));
    const app = await connectPiApp();
    app.apiConnected = true;

    await app.refreshWorkspaces();

    expect(globalThis.fetch).toHaveBeenCalledWith("http://backend.test/api/workspaces", expect.any(Object));
    expect(app.querySelector("[data-workspace-group='w1'] .label").textContent).toBe("demo");
  });

  it("only switches the active session when explicitly requested", async () => {
    globalThis.PI_WEB_API_BASE = "http://backend.test";
    globalThis.fetch = vi.fn(async (url, options = {}) => {
      const path = String(url);
      if (path.endsWith("/models")) {
        return { ok: true, json: async () => ({ providers: [{ id: "openai-codex", models: [{ id: "gpt-5.6-luna", provider: "openai-codex" }] }] }) };
      }
      if (path.endsWith("/settings") && options.method === "PUT") {
        const body = JSON.parse(options.body);
        return { ok: true, json: async () => ({ settings: { global: {}, project: {}, personalGlobal: body.settings, effective: { ...body.settings }, paths: {} } }) };
      }
      if (path.endsWith("/settings")) {
        return { ok: true, json: async () => ({ settings: { global: {}, project: {}, effective: {}, paths: {} } }) };
      }
      if (path.endsWith("/auth/providers")) return { ok: true, json: async () => ({ providers: [] }) };
      if (path.endsWith("/auth/oauth/providers")) return { ok: true, json: async () => ({ providers: [] }) };
      return { ok: true, json: async () => ({ workspaces: [] }) };
    });
    const app = await connectPiApp();
    app.apiConnected = true;
    app.dataset.activeWorkspaceId = "w1";
    app.dataset.activeSessionId = "s1";

    await app.openSettingsModal();
    app.querySelector("[data-account-model]").value = JSON.stringify(["openai-codex", "gpt-5.6-luna"]);
    await app.saveSettingsForm(new Event("submit"));
    expect(globalThis.fetch.mock.calls.some(([url]) => String(url).endsWith("/model"))).toBe(false);
    await app.applyEffectiveModelToActiveSession();

    const modelCall = globalThis.fetch.mock.calls.find(([url, options]) => String(url).endsWith("/model") && options?.method === "POST");
    expect(modelCall).toBeTruthy();
    expect(JSON.parse(modelCall[1].body)).toEqual({ provider: "openai-codex", model: "gpt-5.6-luna" });
  });
});
