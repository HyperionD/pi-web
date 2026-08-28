import { z } from "zod";

export const SPEECH_LANGUAGE_OPTIONS = [
  { value: "system", label: "System default" },
  { value: "en", label: "English" },
  { value: "zh", label: "中文(Chinese)" },
  { value: "de", label: "Deutsch(German)" },
  { value: "es", label: "Español(Spanish)" },
  { value: "ru", label: "Русский(Russian)" },
  { value: "ko", label: "韩语" },
  { value: "fr", label: "Français(French)" },
  { value: "ja", label: "日本語(Japanese)" },
  { value: "pt", label: "Português(Portuguese)" },
  { value: "tr", label: "Türkçe(Turkish)" },
  { value: "pl", label: "Polski(Polish)" },
  { value: "ca", label: "Català(Catalan)" },
  { value: "nl", label: "Nederlands(Dutch)" },
  { value: "ar", label: "العربية(Arabic)" },
  { value: "sv", label: "Svenska(Swedish)" },
  { value: "it", label: "Italiano(Italian)" },
  { value: "id", label: "Bahasa Indonesia(Indonesian)" },
  { value: "hi", label: "हिन्दी(Hindi)" },
  { value: "fi", label: "Suomi(Finnish)" },
  { value: "vi", label: "Tiếng Việt(Vietnamese)" },
  { value: "he", label: "עברית(Hebrew)" },
  { value: "uk", label: "Українська(Ukrainian)" },
  { value: "el", label: "Ελληνικά(Greek)" },
  { value: "ms", label: "Bahasa Melayu(Malay)" },
  { value: "cs", label: "Čeština(Czech)" },
  { value: "ro", label: "Română(Romanian)" },
  { value: "da", label: "Dansk(Danish)" },
  { value: "hu", label: "Magyar(Hungarian)" },
  { value: "ta", label: "தமிழ்(Tamil)" },
  { value: "no", label: "Norsk(Norwegian)" },
  { value: "th", label: "ไทย(Thai)" },
  { value: "ur", label: "اردو(Urdu)" },
  { value: "hr", label: "Hrvatski(Croatian)" },
  { value: "bg", label: "Български(Bulgarian)" },
  { value: "lt", label: "Lietuvių(Lithuanian)" },
  { value: "la", label: "Latina(Latin)" },
  { value: "mi", label: "Māori(Maori)" },
  { value: "ml", label: "മലയാളം(Malayalam)" },
  { value: "cy", label: "Cymraeg(Welsh)" },
  { value: "sk", label: "Slovenčina(Slovak)" },
  { value: "te", label: "తెలుగు(Telugu)" },
  { value: "fa", label: "فارسی(Persian)" },
  { value: "lv", label: "Latviešu(Latvian)" },
  { value: "bn", label: "বাংলা(Bengali)" },
  { value: "sr", label: "Српски(Serbian)" },
  { value: "az", label: "Azərbaycanca(Azerbaijani)" },
  { value: "sl", label: "Slovenščina(Slovenian)" },
  { value: "kn", label: "ಕನ್ನಡ(Kannada)" },
  { value: "et", label: "Eesti(Estonian)" },
  { value: "mk", label: "Македонски(Macedonian)" },
  { value: "br", label: "Brezhoneg(Breton)" },
  { value: "eu", label: "Euskara(Basque)" },
  { value: "is", label: "Íslenska(Icelandic)" },
  { value: "hy", label: "Հայերեն(Armenian)" },
  { value: "ne", label: "नेपाली(Nepali)" },
  { value: "mn", label: "Монгол(Mongolian)" },
  { value: "bs", label: "Bosanski(Bosnian)" },
  { value: "kk", label: "Қазақ(Kazakh)" },
  { value: "sq", label: "Shqip(Albanian)" },
  { value: "sw", label: "Kiswahili(Swahili)" },
  { value: "gl", label: "Galego(Galician)" },
  { value: "mr", label: "मराठी(Marathi)" },
  { value: "pa", label: "ਪੰਜਾਬੀ(Punjabi)" },
  { value: "si", label: "සිංහල(Sinhala)" },
  { value: "km", label: "ខ្មែរ(Khmer)" },
  { value: "sn", label: "chiShona(Shona)" },
  { value: "yo", label: "Yorùbá(Yoruba)" },
  { value: "so", label: "Soomaali(Somali)" },
  { value: "af", label: "Afrikaans" },
  { value: "oc", label: "Occitan" },
  { value: "ka", label: "ქართული(Georgian)" },
  { value: "be", label: "Беларуская(Belarusian)" },
  { value: "tg", label: "Тоҷикӣ(Tajik)" },
  { value: "sd", label: "سنڌي(Sindhi)" },
  { value: "gu", label: "ગુજરાતી(Gujarati)" },
  { value: "am", label: "አማርኛ(Amharic)" },
  { value: "yi", label: "ייִדיש(Yiddish)" },
  { value: "lo", label: "ລາວ(Lao)" },
  { value: "uz", label: "Oʻzbek(Uzbek)" },
  { value: "fo", label: "Føroyskt(Faroese)" },
  { value: "ht", label: "Kreyòl ayisyen(Haitian Creole)" },
  { value: "ps", label: "پښتو(Pashto)" },
  { value: "tk", label: "Türkmen(Turkmen)" },
  { value: "nn", label: "Nynorsk" },
  { value: "mt", label: "Malti(Maltese)" },
  { value: "sa", label: "संस्कृतम्(Sanskrit)" },
  { value: "lb", label: "Lëtzebuergesch(Luxembourgish)" },
  { value: "my", label: "မြန်မာ(Myanmar)" },
  { value: "bo", label: "བོད་ཡིག(Tibetan)" },
  { value: "tl", label: "Tagalog" },
  { value: "mg", label: "Malagasy" },
  { value: "as", label: "অসমীয়া(Assamese)" },
  { value: "tt", label: "Татар(Tatar)" },
  { value: "haw", label: "ʻŌlelo Hawaiʻi(Hawaiian)" },
  { value: "ln", label: "Lingála(Lingala)" },
  { value: "ha", label: "Hausa" },
  { value: "ba", label: "Башҡорт(Bashkir)" },
  { value: "jw", label: "Basa Jawa(Javanese)" },
  { value: "su", label: "Basa Sunda(Sundanese)" },
] as const;

export const SETTINGS_FIELDS = [
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
] as const;

export const settingsScopeSchema = z.enum(["global", "project"]);

const thinkingLevelSchema = z.enum(["off", "minimal", "low", "medium", "high", "xhigh"]);
const steeringModeSchema = z.enum(["one-at-a-time", "all"]);
const transportSchema = z.enum(["sse", "websocket", "websocket-cached", "auto"]);
const doubleEscapeActionSchema = z.enum(["tree", "fork", "none"]);
const treeFilterModeSchema = z.enum(["default", "no-tools", "user-only", "labeled-only", "all"]);
const speechLanguageValues = SPEECH_LANGUAGE_OPTIONS.map(({ value }) => value) as [
  (typeof SPEECH_LANGUAGE_OPTIONS)[number]["value"],
  ...(typeof SPEECH_LANGUAGE_OPTIONS)[number]["value"][],
];
const speechLanguageSchema = z.union([
  z.enum(speechLanguageValues),
  z.enum(["en-US", "ko-KR", "ja-JP", "zh-CN"]),
]);
const voiceLanguageSchema = z.string();
const whisperModelSchema = z.enum([
  "tiny-q5",
  "tiny",
  "base-q5",
  "base",
  "small-q5",
  "small",
  "medium-q5",
  "medium",
  "large-v3-q5",
  "large-v3",
]);

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
  readResponsesAloud: z.boolean().optional(),
  voice: z.object({
    language: voiceLanguageSchema.optional(),
  }).partial().passthrough().optional(),
  enableSpeechInput: z.boolean().optional(),
  speechInput: z.object({
    language: speechLanguageSchema.optional(),
    useLocalWhisper: z.boolean().optional(),
    whisperModel: whisperModelSchema.optional(),
  }).partial().passthrough().optional(),
  speechLanguage: speechLanguageSchema.optional(),
  collapseChangelog: z.boolean().optional(),
  quietStartup: z.boolean().optional(),
  enableInstallTelemetry: z.boolean().optional(),
  doubleEscapeAction: doubleEscapeActionSchema.optional(),
  treeFilterMode: treeFilterModeSchema.optional(),
  remoteNotifications: z.object({
    discord: z.object({
      enabled: z.boolean().optional(),
      token: z.string().optional(),
      channelId: z.string().optional(),
    }).partial().passthrough().optional(),
    telegram: z.object({
      enabled: z.boolean().optional(),
      token: z.string().optional(),
      chatId: z.string().optional(),
    }).partial().passthrough().optional(),
  }).partial().passthrough().optional(),
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
  readResponsesAloud: nullishBoolean,
  voice: z.object({
    language: voiceLanguageSchema.nullable().optional(),
  }).partial().passthrough().optional(),
  enableSpeechInput: nullishBoolean,
  speechInput: z.object({
    language: speechLanguageSchema.nullable().optional(),
    useLocalWhisper: nullishBoolean,
    whisperModel: whisperModelSchema.nullable().optional(),
  }).partial().passthrough().optional(),
  speechLanguage: speechLanguageSchema.nullable().optional(),
  collapseChangelog: nullishBoolean,
  quietStartup: nullishBoolean,
  enableInstallTelemetry: nullishBoolean,
  doubleEscapeAction: doubleEscapeActionSchema.nullable().optional(),
  treeFilterMode: treeFilterModeSchema.nullable().optional(),
  remoteNotifications: z.object({
    discord: z.object({
      enabled: nullishBoolean,
      token: nullishString,
      channelId: nullishString,
    }).partial().passthrough().optional(),
    telegram: z.object({
      enabled: nullishBoolean,
      token: nullishString,
      chatId: nullishString,
    }).partial().passthrough().optional(),
  }).partial().passthrough().optional(),
  warnings: z.object({ anthropicExtraUsage: nullishBoolean }).partial().passthrough().optional(),
}).partial().passthrough();

export const workspaceSettingsSchema = z.object({
  global: settingsObjectSchema,
  project: settingsObjectSchema,
  effective: settingsObjectSchema,
  paths: z.object({
    global: z.string().optional(),
    project: z.string().optional(),
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
