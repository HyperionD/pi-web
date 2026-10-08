import { currentUiLocale } from "../../i18n/client";
import { uiMessage } from "../../i18n/ui";
import { escapeHtml } from "../../lib/renderers";

// Lucide paths keep server-rendered and streamed message identities identical.
const ICON_PATHS = {
  user: '<circle cx="12" cy="8" r="5"/><path d="M20 21a8 8 0 0 0-16 0"/>',
  think: [
    '<path d="M15 14c.2-1 .7-1.7 1.5-2.5 1-.9 1.5-2.2 1.5-3.5A6 6 0 0 0 6 8c0 1 .2 2.2 ' +
      '1.5 3.5.7.7 1.3 1.5 1.5 2.5"/>',
    '<path d="M9 18h6"/>',
    '<path d="M10 22h4"/>',
  ].join(""),
};

export function messageAvatarHtml(kind: string): string {
  if (!["user", "think", "pi"].includes(kind)) return "";
  const label = kind === "user" ? uiMessage(currentUiLocale(), "messageUser") : kind === "think" ? "AI" : "Pi";
  const localization = kind === "user" ? ' data-i18n-aria-label="messageUser"' : "";
  const icon = kind === "pi"
    ? '<span class="message-pi-symbol" data-message-icon="pi" aria-hidden="true">π</span>'
    : `<svg class="message-avatar-icon" data-message-icon="${kind === "user" ? "user-round" : "lightbulb"}"` +
      ` aria-hidden="true" viewBox="0 0 24 24">${ICON_PATHS[kind]}</svg>`;
  return `<span class="message-avatar" data-message-avatar="${kind}" role="img"` +
    ` aria-label="${escapeHtml(label)}"${localization}>${icon}</span>`;
}
