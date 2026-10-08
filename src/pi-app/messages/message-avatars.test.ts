import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { applyUiLocale } from "../../i18n/client";
import { UI_LOCALE_STORAGE_KEY } from "../../i18n/locales";
import { cleanupPiAppFixture, installPiAppFixture } from "../test-helper";
import { messageAvatarHtml } from "./message-avatars";

function avatarNode(kind: string) {
  const container = document.createElement("div");
  container.innerHTML = messageAvatarHtml(kind);
  return container;
}

describe("message identity icons", () => {
  beforeEach(() => {
    installPiAppFixture();
    localStorage.setItem(UI_LOCALE_STORAGE_KEY, "zh-CN");
  });
  afterEach(cleanupPiAppFixture);

  it("renders a localized accessible user icon with no visible label", () => {
    const container = avatarNode("user");
    const avatar = container.querySelector("[role='img']")!;
    expect(avatar.getAttribute("aria-label")).toBe("您");
    expect(avatar.textContent).toBe("");
    expect(avatar.querySelector("svg[data-message-icon='user-round']")).not.toBeNull();
    applyUiLocale("en", container);
    expect(avatar.getAttribute("aria-label")).toBe("You");
    expect(avatar.textContent).toBe("");
  });

  it("renders lightbulb and Pi identities without AI or agent captions", () => {
    const thinking = avatarNode("think");
    const pi = avatarNode("pi");
    expect(thinking.querySelector("svg[data-message-icon='lightbulb']")).not.toBeNull();
    expect(thinking.querySelector('path[d="M9 18h6"]')).not.toBeNull();
    expect(thinking.querySelectorAll("path")).toHaveLength(3);
    expect(thinking.querySelector("[role='img']")!.getAttribute("aria-label")).toBe("AI");
    expect(thinking.textContent).toBe("");
    expect(pi.querySelector("[data-message-icon='pi']")!.textContent).toBe("π");
    expect(pi.querySelector("[role='img']")!.getAttribute("aria-label")).toBe("Pi");
    expect(pi.textContent).not.toContain("agent");
  });

  it("does not interpolate unsupported roles into trusted icon markup", () => {
    expect(messageAvatarHtml("error")).toBe("");
    expect(messageAvatarHtml('<script>alert("unsafe")</script>')).toBe("");
  });
});
