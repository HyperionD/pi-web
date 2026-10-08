// @vitest-environment node
import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";

const template = readFileSync(new URL("../../App.astro", import.meta.url), "utf8");

describe("server-rendered transcript stream cards", () => {
  it("renders thinking with disclosure semantics and raw text for correct hydration", () => {
    expect(template).toContain('data-kind="think" data-raw-text={msg.text}');
    expect(template.includes('set:html={messageAvatarHtml("think")}')).toBe(true);
    expect(template).toContain('class="thinking-block stream-card thinking-card" open={msg.status === "streaming"}');
    expect(template).toContain('<summary class="stream-summary">');
    expect(template).toContain('<div class="thinking-copy">{msg.text}</div>');
  });

  it("uses shared icon-only user and Pi identities for server-rendered messages", () => {
    expect(template.includes('set:html={messageAvatarHtml("user")}')).toBe(true);
    expect(template.includes('set:html={messageAvatarHtml("pi")}')).toBe(true);
    expect(template.includes('data-kind="user" data-raw-text={msg.text}')).toBe(true);
    expect(template).not.toContain('class="prefix pi">agent');
    expect(template).not.toContain('class="prefix user">您');
  });

  it("shares tool statuses and shell classification with the client-rendered cards", () => {
    expect(template).toContain('class:list={["tool-card", "stream-card", isShellTool(msg.tool) && "command-card"]}');
    expect(template).toContain('<summary class="tc-head stream-summary">');
    expect(template).toContain('set:html={toolMessageMethods.toolStatus(msg)}');
    expect(template).toContain('class="tc-command" hidden={!formatToolArgs(msg.tool, msg.args)}');
    expect(template).toContain('class="tc-output-label" data-i18n="toolOutput"');
    expect(template).toContain('data-i18n-aria-label="toolOutput" aria-label="实时输出"');
  });
});
