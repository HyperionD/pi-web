// @ts-nocheck
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanupPiAppFixture, connectPiApp, installPiAppFixture } from "../test-helper";

describe("pi-app messages", () => {
  beforeEach(installPiAppFixture);
  afterEach(cleanupPiAppFixture);

  it("keeps message methods safe when transcript containers are absent", async () => {
    const app = await connectPiApp();
    app.termInner = null;
    expect(() => app.renderMessages([{ kind: "pi", text: "ignored" }])).not.toThrow();
    expect(() => app.appendMessage({ kind: "pi", text: "ignored" })).not.toThrow();
    expect(() => app.appendMessage()).not.toThrow();
  });

  it("does not render a welcome banner for a new session", async () => {
    const app = await connectPiApp();
    app.renderMessages([]);

    expect(app.querySelector("[data-welcome-banner]")).toBeNull();
    expect(app.textContent).not.toContain("connecting to local backend");
  });

  it("renders localized transcript prefixes and the current model for thinking", async () => {
    const app = await connectPiApp();
    app.renderMessages([]);
    app.appendMessage({ kind: "think", text: "before model status" });
    expect(app.querySelector(".msg[data-kind='think'] .prefix").textContent).toBe("—");

    app.updatePromptMeta({ model: "GPT-5.5" });
    expect(app.querySelector(".msg[data-kind='think'] .prefix").textContent).toBe("GPT-5.5");

    app.appendMessage({ kind: "user", text: "hello" });
    app.appendMessage({ kind: "pi", text: "world" });
    expect(app.querySelector(".msg[data-kind='user'] .prefix").textContent).toBe("您 >");
    expect(app.querySelector(".msg[data-kind='pi'] .prefix").textContent).toBe("agent >");
  });

  it("renders banner, thinking, image, fallback choice, and unknown messages", async () => {
    const app = await connectPiApp();
    app.renderMessages([]);

    app.appendMessage({ kind: "banner", text: "<a>ready</a>" });
    app.appendMessage({ kind: "think", text: "<reason>" });
    app.appendMessage({ kind: "user", text: "see", attachments: [{ type: "image", dataUrl: "data:image/png;base64,a", name: "shot" }] });
    app.appendMessage({ kind: "custom", value: 1 });
    app.appendMessage({
      kind: "pi",
      text: [
        "```json",
        '{"type":"piweb_choice","id":"c1","question":"Pick?","options":[{"label":"A","value":"a","description":"Alpha"},{"label":"B","value":"b"}],"allowCustom":false}',
        "```",
      ].join("\n"),
    });

    expect(app.querySelector(".ascii-banner").textContent).toContain("ready");
    expect(app.querySelector(".thinking-block").textContent).toContain("<reason>");
    expect(app.querySelector(".msg-image").getAttribute("alt")).toBe("shot");
    expect(app.querySelector(".fallback-choice-list [data-choice-value='a'] small").textContent).toBe("Alpha");
    expect(app.querySelector("[data-choice-custom-input]")).not.toBeNull();
    expect(app.querySelector(".msg[data-kind='pi'] .body").textContent).toBe("");

    app.appendMessage({ kind: "user", text: "unnamed", attachments: [{ type: "image", dataUrl: "data:image/png;base64,b" }] });
    expect([...app.querySelectorAll(".msg-image")].at(-1).getAttribute("alt")).toBe("attached image");

    app.disableAnsweredChoice();
    const savedTerm = app.termInner;
    app.termInner = null;
    app.disableAnsweredChoice("missing");
    app.termInner = savedTerm;
    app.disableAnsweredChoice("c1");
    expect(app.querySelector(".fallback-choice-list").classList.contains("answered")).toBe(true);
    expect(app.querySelector(".fallback-choice-list button").disabled).toBe(true);
  });


  it("deduplicates echoed user prompts and removes loading on response", async () => {
    const app = await connectPiApp();
    app.renderMessages([]);
    app.appendMessage({ kind: "user", text: "hello" });
    app.appendLoadingMessage();
    app.appendMessage({ kind: "user", text: "hello" });
    expect(app.querySelectorAll(".msg[data-kind='user']")).toHaveLength(1);
    expect(app.querySelector(".msg.loading .spinner")).not.toBeNull();
    app.appendMessage({ kind: "pi", text: "world" });
    expect(app.querySelector(".msg.loading")).toBeNull();
  });

  it("renders user prompts through markdown with preserved line breaks", async () => {
    const app = await connectPiApp();
    const text = [
      "PR 올림: https://github.com/Epsilondelta-ai/juun-ai/pull/14",
      "",
      "Checks:",
      "- Branch pushed",
      "- PR created",
      "- Docs-only change",
    ].join("\n");
    app.renderMessages([]);
    app.appendMessage({ kind: "user", text });
    app.appendLoadingMessage();
    app.appendMessage({ kind: "user", text });

    const body = app.querySelector(".msg[data-kind='user'] .body");
    expect(app.querySelectorAll(".msg[data-kind='user']")).toHaveLength(1);
    expect(body.classList.contains("markdown-body")).toBe(true);
    expect(body.querySelector("a").textContent).toBe("https://github.com/Epsilondelta-ai/juun-ai/pull/14");
    expect([...body.querySelectorAll("p")].map((item) => item.textContent)).toEqual([
      "PR 올림: https://github.com/Epsilondelta-ai/juun-ai/pull/14",
      "Checks:",
    ]);
    expect([...body.querySelectorAll("li")].map((item) => item.textContent)).toEqual([
      "Branch pushed",
      "PR created",
      "Docs-only change",
    ]);
  });

  it("copies markdown code blocks with button feedback", async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, "clipboard", { value: { writeText }, configurable: true });
    const app = await connectPiApp();
    app.renderMessages([]);
    app.appendMessage({ kind: "pi", text: ["```ts", "const ok = true;", "```"].join("\n") });

    const button = app.querySelector(".code-copy-btn");
    button.click();
    await Promise.resolve();
    await Promise.resolve();

    expect(writeText).toHaveBeenCalledWith("const ok = true;\n");
    expect(button.dataset.copyStatus).toBe("copied");
    expect(button.textContent).toBe("copied");
  });



  it("covers fallback defaults for images and detached choice lookup", async () => {
    const app = await connectPiApp();
    const image = app.userMessageNode({ kind: "user", text: "img", attachments: [{ type: "image", dataUrl: "data:image/png;base64,x" }] });
    expect(image.querySelector("img").alt).toBe("attached image");
    const term = app.termInner;
    app.termInner = undefined;
    app.disableAnsweredChoice("missing");
    app.termInner = term;
  });



  it("deduplicates replayed tools that are outside the virtualized DOM", async () => {
    const app = await connectPiApp();
    app.renderMessages([{
      kind: "tool",
      id: "call-2",
      tool: "read",
      status: "ok",
      body: "done",
    }]);
    app.termInner.replaceChildren();

    app.applyEvent({
      type: "tool.started",
      payload: { id: "call-2", kind: "tool", tool: "read", status: "running" },
    });

    expect(app.transcriptItems).toHaveLength(1);
  });

  it("renders lucide-style tool icons while unknown tools keep the dot", async () => {
    const app = await connectPiApp();
    app.renderMessages([]);

    app.appendMessage({ kind: "tool", tool: "read", status: "ok" });
    app.appendMessage({ kind: "tool", tool: "bash", args: JSON.stringify({ command: "git status" }), status: "ok" });
    app.appendMessage({ kind: "tool", tool: "bash", args: JSON.stringify({ command: "bun test" }), status: "ok" });
    app.appendMessage({ kind: "tool", tool: "bash", args: JSON.stringify({ command: "bun run build" }), status: "ok" });
    app.appendMessage({ kind: "tool", tool: "bash", args: JSON.stringify({ command: "bun install" }), status: "ok" });
    app.appendMessage({ kind: "tool", tool: "bash", args: JSON.stringify({ command: "pwd" }), status: "ok" });
    app.appendMessage({ kind: "tool", tool: "unknown_tool", status: "ok" });

    const glyphs = [...app.querySelectorAll(".tool-card .tc-glyph")];
    expect(glyphs[0].querySelector("[data-tool-icon='book-open']")).not.toBeNull();
    expect(glyphs[1].querySelector("[data-tool-icon='git-branch']")).not.toBeNull();
    expect(glyphs[2].querySelector("[data-tool-icon='circle-check']")).not.toBeNull();
    expect(glyphs[3].querySelector("[data-tool-icon='hammer']")).not.toBeNull();
    expect(glyphs[4].querySelector("[data-tool-icon='package']")).not.toBeNull();
    expect(glyphs[5].querySelector("[data-tool-icon='terminal']")).not.toBeNull();
    expect(glyphs[6].querySelector("svg")).toBeNull();
    expect(glyphs[6].textContent).toBe("●");
  });



















  it("does not show loading after a final assistant message while waiting for idle status", async () => {
    const app = await connectPiApp();
    app.renderMessages([]);
    app.setMode("running");
    app.appendMessage({ kind: "pi", text: "complete answer" });
    expect(app.querySelector(".msg.loading")).toBeNull();
  });




  it("renders response errors as escaped transcript messages", async () => {
    const app = await connectPiApp();
    app.renderMessages([]);

    app.appendErrorMessage("<script>alert('xss')</script>");

    const error = app.querySelector(".term-inner .msg.error");
    expect(error?.querySelector(".prefix")?.textContent).toBe("error >");
    expect(error?.querySelector(".body")?.textContent).toBe("<script>alert('xss')</script>");
    expect(error?.querySelector(".body")?.innerHTML).not.toContain("<script>");
    expect(app.querySelector(".streaming-tail .msg.error")).toBeNull();
  });



  it("coalesces repeated terminal scroll requests into one animation frame", async () => {
    const frames = [];
    vi.spyOn(window, "requestAnimationFrame").mockImplementation((callback) => {
      frames.push(callback);
      return frames.length;
    });

    const app = await connectPiApp();
    app.scrollFrame = undefined;
    app.scrollTerm();
    app.scrollTerm();
    app.scrollTerm();

    expect(frames).toHaveLength(1);
  });

  it("keeps transient loading messages out of transcript state without height notifications", async () => {
    const app = await connectPiApp();
    app.renderMessages([]);
    const heightNotification = vi.spyOn(app, "notifyTranscriptNodeHeightDidChange");
    app.appendLoadingMessage();

    expect(app.querySelector(".msg.loading")).not.toBeNull();
    expect(app.transcriptItems).toHaveLength(0);
    expect(heightNotification).not.toHaveBeenCalled();

    app.removeLoadingMessage();

    expect(app.transcriptItems).toHaveLength(0);
    expect(app.querySelector(".msg.loading")).toBeNull();
    expect(heightNotification).not.toHaveBeenCalled();
  });


  it("keeps following the bottom when the virtual scroller scrolls programmatically", async () => {
    const app = await connectPiApp();
    app.renderMessages([]);
    app.transcriptFollowBottom = true;
    Object.defineProperty(app.term, "scrollHeight", { configurable: true, get: () => 1000 });
    Object.defineProperty(app.term, "clientHeight", { configurable: true, get: () => 100 });
    Object.defineProperty(app.term, "scrollTop", { configurable: true, get: () => 500, set: () => {} });

    // programmatic scroll (virtual scroller layout correction) must not break follow-bottom
    app.term.dispatchEvent(new Event("scroll"));
    expect(app.transcriptFollowBottom).toBe(true);
    expect(app.shouldStickToBottom()).toBe(true);

    // a user wheel gesture moving away from the bottom should stop follow-bottom
    app.term.dispatchEvent(new WheelEvent("wheel", { deltaY: 100 }));
    app.term.dispatchEvent(new Event("scroll"));
    expect(app.shouldStickToBottom()).toBe(false);
    expect(app.transcriptFollowBottom).toBe(false);
  });
});
