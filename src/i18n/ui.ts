import { DEFAULT_UI_LOCALE, type UiLocale } from "./locales";

const EN_MESSAGES = {
  language: "Language",
  languageHint: "Changes apply immediately and are saved on this device.",
  settingsTitle: "pi settings",
  settingsSubtitle: "same files used by /settings",
  closeSettings: "close settings",
  scope: "scope",
  project: "project",
  global: "global",
  authTitle: "Authentication",
  authMode: "API key login",
  authStatus: "API key is stored in ~/.pi/agent/auth.json",
  provider: "provider",
  apiKey: "API key",
  pasteApiKey: "paste API key",
  saveKey: "save key",
  logout: "logout",
  oauthProvider: "OAuth provider",
  loadingOAuthProviders: "loading OAuth providers…",
  loginWithOAuth: "login with OAuth",
  openLoginPage: "open login page",
  oauthInput: "prompt input or redirect URL",
  send: "send",
  oauthStatus: "OAuth supports Claude, Codex, and Copilot subscriptions.",
  model: "Model",
  modelDescription: "provider, model, thinking",
  conversation: "Conversation",
  conversationDescription: "prompt flow and transcript behavior",
  warnings: "Warnings",
  warningsDescription: "usage and safety notices",
  effectiveNotSet: "effective: not set",
  settingsInheritHint: "blank fields inherit from effective settings",
  cancel: "cancel",
  save: "save",
} as const;

export type UiMessageKey = keyof typeof EN_MESSAGES;

export const UI_MESSAGES = {
  en: EN_MESSAGES,
  "zh-CN": {
    language: "语言", languageHint: "更改会立即应用并保存在此设备上。", settingsTitle: "pi 设置", settingsSubtitle: "使用与 /settings 相同的文件", closeSettings: "关闭设置", scope: "范围", project: "项目", global: "全局", authTitle: "身份验证", authMode: "API 密钥登录", authStatus: "API 密钥存储在 ~/.pi/agent/auth.json", provider: "提供商", apiKey: "API 密钥", pasteApiKey: "粘贴 API 密钥", saveKey: "保存密钥", logout: "退出登录", oauthProvider: "OAuth 提供商", loadingOAuthProviders: "正在加载 OAuth 提供商…", loginWithOAuth: "使用 OAuth 登录", openLoginPage: "打开登录页面", oauthInput: "提示输入或重定向 URL", send: "发送", oauthStatus: "OAuth 支持 Claude、Codex 和 Copilot 订阅。", model: "模型", modelDescription: "提供商、模型、思考", conversation: "对话", conversationDescription: "提示流程和转录行为", warnings: "警告", warningsDescription: "使用和安全提示", effectiveNotSet: "有效值：未设置", settingsInheritHint: "空字段会继承有效设置", cancel: "取消", save: "保存",
  },
  ja: {
    language: "言語", languageHint: "変更はすぐに適用され、このデバイスに保存されます。", settingsTitle: "pi 設定", settingsSubtitle: "/settings と同じファイルを使用します", closeSettings: "設定を閉じる", scope: "スコープ", project: "プロジェクト", global: "グローバル", authTitle: "認証", authMode: "API キーログイン", authStatus: "API キーは ~/.pi/agent/auth.json に保存されます", provider: "プロバイダー", apiKey: "API キー", pasteApiKey: "API キーを貼り付け", saveKey: "キーを保存", logout: "ログアウト", oauthProvider: "OAuth プロバイダー", loadingOAuthProviders: "OAuth プロバイダーを読み込み中…", loginWithOAuth: "OAuth でログイン", openLoginPage: "ログインページを開く", oauthInput: "プロンプト入力またはリダイレクト URL", send: "送信", oauthStatus: "OAuth は Claude、Codex、Copilot サブスクリプションをサポートします。", model: "モデル", modelDescription: "プロバイダー、モデル、思考", conversation: "会話", conversationDescription: "プロンプトフローとトランスクリプト動作", warnings: "警告", warningsDescription: "使用量と安全に関する通知", effectiveNotSet: "有効値: 未設定", settingsInheritHint: "空のフィールドは有効設定を継承します", cancel: "キャンセル", save: "保存",
  },
  es: {
    language: "Idioma", languageHint: "Los cambios se aplican al instante y se guardan en este dispositivo.", settingsTitle: "configuración de pi", settingsSubtitle: "usa los mismos archivos que /settings", closeSettings: "cerrar configuración", scope: "ámbito", project: "proyecto", global: "global", authTitle: "Autenticación", authMode: "inicio con clave API", authStatus: "La clave API se guarda en ~/.pi/agent/auth.json", provider: "proveedor", apiKey: "clave API", pasteApiKey: "pegar clave API", saveKey: "guardar clave", logout: "cerrar sesión", oauthProvider: "proveedor OAuth", loadingOAuthProviders: "cargando proveedores OAuth…", loginWithOAuth: "iniciar con OAuth", openLoginPage: "abrir página de inicio", oauthInput: "entrada de prompt o URL de redirección", send: "enviar", oauthStatus: "OAuth admite suscripciones de Claude, Codex y Copilot.", model: "Modelo", modelDescription: "proveedor, modelo, razonamiento", conversation: "Conversación", conversationDescription: "flujo de prompt y comportamiento del transcript", warnings: "Advertencias", warningsDescription: "avisos de uso y seguridad", effectiveNotSet: "efectivo: sin establecer", settingsInheritHint: "los campos vacíos heredan la configuración efectiva", cancel: "cancelar", save: "guardar",
  },
  "pt-BR": {
    language: "Idioma", languageHint: "As alterações são aplicadas imediatamente e salvas neste dispositivo.", settingsTitle: "configurações do pi", settingsSubtitle: "usa os mesmos arquivos de /settings", closeSettings: "fechar configurações", scope: "escopo", project: "projeto", global: "global", authTitle: "Autenticação", authMode: "login com chave de API", authStatus: "A chave de API é armazenada em ~/.pi/agent/auth.json", provider: "provedor", apiKey: "chave de API", pasteApiKey: "cole a chave de API", saveKey: "salvar chave", logout: "sair", oauthProvider: "provedor OAuth", loadingOAuthProviders: "carregando provedores OAuth…", loginWithOAuth: "entrar com OAuth", openLoginPage: "abrir página de login", oauthInput: "entrada de prompt ou URL de redirecionamento", send: "enviar", oauthStatus: "OAuth suporta assinaturas Claude, Codex e Copilot.", model: "Modelo", modelDescription: "provedor, modelo, raciocínio", conversation: "Conversa", conversationDescription: "fluxo de prompt e comportamento do transcript", warnings: "Avisos", warningsDescription: "avisos de uso e segurança", effectiveNotSet: "efetivo: não definido", settingsInheritHint: "campos vazios herdam as configurações efetivas", cancel: "cancelar", save: "salvar",
  },
  fr: {
    language: "Langue", languageHint: "Les changements s’appliquent immédiatement et sont enregistrés sur cet appareil.", settingsTitle: "paramètres pi", settingsSubtitle: "utilise les mêmes fichiers que /settings", closeSettings: "fermer les paramètres", scope: "portée", project: "projet", global: "global", authTitle: "Authentification", authMode: "connexion par clé API", authStatus: "La clé API est stockée dans ~/.pi/agent/auth.json", provider: "fournisseur", apiKey: "clé API", pasteApiKey: "coller la clé API", saveKey: "enregistrer la clé", logout: "déconnexion", oauthProvider: "fournisseur OAuth", loadingOAuthProviders: "chargement des fournisseurs OAuth…", loginWithOAuth: "se connecter avec OAuth", openLoginPage: "ouvrir la page de connexion", oauthInput: "entrée de prompt ou URL de redirection", send: "envoyer", oauthStatus: "OAuth prend en charge les abonnements Claude, Codex et Copilot.", model: "Modèle", modelDescription: "fournisseur, modèle, réflexion", conversation: "Conversation", conversationDescription: "flux de prompt et comportement du transcript", warnings: "Avertissements", warningsDescription: "avis d’utilisation et de sécurité", effectiveNotSet: "effectif : non défini", settingsInheritHint: "les champs vides héritent des paramètres effectifs", cancel: "annuler", save: "enregistrer",
  },
  ru: {
    language: "Язык", languageHint: "Изменения применяются сразу и сохраняются на этом устройстве.", settingsTitle: "настройки pi", settingsSubtitle: "используются те же файлы, что и /settings", closeSettings: "закрыть настройки", scope: "область", project: "проект", global: "глобально", authTitle: "Аутентификация", authMode: "вход по API-ключу", authStatus: "API-ключ хранится в ~/.pi/agent/auth.json", provider: "провайдер", apiKey: "API-ключ", pasteApiKey: "вставьте API-ключ", saveKey: "сохранить ключ", logout: "выйти", oauthProvider: "провайдер OAuth", loadingOAuthProviders: "загрузка провайдеров OAuth…", loginWithOAuth: "войти через OAuth", openLoginPage: "открыть страницу входа", oauthInput: "ввод prompt или URL перенаправления", send: "отправить", oauthStatus: "OAuth поддерживает подписки Claude, Codex и Copilot.", model: "Модель", modelDescription: "провайдер, модель, рассуждение", conversation: "Разговор", conversationDescription: "поток prompt и поведение transcript", warnings: "Предупреждения", warningsDescription: "уведомления об использовании и безопасности", effectiveNotSet: "эффективно: не задано", settingsInheritHint: "пустые поля наследуют эффективные настройки", cancel: "отмена", save: "сохранить",
  },
  de: {
    language: "Sprache", languageHint: "Änderungen werden sofort angewendet und auf diesem Gerät gespeichert.", settingsTitle: "pi-Einstellungen", settingsSubtitle: "verwendet dieselben Dateien wie /settings", closeSettings: "Einstellungen schließen", scope: "Bereich", project: "Projekt", global: "global", authTitle: "Authentifizierung", authMode: "API-Key-Login", authStatus: "Der API-Key wird in ~/.pi/agent/auth.json gespeichert", provider: "Anbieter", apiKey: "API-Key", pasteApiKey: "API-Key einfügen", saveKey: "Key speichern", logout: "abmelden", oauthProvider: "OAuth-Anbieter", loadingOAuthProviders: "OAuth-Anbieter werden geladen…", loginWithOAuth: "mit OAuth anmelden", openLoginPage: "Login-Seite öffnen", oauthInput: "Prompt-Eingabe oder Redirect-URL", send: "senden", oauthStatus: "OAuth unterstützt Claude-, Codex- und Copilot-Abonnements.", model: "Modell", modelDescription: "Anbieter, Modell, Denken", conversation: "Konversation", conversationDescription: "Prompt-Fluss und Transcript-Verhalten", warnings: "Warnungen", warningsDescription: "Nutzungs- und Sicherheitshinweise", effectiveNotSet: "effektiv: nicht gesetzt", settingsInheritHint: "leere Felder erben die effektiven Einstellungen", cancel: "abbrechen", save: "speichern",
  },
} satisfies Record<UiLocale, Partial<Record<UiMessageKey, string>>>;

export function uiMessage(locale: UiLocale, key: UiMessageKey): string {
  /* v8 ignore next -- locale dictionaries are complete for supported UI keys */
  return UI_MESSAGES[locale][key] || UI_MESSAGES[DEFAULT_UI_LOCALE][key];
}
