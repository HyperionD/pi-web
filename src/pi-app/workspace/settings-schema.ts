import { z } from "zod";

export const SETTINGS_FIELDS = [
  { path: "defaultProvider", label: "默认提供商", type: "providerSelect", hint: "提供商 ID" },
  { path: "defaultModel", label: "默认模型", type: "modelSelect", hint: "模型 ID 或提供商/模型" },
  { path: "defaultThinkingLevel", label: "思考级别", type: "select", values: ["off", "minimal", "low", "medium", "high", "xhigh", "max"] },
  { path: "compaction.enabled", label: "自动压缩", type: "boolean" },
  { path: "enableSkillCommands", label: "技能命令", type: "boolean" },
  { path: "steeringMode", label: "引导模式", type: "select", values: ["one-at-a-time", "all"] },
  { path: "followUpMode", label: "后续模式", type: "select", values: ["one-at-a-time", "all"] },
  { path: "hideThinkingBlock", label: "隐藏思考内容", type: "boolean" },
  { path: "warnings.anthropicExtraUsage", label: "Anthropic 额外用量警告", type: "boolean" },
] as const;

export const settingsScopeSchema = z.enum(["global", "project", "personal"]);

export const MODEL_SETTING_PATHS = ["defaultProvider", "defaultModel", "defaultThinkingLevel"] as const;

const thinkingLevelSchema = z.enum(["off", "minimal", "low", "medium", "high", "xhigh", "max"]);
const steeringModeSchema = z.enum(["one-at-a-time", "all"]);
const transportSchema = z.enum(["sse", "websocket", "websocket-cached", "auto"]);
const doubleEscapeActionSchema = z.enum(["tree", "fork", "none"]);
const treeFilterModeSchema = z.enum(["default", "no-tools", "user-only", "labeled-only", "all"]);
const settingsObjectSchema = z.object({
  defaultProvider: z.string().optional(),
  defaultModel: z.string().optional(),
  defaultThinkingLevel: thinkingLevelSchema.optional(),
  theme: z.string().optional(),
  compaction: z.object({ enabled: z.boolean().optional() }).partial().passthrough().optional(),
  terminal: z.object({
    showImages: z.boolean().optional(),
    imageWidthCells: z.number().optional(),
    clearOnShrink: z.boolean().optional(),
    showTerminalProgress: z.boolean().optional(),
  }).partial().passthrough().optional(),
  images: z.object({
    autoResize: z.boolean().optional(),
    blockImages: z.boolean().optional(),
  }).partial().passthrough().optional(),
  enableSkillCommands: z.boolean().optional(),
  showHardwareCursor: z.boolean().optional(),
  editorPaddingX: z.number().optional(),
  autocompleteMaxVisible: z.number().optional(),
  steeringMode: steeringModeSchema.optional(),
  followUpMode: steeringModeSchema.optional(),
  transport: transportSchema.optional(),
  hideThinkingBlock: z.boolean().optional(),
  collapseChangelog: z.boolean().optional(),
  quietStartup: z.boolean().optional(),
  enableInstallTelemetry: z.boolean().optional(),
  doubleEscapeAction: doubleEscapeActionSchema.optional(),
  treeFilterMode: treeFilterModeSchema.optional(),
  warnings: z.object({ anthropicExtraUsage: z.boolean().optional() }).partial().passthrough().optional(),
}).partial().passthrough();

const nullishString = z.string().nullable().optional();
const nullishBoolean = z.boolean().nullable().optional();
const nullishNumber = z.number().nullable().optional();

export const settingsPatchSchema = z.object({
  defaultProvider: nullishString,
  defaultModel: nullishString,
  defaultThinkingLevel: thinkingLevelSchema.nullable().optional(),
  theme: nullishString,
  compaction: z.object({ enabled: nullishBoolean }).partial().passthrough().optional(),
  terminal: z.object({
    showImages: nullishBoolean,
    imageWidthCells: nullishNumber,
    clearOnShrink: nullishBoolean,
    showTerminalProgress: nullishBoolean,
  }).partial().passthrough().optional(),
  images: z.object({
    autoResize: nullishBoolean,
    blockImages: nullishBoolean,
  }).partial().passthrough().optional(),
  enableSkillCommands: nullishBoolean,
  showHardwareCursor: nullishBoolean,
  editorPaddingX: nullishNumber,
  autocompleteMaxVisible: nullishNumber,
  steeringMode: steeringModeSchema.nullable().optional(),
  followUpMode: steeringModeSchema.nullable().optional(),
  transport: transportSchema.nullable().optional(),
  hideThinkingBlock: nullishBoolean,
  collapseChangelog: nullishBoolean,
  quietStartup: nullishBoolean,
  enableInstallTelemetry: nullishBoolean,
  doubleEscapeAction: doubleEscapeActionSchema.nullable().optional(),
  treeFilterMode: treeFilterModeSchema.nullable().optional(),
  warnings: z.object({ anthropicExtraUsage: nullishBoolean }).partial().passthrough().optional(),
}).partial().passthrough();

export const workspaceSettingsSchema = z.object({
  global: settingsObjectSchema,
  project: settingsObjectSchema,
  effective: settingsObjectSchema,
  personalGlobal: settingsObjectSchema.optional(),
  personalProject: settingsObjectSchema.optional(),
  paths: z.object({
    global: z.string().optional(),
    project: z.string().optional(),
    personalGlobal: z.string().optional(),
    personalProject: z.string().optional(),
  }).partial().passthrough(),
}).passthrough();

export type SettingsField = (typeof SETTINGS_FIELDS)[number];
export type SettingsPatch = z.infer<typeof settingsPatchSchema>;
export type WorkspaceSettings = z.infer<typeof workspaceSettingsSchema>;

function validationMessage(prefix: string, error: z.ZodError) {
  const details = error.issues.map((issue) => `${issue.path.join(".") || "value"}: ${issue.message}`).join("; ");
  return `${prefix}: ${details}`;
}

export function parseWorkspaceSettings(value: unknown): WorkspaceSettings {
  const result = workspaceSettingsSchema.safeParse(value);
  if (!result.success) throw new Error(validationMessage("invalid settings response", result.error));
  return result.data;
}

export function parseSettingsPatch(value: unknown): SettingsPatch {
  const result = settingsPatchSchema.safeParse(value);
  if (!result.success) throw new Error(validationMessage("invalid settings patch", result.error));
  return result.data;
}
