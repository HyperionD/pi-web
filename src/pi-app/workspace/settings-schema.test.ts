import { describe, expect, it } from "vitest";
import { SETTINGS_FIELDS, parseSettingsPatch, parseWorkspaceSettings } from "./settings-schema";

describe("settings schema", () => {
  it("does not expose removed version, notification, or speech settings", () => {
    const paths = SETTINGS_FIELDS.map((field) => field.path);

    expect(paths).not.toContain("readResponsesAloud");
    expect(paths).not.toContain("voice.language");
    expect(paths).not.toContain("enableSpeechInput");
    expect(paths).not.toContain("speechInput.language");
    expect(paths.some((path) => path.startsWith("remoteNotifications."))).toBe(false);
  });

  it("provides the remaining pi-web settings", () => {
    expect(SETTINGS_FIELDS).toEqual(expect.arrayContaining([
      { path: "defaultProvider", label: "默认提供商", type: "providerSelect", hint: "提供商 ID" },
      { path: "defaultModel", label: "默认模型", type: "modelSelect", hint: "模型 ID 或提供商/模型" },
      { path: "defaultThinkingLevel", label: "思考级别", type: "select", values: ["off", "minimal", "low", "medium", "high", "xhigh", "max"] },
      { path: "compaction.enabled", label: "自动压缩", type: "boolean" },
      { path: "enableSkillCommands", label: "技能命令", type: "boolean" },
      { path: "steeringMode", label: "引导模式", type: "select", values: ["one-at-a-time", "all"] },
      { path: "followUpMode", label: "后续模式", type: "select", values: ["one-at-a-time", "all"] },
      { path: "hideThinkingBlock", label: "隐藏思考内容", type: "boolean" },
      { path: "warnings.anthropicExtraUsage", label: "Anthropic 额外用量警告", type: "boolean" },
    ]));
  });

  it("accepts valid workspace settings responses with unknown custom keys", () => {
    expect(parseWorkspaceSettings({
      global: { theme: "dark", customSetting: "keep" },
      project: { terminal: { showImages: true } },
      effective: { transport: "auto", customSetting: "keep" },
      personalGlobal: { defaultModel: "personal" },
      personalProject: {},
      paths: { global: "/home/me/.pi/agent/settings.json", project: "/repo/.pi/settings.json", personalGlobal: "database", personalProject: "database" },
    })).toMatchObject({
      global: { customSetting: "keep" },
      effective: { transport: "auto", customSetting: "keep" },
    });
  });

  it("rejects malformed model selection state", () => {
    expect(() => parseWorkspaceSettings({ global: {}, project: {}, effective: {}, paths: {},
      modelSelection: { source: "unknown", invalidPersonal: "false" },
    })).toThrow("invalid settings response");
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
      terminal: { imageWidthCells: 120, showImages: false },
    })).toMatchObject({
      transport: "sse",
      terminal: { showImages: false },
    });

    expect(() => parseSettingsPatch({ defaultThinkingLevel: "explode" })).toThrow("invalid settings patch");
    expect(() => parseSettingsPatch({ doubleEscapeAction: "explode" })).toThrow("invalid settings patch");
  });
});
