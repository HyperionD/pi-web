import { describe, expect, it } from "vitest";
import { SETTINGS_FIELDS, SPEECH_LANGUAGE_OPTIONS, parseSettingsPatch, parseWorkspaceSettings } from "./settings-schema";

describe("settings schema", () => {
  it("presents the Korean speech option with a Chinese label", () => {
    expect(SPEECH_LANGUAGE_OPTIONS.find((option) => option.value === "ko")?.label).toBe("韩语");
  });

  it("provides Simplified Chinese labels and hints for every settings field", () => {
    expect(SETTINGS_FIELDS).toEqual(expect.arrayContaining([
      { path: "defaultProvider", label: "默认提供商", type: "providerSelect", hint: "提供商 ID" },
      { path: "defaultModel", label: "默认模型", type: "modelSelect", hint: "模型 ID 或提供商/模型" },
      { path: "defaultThinkingLevel", label: "思考级别", type: "select", values: ["off", "minimal", "low", "medium", "high", "xhigh"] },
      { path: "compaction.enabled", label: "自动压缩", type: "boolean" },
      { path: "enableSkillCommands", label: "技能命令", type: "boolean" },
      { path: "steeringMode", label: "引导模式", type: "select", values: ["one-at-a-time", "all"] },
      { path: "followUpMode", label: "后续模式", type: "select", values: ["one-at-a-time", "all"] },
      { path: "hideThinkingBlock", label: "隐藏思考内容", type: "boolean" },
      { path: "readResponsesAloud", label: "朗读回答", type: "checkbox" },
      { path: "voice.language", label: "朗读语言", type: "speechLanguage" },
      { path: "enableSpeechInput", label: "语音输入", type: "checkbox" },
      { path: "speechInput.language", label: "语音语言", type: "speechLanguage" },
      { path: "remoteNotifications.discord.enabled", label: "启用 Discord", type: "checkbox" },
      { path: "remoteNotifications.discord.token", label: "Discord 令牌", type: "password", hint: "机器人令牌" },
      { path: "remoteNotifications.discord.channelId", label: "Discord 频道 ID", type: "text", hint: "频道 ID" },
      { path: "remoteNotifications.telegram.enabled", label: "启用 Telegram", type: "checkbox" },
      { path: "remoteNotifications.telegram.token", label: "Telegram 机器人令牌", type: "password", hint: "BotFather 令牌" },
      { path: "remoteNotifications.telegram.chatId", label: "Telegram 聊天 ID", type: "text", hint: "chat_id" },
      { path: "warnings.anthropicExtraUsage", label: "Anthropic 额外用量警告", type: "boolean" },
    ]));
  });

  it("shows only settings relevant to pi-web", () => {
    const paths = SETTINGS_FIELDS.map((field) => field.path);

    expect(paths).toContain("defaultModel");
    expect(paths).toContain("compaction.enabled");
    expect(paths).toContain("enableSkillCommands");
    expect(paths).toContain("readResponsesAloud");
    expect(paths).toContain("voice.language");
    expect(paths).toContain("enableSpeechInput");
    expect(paths).toContain("speechInput.language");
    expect(paths).toContain("remoteNotifications.discord.enabled");
    expect(paths).toContain("remoteNotifications.discord.token");
    expect(paths).toContain("remoteNotifications.discord.channelId");
    expect(paths).toContain("remoteNotifications.telegram.enabled");
    expect(paths).toContain("remoteNotifications.telegram.token");
    expect(paths).toContain("remoteNotifications.telegram.chatId");
    expect(paths).not.toContain("voice.engine");
    expect(paths).not.toContain("theme");
    expect(paths).not.toContain("terminal.imageWidthCells");
    expect(paths).not.toContain("transport");
    expect(paths).not.toContain("doubleEscapeAction");
  });

  it("accepts valid workspace settings responses with unknown custom keys", () => {
    expect(parseWorkspaceSettings({
      global: { theme: "dark", customSetting: "keep" },
      project: { terminal: { showImages: true } },
      effective: {
        transport: "auto",
        readResponsesAloud: true,
        voice: { language: "ko-KR" },
        enableSpeechInput: true,
        speechInput: { language: "ko-KR", useLocalWhisper: true, whisperModel: "large-v3" },
        remoteNotifications: {
          discord: { enabled: true, token: "bot-token", channelId: "123" },
          telegram: { enabled: true, token: "telegram-token", chatId: "456" },
        },
        terminal: { imageWidthCells: 80 },
      },
      paths: { global: "/home/me/.pi/agent/settings.json", project: "/repo/.pi/settings.json" },
    })).toMatchObject({
      global: { customSetting: "keep" },
      effective: {
        transport: "auto",
        readResponsesAloud: true,
        voice: { language: "ko-KR" },
        enableSpeechInput: true,
        speechInput: { language: "ko-KR", useLocalWhisper: true, whisperModel: "large-v3" },
        remoteNotifications: {
          discord: { enabled: true, token: "bot-token", channelId: "123" },
          telegram: { enabled: true, token: "telegram-token", chatId: "456" },
        },
      },
    });
  });

  it("rejects invalid workspace settings responses", () => {
    expect(() => parseWorkspaceSettings({
      global: {},
      project: {},
      effective: { transport: "ftp" },
      paths: {},
    })).toThrow("invalid settings response");
    expect(() => parseWorkspaceSettings(null)).toThrow("value:");
  });

  it("accepts nullable form patches and rejects invalid enum values", () => {
    expect(parseSettingsPatch({
      defaultProvider: null,
      transport: "sse",
      readResponsesAloud: true,
      voice: { language: "ja-JP" },
      enableSpeechInput: true,
      speechInput: { language: "ja-JP", useLocalWhisper: true, whisperModel: "large-v3-q5" },
      remoteNotifications: {
        discord: { enabled: true, token: "bot-token", channelId: "123" },
        telegram: { enabled: true, token: "telegram-token", chatId: "456" },
      },
      terminal: { imageWidthCells: 120, showImages: false },
    })).toMatchObject({
      transport: "sse",
      remoteNotifications: {
        discord: { enabled: true, channelId: "123" },
        telegram: { enabled: true, chatId: "456" },
      },
      terminal: { showImages: false },
    });

    expect(() => parseSettingsPatch({ speechInput: { whisperModel: "huge" } })).toThrow("invalid settings patch");
    expect(() => parseSettingsPatch({ doubleEscapeAction: "explode" })).toThrow("invalid settings patch");
  });
});
