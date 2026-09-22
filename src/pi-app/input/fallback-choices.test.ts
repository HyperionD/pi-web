// @ts-nocheck
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { cleanupPiAppFixture, connectPiApp, installPiAppFixture } from "../test-helper";

const fallbackChoicePayload = JSON.stringify({
  type: "piweb_choice",
  id: "runtime",
  question: "Runtime?",
  options: [{ label: "Go", value: "go" }],
  allowCustom: false,
});

const customFallbackChoicePayload = JSON.stringify({
  type: "piweb_choice",
  id: "runtime",
  question: "Runtime?",
  options: [{ label: "Go", value: "go", description: "Use Go" }],
  allowCustom: true,
});

const fallbackChoiceJson = ["```json", fallbackChoicePayload, "```"].join("\n");
const customFallbackChoiceText = ["Pick one", "```json", customFallbackChoicePayload, "```"].join("\n");

const fallbackInputPayload = JSON.stringify({
  type: "piweb_input",
  id: "feedback",
  question: "Please explain the issue",
  placeholder: "Write your feedback",
  multiline: true,
});
const fallbackInputJson = ["```json", fallbackInputPayload, "```"].join("\n");

describe("pi-app fallback choices", () => {
  beforeEach(installPiAppFixture);
  afterEach(cleanupPiAppFixture);


  it("renders fallback choice blocks as clickable answer options", async () => {
    const app = await connectPiApp();
    app.renderMessages([]);
    app.appendMessage({ kind: "pi", text: customFallbackChoiceText });
    expect(app.querySelector(".fallback-choice-list strong").textContent).toBe("Runtime?");
    expect(app.querySelector("[data-action='fallback-choice'] .choice-label").textContent).toBe("Go");
    expect(app.querySelector("[data-choice-custom-input]").placeholder).toBe("Type a custom answer");
    expect(app.querySelector(".msg[data-kind='pi'] .body").textContent).toContain("Pick one");
    expect(app.querySelector(".msg[data-kind='pi'] .body").textContent).not.toContain("piweb_choice");
  });

  it("allows custom answers from the choice panel", async () => {
    const app = await connectPiApp();
    globalThis.fetch = vi.fn(async () => ({ ok: true, json: async () => ({}) }));
    app.apiConnected = true;
    app.dataset.activeSessionId = "s1";
    app.renderMessages([]);
    app.appendMessage({ kind: "pi", text: fallbackChoiceJson });

    const input = app.querySelector("[data-choice-custom-input]");
    input.value = "custom runtime";
    app.querySelector("[data-action='fallback-choice-custom']").click();
    await Promise.resolve();

    expect(app.querySelector(".fallback-choice-list").classList.contains("answered")).toBe(true);
    expect(app.querySelector("[data-action='fallback-choice']").disabled).toBe(true);
    expect(input.disabled).toBe(true);
    expect(JSON.parse(globalThis.fetch.mock.calls.at(-1)[1].body)).toMatchObject({
      text: "选项回复：\nid: runtime\nvalue: custom runtime",
      attachments: [],
    });
  });

  it("disables already answered fallback choices on reload", async () => {
    const app = await connectPiApp();
    app.renderMessages([
      { kind: "pi", text: customFallbackChoiceText },
      { kind: "user", text: "选项回复：\nid: runtime\nvalue: go" },
    ]);
    expect(app.querySelector(".fallback-choice-list").classList.contains("answered")).toBe(true);
    expect(app.querySelector("[data-action='fallback-choice']").disabled).toBe(true);
    expect(app.querySelector("[data-choice-custom-input]").disabled).toBe(true);
  });

  it("renders multiline fallback input and hides its protocol block", async () => {
    const app = await connectPiApp();
    app.renderMessages([]);
    app.appendMessage({ kind: "pi", text: fallbackInputJson });

    const panel = app.querySelector(".fallback-input-panel");
    expect(panel.querySelector("strong").textContent).toBe("Please explain the issue");
    expect(panel.querySelector("textarea").placeholder).toBe("Write your feedback");
    expect(panel.querySelector("[data-action='fallback-input-submit']")).not.toBeNull();
    expect(app.querySelector(".msg[data-kind='pi'] .body").textContent).not.toContain("piweb_input");
  });

  it("disables fallback input after its answer is restored", async () => {
    const app = await connectPiApp();
    app.renderMessages([
      { kind: "pi", text: fallbackInputJson },
      { kind: "user", text: "输入回复：\nid: feedback\nvalue: fixed\nline" },
    ]);

    const panel = app.querySelector(".fallback-input-panel");
    expect(panel.classList.contains("answered")).toBe(true);
    expect(panel.querySelector("textarea").disabled).toBe(true);
  });

  it("submits multiline fallback input with its id and preserved line breaks", async () => {
    const app = await connectPiApp();
    app.apiConnected = true;
    app.dataset.activeSessionId = "s1";
    app.renderMessages([]);
    app.appendMessage({ kind: "pi", text: fallbackInputJson });
    globalThis.fetch = vi.fn(async () => ({ ok: true, json: async () => ({}) }));

    const panel = app.querySelector(".fallback-input-panel");
    panel.querySelector("textarea").value = "line one\nline two";
    panel.querySelector("[data-action='fallback-input-submit']").click();
    await Promise.resolve();

    expect(panel.classList.contains("answered")).toBe(true);
    expect(JSON.parse(globalThis.fetch.mock.calls.at(-1)[1].body)).toMatchObject({
      text: "输入回复：\nid: feedback\nvalue: line one\nline two",
      attachments: [],
    });
  });
});
