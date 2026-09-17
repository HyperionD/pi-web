// @ts-nocheck
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanupPiAppFixture, connectPiApp, installPiAppFixture } from "../test-helper";
import { emptyTranscriptState } from "../sessions/transcript-state";
import { installSessionSnapshot } from "../sessions/transcript-reducer";

function installVirtualizedTranscriptFixture() {
  installPiAppFixture();
  vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockImplementation(function () {
    const height = this.classList?.contains("transcript-item") ? 80 : 0;
    return { x: 0, y: 0, width: 0, height, top: 0, right: 0, bottom: height, left: 0, toJSON: () => ({}) };
  });
}

describe("pi-app transcript virtualization", () => {
  beforeEach(installVirtualizedTranscriptFixture);
  afterEach(cleanupPiAppFixture);

  it("virtualizes long transcripts to the visible message window", async () => {
    const app = await connectPiApp();
    const messages = Array.from({ length: 250 }, (_, index) => ({
      kind: "pi",
      text: `message ${index}`,
    }));
    const messageNode = vi.spyOn(app, "messageNode");

    app.renderMessages(messages);

    const renderedMessages = [...app.querySelectorAll(".term-inner .msg")];
    expect(app.transcriptItems).toHaveLength(250);
    expect(messageNode).toHaveBeenCalledTimes(30);
    expect(renderedMessages.length).toBeLessThan(250);
    expect(app.querySelectorAll(".term-inner .transcript-item")).toHaveLength(30);
    expect(app.termInner.style.paddingTop).not.toBe("0px");
    expect(renderedMessages.at(-1).textContent).toContain("message 249");
  });

  it("moves the virtual transcript window when the user scrolls upward", async () => {
    const app = await connectPiApp();
    Object.defineProperty(app.term, "clientHeight", { configurable: true, value: 600 });
    const messages = Array.from({ length: 250 }, (_, index) => ({
      kind: "pi",
      text: `message ${index}`,
    }));
    app.renderMessages(messages);

    app.term.scrollTop = 0;
    app.renderTranscriptWindow({ stickToBottom: false });

    const renderedMessages = [...app.querySelectorAll(".term-inner .msg")];
    expect(renderedMessages[0].textContent).toContain("message 0");
    expect(renderedMessages.at(-1).textContent).not.toContain("message 249");
  });

  it("renders appended transcript items without waiting for a user scroll", async () => {
    const app = await connectPiApp();
    const messages = Array.from({ length: 250 }, (_, index) => ({
      kind: "pi",
      text: `message ${index}`,
    }));
    app.renderMessages(messages);
    const previousTranscriptItems = app.transcriptItems;

    app.appendMessage({ kind: "pi", text: "new tail message" });

    const renderedMessages = [...app.querySelectorAll(".term-inner .msg")];
    expect(app.transcriptItems).not.toBe(previousTranscriptItems);
    expect(app.transcriptItems).toHaveLength(251);
    expect(renderedMessages.at(-1).textContent).toContain("new tail message");
  });

  it("renders a durable item appended to a virtualized transcript", async () => {
    const app = await connectPiApp();
    app.durableEvents = true;
    app.dataset.activeSessionId = "s1";
    const snapshot = installSessionSnapshot(emptyTranscriptState("s1"), {
      sessionId: "s1",
      items: Array.from({ length: 250 }, (_, index) => ({
        itemId: `pi:${index}`,
        ordinal: index + 1,
        kind: "pi",
        origin: "runtime",
        text: `message ${index}`,
      })),
      status: "idle",
      seq: "3",
      projectionGeneration: "1",
    });
    app.transcriptState = snapshot;
    app.renderDurableTranscript();
    const previousTranscriptItems = app.transcriptItems;

    app.applyDurableEvent({
      seq: "4",
      sessionId: "s1",
      type: "user.message.created",
      sourceId: "command:test:user",
      payload: { text: "new user message" },
    });

    await new Promise((resolve) => requestAnimationFrame(resolve));
    expect(app.transcriptItems).not.toBe(previousTranscriptItems);
    expect([...app.querySelectorAll(".term-inner .msg")].at(-1)?.textContent)
      .toContain("new user message");
  });

  it("keeps the streaming assistant text node stable between durable deltas", async () => {
    const app = await connectPiApp();
    app.durableEvents = true;
    app.dataset.activeSessionId = "s1";
    app.transcriptState = emptyTranscriptState("s1");
    app.renderDurableTranscript();

    app.applyDurableEvent({
      seq: "1",
      sessionId: "s1",
      type: "assistant.segment.started",
      entityId: "assistant:run-1:1",
      payload: { kind: "pi" },
    });
    const body = app.querySelector(".transcript-item .body");
    const initialChild = body.firstChild;

    app.applyDurableEvent({
      seq: "2",
      sessionId: "s1",
      type: "assistant.segment.delta",
      entityId: "assistant:run-1:1",
      payload: { kind: "pi", delta: "summary" },
    });
    await new Promise((resolve) => requestAnimationFrame(resolve));

    expect(body.firstChild).toBe(initialChild);
    expect(body.textContent).toBe("summary");
  });

  it("renders a choice panel when a streamed assistant item is committed", async () => {
    const app = await connectPiApp();
    app.durableEvents = true;
    app.dataset.activeSessionId = "s1";
    app.transcriptState = emptyTranscriptState("s1");
    app.renderDurableTranscript();

    const choiceText = [
      "```json",
      '{"type":"piweb_choice","id":"c1","question":"Pick?","options":[{"label":"A","value":"a"}]}',
      "```",
    ].join("\n");
    app.applyDurableEvent({
      seq: "1",
      sessionId: "s1",
      type: "assistant.segment.delta",
      entityId: "assistant:run-1:1",
      payload: { kind: "pi", delta: "```json\n{\"type\":\"piweb_choice\"" },
    });
    await new Promise((resolve) => requestAnimationFrame(resolve));
    expect(app.querySelector(".fallback-choice-list")).toBeNull();

    app.applyDurableEvent({
      seq: "2",
      sessionId: "s1",
      type: "assistant.segment.committed",
      entityId: "assistant:run-1:1",
      payload: { kind: "pi", text: choiceText },
    });

    expect(app.querySelector(".fallback-choice-list")).not.toBeNull();
    expect(app.querySelector(".fallback-choice-list strong").textContent).toBe("Pick?");
  });

  it("recreates a tool card from the latest message after its node is discarded", async () => {
    const app = await connectPiApp();
    app.renderMessages([{ id: "call-1", kind: "tool", tool: "bash", status: "running", body: "partial" }]);

    expect(app.transcriptItems[0].nodes).toBeUndefined();
    app.transcriptItems[0].message = {
      id: "call-1", kind: "tool", tool: "bash", status: "err", body: "final output",
    };
    app.destroyTranscriptVirtualScroller();
    app.termInner.replaceChildren();
    app.renderTranscriptWindow({ stickToBottom: true });

    const card = app.querySelector(".tool-card");
    expect(card?.dataset.status).toBe("err");
    expect(card?.querySelector(".tc-body")?.textContent).toContain("final output");
    expect(app.querySelectorAll(".tool-card")).toHaveLength(1);
  });

  it("renders only the latest 4000 characters for long tool output", async () => {
    const app = await connectPiApp();
    const largeBody = `old-start\n${"x".repeat(20000)}\ntail-marker`;

    app.renderMessages([]);
    app.appendMessage({
      kind: "tool",
      tool: "bash",
      status: "ok",
      collapsedByDefault: true,
      body: largeBody,
    });

    const body = app.querySelector(".tool-card .tc-body");
    expect(body.hidden).toBe(false);
    expect(body.textContent).not.toContain("old-start");
    expect(body.textContent).toContain("tail-marker");
    expect(body.textContent.length).toBeLessThan(largeBody.length);
  });

  it("renders full large tool output only after explicit request", async () => {
    const app = await connectPiApp();
    const largeBody = `${"line\n".repeat(5000)}tail-marker`;

    app.renderMessages([]);
    app.appendMessage({
      kind: "tool",
      tool: "bash",
      status: "ok",
      collapsedByDefault: true,
      body: largeBody,
    });

    expect(app.querySelector(".tool-card .tc-body").textContent).toContain("tail-marker");

    app.showFullToolOutput(app.querySelector("[data-action='show-full-tool-output']"));
    expect(app.querySelector(".tool-card .tc-body").textContent).toContain("tail-marker");
  });
});
