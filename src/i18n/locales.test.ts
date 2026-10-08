import { describe, expect, it, vi } from "vitest";
import { applyUiLocale, currentUiLocale, saveUiLocale, setUiLocale } from "./client";
import { UI_LOCALES, UI_LOCALE_STORAGE_KEY, browserUiLocale, normalizeUiLocale } from "./locales";
import { UI_MESSAGES, uiMessage } from "./ui";

describe("ui locales", () => {
  it("normalizes supported locales and excludes Korean from the UI", () => {
    expect(normalizeUiLocale("pt-br")).toBe("pt-BR");
    expect(normalizeUiLocale("zh-Hans-CN")).toBe("zh-CN");
    expect(normalizeUiLocale("de-DE")).toBe("de");
    expect(normalizeUiLocale("ko-KR")).toBeUndefined();
    expect(UI_LOCALES.some((locale) => locale.code === ("ko" as never))).toBe(false);
  });

  it("detects the first supported browser locale and defaults to Simplified Chinese", () => {
    expect(browserUiLocale(["it-IT", "ja-JP", "en-US"])).toBe("ja");
    expect(browserUiLocale(["it-IT"])).toBe("zh-CN");
    expect(normalizeUiLocale(null)).toBeUndefined();
  });

  it("returns Chinese settings messages without a Korean UI dictionary", () => {
    expect(uiMessage("zh-CN", "settingsTitle")).toBe("pi 设置");
    expect("ko" in UI_MESSAGES).toBe(false);
    expect(UI_MESSAGES.en.settingsTitle).toBe("pi settings");
  });

  it("updates static i18n text without clobbering dynamic settings status", () => {
    document.body.innerHTML = `
      <main>
        <strong data-i18n="settingsTitle">pi settings</strong>
        <small data-settings-status>saved</small>
        <small>effective: claude/sonnet</small>
      </main>
    `;

    applyUiLocale("zh-CN", document);

    expect(document.querySelector("[data-i18n='settingsTitle']")?.textContent).toBe("pi 设置");
    expect(document.querySelector("[data-settings-status]")?.textContent).toBe("saved");
    expect(document.querySelector("main small:last-child")?.textContent).toBe("effective: claude/sonnet");
  });

  it("updates attributes, selector state, storage, and current locale", () => {
    document.body.innerHTML = `
      <main>
        <select data-ui-language><option value="en">English</option><option value="de">Deutsch</option></select>
        <input data-i18n-placeholder="pasteApiKey" />
        <button data-i18n-aria-label="closeSettings" data-i18n-title="closeSettings"></button>
      </main>
    `;

    setUiLocale("de", document);

    expect(localStorage.getItem(UI_LOCALE_STORAGE_KEY)).toBe("de");
    expect(currentUiLocale()).toBe("de");
    expect(document.documentElement.lang).toBe("de");
    expect(document.querySelector("select")?.value).toBe("de");
    expect(document.querySelector("input")?.getAttribute("placeholder")).toBe("API-Key einfügen");
    expect(document.querySelector("button")?.getAttribute("aria-label")).toBe("Einstellungen schließen");
    expect(document.querySelector("button")?.getAttribute("title")).toBe("Einstellungen schließen");

    applyUiLocale("unsupported" as never, document);
    expect(document.documentElement.lang).toBe("zh-CN");
    setUiLocale("unsupported" as never, document);
    expect(localStorage.getItem(UI_LOCALE_STORAGE_KEY)).toBe("zh-CN");
  });

  it("localizes shared workbench labels and controls in English and Chinese", () => {
    document.body.innerHTML = `
      <nav data-i18n-aria-label="managementTools">
        <span data-i18n="userManagementShort">用户</span>
        <span data-i18n="promptPresetsShort">预设</span>
        <span data-i18n="skillManagerShort">技能</span>
        <span data-i18n="settingsShort">设置</span>
      </nav>
      <details><summary data-i18n="attachmentLimits">附件限制说明</summary></details>
      <button data-i18n-aria-label="addAttachment" data-i18n-title="addAttachment"></button>
      <button data-i18n-aria-label="sendMessage" data-i18n-title="sendMessage">
        <span data-i18n="sendPrompt">发送</span>
      </button>
      <textarea data-i18n-placeholder="promptPlaceholder"></textarea>
      <span data-i18n="thinking">思考过程</span>
      <span data-i18n="running">运行中</span>
      <span data-i18n="executionFailed">执行失败</span>
      <button data-i18n-aria-label="scrollToBottom" data-i18n-title="scrollToBottom"></button>
    `;

    applyUiLocale("en", document);
    expect(document.querySelector("nav").getAttribute("aria-label")).toBe("Management tools");
    expect([...document.querySelectorAll("nav [data-i18n]")].map((label) => label.textContent))
      .toEqual(["Users", "Presets", "Skills", "Settings"]);
    expect(document.querySelector("summary").textContent).toBe("Attachment limits");
    expect(document.querySelector("button").getAttribute("aria-label")).toBe("Add attachment");
    expect(document.querySelector("[data-i18n='sendPrompt']").textContent).toBe("Send");
    expect(document.querySelector("textarea").placeholder).toBe("Type your prompt");
    expect(document.querySelector("[data-i18n='thinking']").textContent).toBe("Thinking");
    expect(document.querySelector("[data-i18n='running']").textContent).toBe("Running");
    expect(document.querySelector("[data-i18n='executionFailed']").textContent).toBe("Execution failed");
    expect(document.querySelectorAll("button")[2].title).toBe("Scroll to bottom");

    applyUiLocale("zh-CN", document);
    expect(document.querySelector("nav").getAttribute("aria-label")).toBe("管理工具");
    expect(document.querySelector("summary").textContent).toBe("附件限制说明");
    expect(document.querySelector("[data-i18n='sendPrompt']").textContent).toBe("发送");
    expect(document.querySelector("textarea").placeholder).toBe("输入你的想法");
    expect(document.querySelector("[data-i18n='executionFailed']").textContent).toBe("执行失败");
  });

  it("applies workbench state messages in every supported locale", () => {
    document.body.innerHTML = `
      <span data-i18n="managementTools"></span>
      <span data-i18n="sessionWaiting"></span>
      <span data-i18n="connectionConnected"></span>
    `;
    const translations = [
      { locale: "en", tools: "Management tools", waiting: "Waiting", connected: "Connected" },
      { locale: "zh-CN", tools: "管理工具", waiting: "等待中", connected: "已连接" },
      { locale: "ja", tools: "管理ツール", waiting: "待機中", connected: "接続済み" },
      {
        locale: "es",
        tools: "Herramientas de gestión",
        waiting: "En espera",
        connected: "Conectado",
      },
      {
        locale: "pt-BR",
        tools: "Ferramentas de gerenciamento",
        waiting: "Aguardando",
        connected: "Conectado",
      },
      { locale: "fr", tools: "Outils de gestion", waiting: "En attente", connected: "Connecté" },
      {
        locale: "ru",
        tools: "Инструменты управления",
        waiting: "Ожидание",
        connected: "Подключено",
      },
      { locale: "de", tools: "Verwaltungswerkzeuge", waiting: "Wartet", connected: "Verbunden" },
    ] as const;

    for (const translation of translations) {
      applyUiLocale(translation.locale, document);
      const labels = [...document.querySelectorAll("span")].map((label) => label.textContent);
      expect(labels).toEqual([translation.tools, translation.waiting, translation.connected]);
    }
  });

  it("falls back when storage is unavailable", () => {
    const originalStorage = globalThis.localStorage;
    Object.defineProperty(globalThis, "localStorage", {
      configurable: true,
      value: {
        getItem: vi.fn(() => { throw new Error("blocked"); }),
        setItem: vi.fn(() => { throw new Error("blocked"); }),
      },
    });

    saveUiLocale("fr");
    expect(currentUiLocale()).toBe(browserUiLocale(navigator.languages));

    Object.defineProperty(globalThis, "localStorage", { configurable: true, value: originalStorage });
  });

  it("falls back to default locale without navigator languages", () => {
    const originalNavigator = globalThis.navigator;
    Object.defineProperty(globalThis, "navigator", { configurable: true, value: undefined });
    localStorage.removeItem(UI_LOCALE_STORAGE_KEY);
    expect(currentUiLocale()).toBe("zh-CN");
    Object.defineProperty(globalThis, "navigator", { configurable: true, value: originalNavigator });
  });

  it("falls back to Chinese messages for missing localized keys", () => {
    const jaMessages = UI_MESSAGES.ja as Record<string, string | undefined>;
    const previous = jaMessages.save;
    delete jaMessages.save;
    expect(uiMessage("ja", "save")).toBe("保存");
    jaMessages.save = previous;
  });
});
