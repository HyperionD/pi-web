// @ts-nocheck
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanupPiAppFixture, connectPiApp, installPiAppFixture } from "../test-helper";

describe("pi-app transcript window", () => {
  beforeEach(installPiAppFixture);
  afterEach(cleanupPiAppFixture);

  it("renders only the newest 30 transcript items by default", async () => {
    const app = await connectPiApp();
    const messages = Array.from({ length: 35 }, (_, index) => ({
      kind: "pi",
      text: `message ${index}`,
    }));

    app.renderMessages(messages);

    const renderedMessages = [...app.querySelectorAll(".term-inner .msg")];
    expect(app.transcriptItems).toHaveLength(35);
    expect(renderedMessages).toHaveLength(30);
    expect(renderedMessages[0].textContent).toContain("message 5");
    expect(renderedMessages.at(-1).textContent).toContain("message 34");
  });

  it("adopts server-rendered transcript nodes with their collapsed margin height", async () => {
    const app = await connectPiApp();
    Object.defineProperty(app.term, "clientHeight", { configurable: true, value: 600 });
    vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockImplementation(function () {
      const height = this.classList?.contains("transcript-item") ? 35.6953125 : 21.6953125;
      return { x: 0, y: 0, width: 0, height, top: 0, right: 0, bottom: height, left: 0, toJSON: () => ({}) };
    });
    vi.spyOn(window, "getComputedStyle").mockImplementation((node) => ({
      marginTop: "0px",
      marginBottom: node.classList?.contains("msg") ? "14px" : "0px",
    }));
    const warn = vi.spyOn(console, "warn").mockImplementation(() => {});
    app.termInner.innerHTML = [
      `<div class="transcript-spacer"></div>`,
      `<div class="msg" data-kind="pi"><div class="prefix pi">pi &gt;</div><div class="body">existing</div></div>`,
    ].join("");

    app.adoptRenderedTranscript();

    expect(app.transcriptItems).toHaveLength(1);
    expect(app.transcriptItems[0].height).toBe(35.6953125);
    expect(app.querySelector(".term-inner .msg").textContent).toContain("existing");
    expect(warn.mock.calls.some((call) => call.some((part) => String(part).includes("height changed unexpectedly"))))
      .toBe(false);
  });

  it("covers tool fallback metadata and output chunks", async () => {
    const app = await connectPiApp();
    app.renderMessages([]);
    const card = app.toolCard({ kind: "tool", tool: "read" });
    expect(card.dataset.status).toBe("");
    expect(app.toolStatus({ status: "err" })).toContain("failed");
    app.appendTranscriptNode(card, { stickToBottom: true });
    app.appendToolOutput({ tool: "read" });
    await new Promise((resolve) => requestAnimationFrame(resolve));
    expect(card.querySelector(".tc-body").textContent).toBe("");
  });

  it("renders error tools and appends finished tools that were not visible", async () => {
    const app = await connectPiApp();
    app.renderMessages([]);

    app.appendMessage({ kind: "tool", tool: "bash", status: "err", resultMeta: "failed" });
    expect(app.querySelector(".tool-card .tc-meta").textContent).toContain("failed");

    app.renderMessages([]);
    app.finishTool({ kind: "tool", tool: "missing", status: "ok", body: "done" });
    expect(app.querySelector(".tool-card[data-tool='missing'] .tc-body").textContent).toContain("done");
  });

  it("keeps tool output flushing guarded and replaces cards when window replacement misses", async () => {
    const frames = [];
    vi.spyOn(window, "requestAnimationFrame").mockImplementation((callback) => {
      frames.push(callback);
      return frames.length;
    });

    const app = await connectPiApp();
    app.renderMessages([]);
    frames.length = 0;
    app.appendToolOutput({ tool: "missing", chunk: "ignored" });

    const busyBody = document.createElement("div");
    busyBody.__toolOutputFrame = 1;
    app.scheduleToolOutputFlush(busyBody);
    expect(frames).toHaveLength(0);

    const emptyBody = document.createElement("div");
    app.scheduleToolOutputFlush(emptyBody);
    frames.splice(0).forEach((callback) => callback(0));
    expect(emptyBody.textContent).toBe("");

    const bodyWithContent = document.createElement("div");
    bodyWithContent.textContent = "old";
    bodyWithContent.__pendingToolOutput = ["new"];
    app.scheduleToolOutputFlush(bodyWithContent);
    frames.splice(0).forEach((callback) => callback(0));
    expect(bodyWithContent.textContent).toBe("old\nnew");

    app.appendMessage({ kind: "tool", tool: "bash", status: "running" });
    vi.spyOn(app, "replaceTranscriptNode").mockReturnValue(false);
    app.finishTool({ kind: "tool", tool: "bash", status: "ok", body: "finished" });
    expect(app.querySelector(".tool-card[data-tool='bash'] .tc-body").textContent).toContain("finished");
  });

  it("keeps live tool output visible without a collapse interaction", async () => {
    const frames = [];
    vi.spyOn(window, "requestAnimationFrame").mockImplementation((callback) => {
      frames.push(callback);
      return frames.length;
    });

    let itemHeight = 80;
    vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockImplementation(function () {
      const height = this.classList?.contains("transcript-item") ? itemHeight : 0;
      return { x: 0, y: 0, width: 0, height, top: 0, right: 0, bottom: height, left: 0, toJSON: () => ({}) };
    });
    const app = await connectPiApp();
    Object.defineProperty(app.term, "clientHeight", { configurable: true, value: 600 });
    app.renderMessages([]);
    app.appendMessage({ kind: "tool", tool: "bash", args: JSON.stringify({ command: "git status" }), status: "running", collapsedByDefault: true });
    const heightChanged = vi.spyOn(app.transcriptVirtualScroller, "onItemHeightDidChange");
    app.appendToolOutput({ tool: "bash", chunk: "streaming output" });
    frames.splice(0).forEach((callback) => callback(0));

    const body = app.querySelector(".tool-card .tc-body");
    expect(body.hidden).toBe(false);
    expect(body.textContent).toContain("streaming output");
    expect(app.querySelector(".tc-head").dataset.action).toBeUndefined();
    expect(app.querySelector(".tc-args").textContent).toBe("git status");
    expect(heightChanged).not.toHaveBeenCalled();

    itemHeight = 120;
    app.toggleTool(app.querySelector(".tc-head"));
    expect(body.hidden).toBe(false);
  });

  it("keeps expanded live tool output pinned when it grows at the bottom", async () => {
    const frames = [];
    vi.spyOn(window, "requestAnimationFrame").mockImplementation((callback) => {
      frames.push(callback);
      return frames.length;
    });

    let itemHeight = 80;
    vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockImplementation(function () {
      const height = this.classList?.contains("transcript-item") ? itemHeight : 0;
      return { x: 0, y: 0, width: 0, height, top: 0, right: 0, bottom: height, left: 0, toJSON: () => ({}) };
    });
    const app = await connectPiApp();
    let scrollHeight = 1000;
    let scrollTop = 900;
    Object.defineProperty(app.term, "clientHeight", { configurable: true, value: 100 });
    Object.defineProperty(app.term, "scrollHeight", { configurable: true, get: () => scrollHeight });
    Object.defineProperty(app.term, "scrollTop", {
      configurable: true,
      get: () => scrollTop,
      set: (value) => { scrollTop = value; },
    });
    app.renderMessages([]);
    frames.splice(0).forEach((callback) => callback(0));
    frames.splice(0).forEach((callback) => callback(0));

    app.appendMessage({ kind: "tool", tool: "bash", status: "running", collapsedByDefault: false });
    frames.splice(0).forEach((callback) => callback(0));
    frames.splice(0).forEach((callback) => callback(0));
    app.toggleTool(app.querySelector(".tc-head"));
    frames.splice(0).forEach((callback) => callback(0));
    frames.splice(0).forEach((callback) => callback(0));
    scrollHeight = 1200;
    itemHeight = 280;
    app.appendToolOutput({ tool: "bash", chunk: "streaming output" });
    frames.splice(0).forEach((callback) => callback(0));
    frames.splice(0).forEach((callback) => callback(0));
    frames.splice(0).forEach((callback) => callback(0));

    expect(scrollTop).toBe(1200);
  });

  it("registers server-rendered tool cards by call id", async () => {
    const app = await connectPiApp();
    const card = document.createElement("div");
    card.className = "tool-card";
    card.dataset.tool = "bash";
    card.dataset.toolCallId = "call-1";
    app.termInner.append(card);

    app.adoptRenderedTranscript();

    expect(app.latestToolCards["call-1"]).toBe(card);
  });

  it("clears streaming tail and cancels pending render on transcript reset", async () => {
    let nextFrameId = 0;
    vi.spyOn(window, "requestAnimationFrame").mockImplementation(() => ++nextFrameId);
    const cancelAnimationFrame = vi.spyOn(window, "cancelAnimationFrame").mockImplementation(() => undefined);
    const app = await connectPiApp();
    app.appendDelta({ kind: "pi", delta: "pending" });
    const pendingFrame = app.streamingRenderFrame;

    expect(pendingFrame).toBeDefined();
    expect(app.querySelector(".streaming-tail .msg.streaming")).not.toBeNull();
    app.resetTranscriptWindow();

    expect(cancelAnimationFrame).toHaveBeenCalledWith(pendingFrame);
    expect(app.querySelector(".streaming-tail").childElementCount).toBe(0);
    expect(app.pendingStreamingRow).toBeUndefined();
    expect(app.streamingRenderFrame).toBeUndefined();
  });

  it("clears streaming state and history remains intact on repeated clear", async () => {
    const cancelAnimationFrame = vi.spyOn(window, "cancelAnimationFrame").mockImplementation(() => undefined);
    const app = await connectPiApp();
    const history = document.createElement("div");
    history.className = "msg";
    history.textContent = "history";
    app.termInner.append(history);
    app.transcriptItems = [app.createTranscriptItem(undefined, { nodes: [history] })];
    app.scrollFrame = undefined;
    app.streamingTail.append(document.createElement("div"));
    app.piDeltaBuffer = "```json";
    app.streamingRenderFrame = 0;

    app.clearStreamingTail();
    app.clearStreamingTail();

    expect(cancelAnimationFrame).toHaveBeenCalledWith(0);
    expect(app.streamingRenderFrame).toBeUndefined();
    expect(app.scrollFrame).toBeUndefined();
    expect(app.piDeltaBuffer).toBe("");
    expect(app.streamingTail.childElementCount).toBe(0);
    expect(app.transcriptItems).toHaveLength(1);
    expect(app.termInner.textContent).toContain("history");
  });

  it("clears both animation frame handles when resetting with zero-valued IDs", async () => {
    const cancelAnimationFrame = vi.spyOn(window, "cancelAnimationFrame").mockImplementation(() => undefined);
    const app = await connectPiApp();
    app.streamingRenderFrame = 0;
    app.scrollFrame = 0;

    app.resetTranscriptWindow();

    expect(cancelAnimationFrame).toHaveBeenCalledWith(0);
    expect(cancelAnimationFrame).toHaveBeenCalledTimes(2);
    expect(app.streamingRenderFrame).toBeUndefined();
    expect(app.scrollFrame).toBeUndefined();
  });

  it("keeps streamed assistant markup height changes outside the virtual scroller", async () => {
    const frames = [];
    vi.spyOn(window, "requestAnimationFrame").mockImplementation((callback) => {
      frames.push(callback);
      return frames.length;
    });

    let itemHeight = 80;
    vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockImplementation(function () {
      const height = this.classList?.contains("transcript-item") ? itemHeight : 0;
      return { x: 0, y: 0, width: 0, height, top: 0, right: 0, bottom: height, left: 0, toJSON: () => ({}) };
    });
    const app = await connectPiApp();
    Object.defineProperty(app.term, "clientHeight", { configurable: true, value: 600 });
    app.renderMessages([{ kind: "pi", text: "history" }]);
    app.appendDelta({ kind: "pi", delta: "start" });
    const heightChanged = vi.spyOn(app.transcriptVirtualScroller, "onItemHeightDidChange");

    itemHeight = 120;
    app.appendDelta({ kind: "pi", delta: "\nsecond line" });
    frames.splice(0).forEach((callback) => callback(0));

    expect(app.querySelector(".streaming-tail .msg.streaming .markdown-body").innerHTML).toContain("<br>");
    expect(heightChanged).not.toHaveBeenCalled();
  });

  it("syncs rendered item heights before updating virtual-scroller items", async () => {
    let itemHeight = 80;
    vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockImplementation(function () {
      const height = this.classList?.contains("transcript-item") ? itemHeight : 0;
      return { x: 0, y: 0, width: 0, height, top: 0, right: 0, bottom: height, left: 0, toJSON: () => ({}) };
    });
    const app = await connectPiApp();
    Object.defineProperty(app.term, "clientHeight", { configurable: true, value: 600 });
    app.renderMessages(Array.from({ length: 35 }, (_, index) => ({ kind: "pi", text: `message ${index}` })));
    const heightChanged = vi.spyOn(app.transcriptVirtualScroller, "onItemHeightDidChange");

    itemHeight = 120;
    app.appendMessage({ kind: "pi", text: "new tail" });

    expect(heightChanged).toHaveBeenCalled();
  });

  it("ignores sub-pixel transcript height jitter", async () => {
    let itemHeight = 35.6953125;
    vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockImplementation(function () {
      const height = this.classList?.contains("transcript-item") ? itemHeight : 0;
      return { x: 0, y: 0, width: 0, height, top: 0, right: 0, bottom: height, left: 0, toJSON: () => ({}) };
    });
    const app = await connectPiApp();
    Object.defineProperty(app.term, "clientHeight", { configurable: true, value: 600 });
    app.renderMessages([{ kind: "pi", text: "message" }]);
    const heightChanged = vi.spyOn(app.transcriptVirtualScroller, "onItemHeightDidChange");

    itemHeight = 35.6875;
    app.notifyTranscriptItemHeightDidChange(app.transcriptItems[0]);

    expect(app.transcriptItems[0].height).toBe(35.6875);
    expect(heightChanged).not.toHaveBeenCalled();
  });

  it("skips virtual-scroller notifications for unrendered transcript items", async () => {
    const app = await connectPiApp();
    Object.defineProperty(app.term, "clientHeight", { configurable: true, value: 600 });
    app.renderMessages(Array.from({ length: 35 }, (_, index) => ({ kind: "pi", text: `message ${index}` })));
    app.transcriptVisibleStart = 5;
    app.transcriptVisibleEnd = 35;
    const heightChanged = vi.spyOn(app.transcriptVirtualScroller, "onItemHeightDidChange");
    const staleElement = document.createElement("div");
    staleElement.className = "transcript-item";
    staleElement.dataset.transcriptItem = String(app.transcriptItems[0].id);
    app.termInner.append(staleElement);

    app.notifyTranscriptItemHeightDidChange(app.transcriptItems[0], staleElement);

    expect(heightChanged).not.toHaveBeenCalled();
  });

  it("covers transcript guard fallback paths", async () => {
    const frames = [];
    vi.spyOn(window, "requestAnimationFrame").mockImplementation((callback) => {
      frames.push(callback);
      return frames.length;
    });

    const app = await connectPiApp();
    const term = app.term;
    app.term = null;
    app.scrollTerm({ force: true });
    frames.splice(0).forEach((callback) => callback(0));
    expect(app.isTermPinnedToBottom()).toBe(true);
    app.term = term;

    app.transcriptItems = null;
    app.measureRenderedTranscriptItems();
    app.transcriptItems = [undefined, { nodes: [document.createElement("div")], height: 0 }];
    app.transcriptVisibleStart = 0;
    app.transcriptVisibleEnd = 2;
    app.measureRenderedTranscriptItems();

    const loading = document.createElement("div");
    const replacement = document.createElement("div");
    app.transcriptItems = [{ message: { kind: "pi", text: "not materialized" } }, { nodes: [loading] }];
    expect(() => app.removeTranscriptNode(loading)).not.toThrow();
    app.transcriptItems = [{ message: { kind: "pi", text: "not materialized" } }, { nodes: [loading] }];
    expect(app.replaceTranscriptNode(loading, replacement)).toBe(true);
    expect(app.transcriptItemHeight(9)).toBe(80);
  });

  it("always renders tool cards expanded with readable arguments", async () => {
    const app = await connectPiApp();
    const card = app.toolCard({ tool: "bash", args: JSON.stringify({ command: "bun test" }), status: "ok", body: "body" });
    const plainArgsCard = app.toolCard({ tool: "bash", args: "pwd", status: "ok" });
    const errorStatus = app.toolStatus({ status: "err" });
    const outputCard = app.toolCard({ tool: "pi", status: "running", body: "" });
    app.append(outputCard);
    app.appendToolOutput({ tool: "pi" });

    expect(card.dataset.tool).toBe("bash");
    expect(card.querySelector(".tc-body").hidden).toBe(false);
    expect(card.querySelector(".tc-args").textContent).toBe("bun test");
    expect(plainArgsCard.querySelector(".tc-args").textContent).toBe("pwd");
    expect(card.querySelector(".tc-head").dataset.action).toBeUndefined();
    expect(errorStatus).toContain("failed");
  });

  it("routes parallel same-name tool streams by call id", async () => {
    const app = await connectPiApp();
    app.renderMessages([]);
    const first = app.toolCard({ id: "call-1", tool: "bash", status: "running" });
    const second = app.toolCard({ id: "call-2", tool: "bash", status: "running" });
    app.append(first, second);

    app.updateToolArgs({ id: "call-1", tool: "bash", chunk: '{"command":"pwd"' });
    expect(first.querySelector(".tc-args").textContent).toBe("");
    app.updateToolArgs({ id: "call-1", tool: "bash", chunk: "}" });
    app.appendToolOutput({ id: "call-2", tool: "bash", chunk: "second output" });
    await new Promise((resolve) => requestAnimationFrame(resolve));

    expect(first.querySelector(".tc-args").textContent).toBe("pwd");
    expect(first.querySelector(".tc-body").textContent).toBe("");
    expect(second.querySelector(".tc-body").textContent).toContain("second output");
  });

  it("flushes pending streamed output before the tool ends", async () => {
    const app = await connectPiApp();
    app.renderMessages([]);
    app.appendMessage({ id: "call-1", kind: "tool", tool: "bash", status: "running" });
    app.appendToolOutput({ id: "call-1", tool: "bash", chunk: "last output" });

    app.finishTool({ id: "call-1", kind: "tool", tool: "bash", status: "ok", body: "" });

    expect(app.querySelector(".tool-card .tc-body").textContent).toContain("last output");
  });

  it("keeps streamed output when the tool end has no final body", async () => {
    const app = await connectPiApp();
    app.renderMessages([]);
    app.appendMessage({ id: "call-1", kind: "tool", tool: "bash", status: "running" });
    app.appendToolOutput({ id: "call-1", tool: "bash", chunk: "streamed output" });
    await new Promise((resolve) => requestAnimationFrame(resolve));

    app.finishTool({ id: "call-1", kind: "tool", tool: "bash", status: "ok", body: "" });

    expect(app.querySelector(".tool-card").dataset.status).toBe("ok");
    expect(app.querySelector(".tool-card .tc-body").textContent).toContain("streamed output");
  });

  it("previews the latest 4000 characters of live tool output", async () => {
    const app = await connectPiApp();
    app.renderMessages([]);
    app.appendMessage({ kind: "tool", tool: "bash", status: "running" });
    const output = `old-start\n${"x".repeat(20000)}\nlatest-marker`;

    app.appendToolOutput({ tool: "bash", chunk: output });
    await new Promise((resolve) => requestAnimationFrame(resolve));

    const body = app.querySelector(".tool-card .tc-body");
    expect(body.textContent).not.toContain("old-start");
    expect(body.textContent).toContain("latest-marker");
    expect(body.querySelector("[data-action='show-full-tool-output']")).not.toBeNull();
    expect(body.__fullToolBody).toBe(output);
  });

  it("updates the live preview with newly appended tool output", async () => {
    const app = await connectPiApp();
    app.renderMessages([]);
    app.appendMessage({ kind: "tool", tool: "bash", status: "running" });
    const output = `old-start\n${"x".repeat(20000)}`;

    app.appendToolOutput({ tool: "bash", chunk: output });
    await new Promise((resolve) => requestAnimationFrame(resolve));
    app.appendToolOutput({ tool: "bash", chunk: "newest-marker" });
    await new Promise((resolve) => requestAnimationFrame(resolve));

    const body = app.querySelector(".tool-card .tc-body");
    expect(body.textContent).toContain("newest-marker");
    expect(body.__fullToolBody).toContain("newest-marker");

    app.showFullToolOutput(body.querySelector("[data-action='show-full-tool-output']"));
    expect(body.textContent).toContain("old-start");
  });

  it("binds an existing scroll-bottom button", async () => {
    const app = await connectPiApp();
    const existingButton = document.createElement("button");
    existingButton.dataset.action = "scroll-bottom";
    app.transcriptScrollButton.remove();
    app.append(existingButton);

    expect(app.ensureTranscriptScrollButton()).toBe(existingButton);
  });

  it("loads older messages when the user scrolls near the top", async () => {
    const app = await connectPiApp();
    app.sessionHistoryHasMore = true;
    app.sessionHistoryLoading = false;
    app.loadOlderSessionMessages = vi.fn(() => {
      app.sessionHistoryLoading = true;
    });

    app.term.scrollTop = 0;
    app.handleTranscriptScroll();
    app.handleTranscriptScroll();

    expect(app.loadOlderSessionMessages).toHaveBeenCalledTimes(1);
  });

  it("forces programmatic bottom scrolls to skip smooth behavior", async () => {
    const frames = [];
    vi.spyOn(window, "requestAnimationFrame").mockImplementation((callback) => {
      frames.push(callback);
      return frames.length;
    });

    const app = await connectPiApp();
    frames.length = 0;
    app.scrollFrame = undefined;
    app.term.style.scrollBehavior = "smooth";
    let scrollTop = 0;
    const scrollWrites = [];
    Object.defineProperty(app.term, "scrollHeight", { configurable: true, value: 1000 });
    Object.defineProperty(app.term, "scrollTop", {
      configurable: true,
      get: () => scrollTop,
      set: (value) => {
        scrollWrites.push({ behavior: app.term.style.scrollBehavior, value });
        scrollTop = value;
      },
    });

    app.scrollTerm({ force: true });
    frames.splice(0).forEach((callback) => callback(0));
    frames.splice(0).forEach((callback) => callback(0));

    expect(scrollWrites).toEqual([
      { behavior: "auto", value: 1000 },
      { behavior: "auto", value: 1000 },
    ]);
    expect(app.term.style.scrollBehavior).toBe("smooth");
  });

  it("treats null animation-frame sentinels as available", async () => {
    const requestAnimationFrame = vi.spyOn(window, "requestAnimationFrame").mockReturnValue(7);
    const cancelAnimationFrame = vi.spyOn(window, "cancelAnimationFrame").mockImplementation(() => undefined);
    const app = await connectPiApp();
    app.streamingRenderFrame = null;
    app.scrollFrame = null;

    app.scheduleStreamingRender(document.createElement("div"));
    app.scrollTerm();

    expect(requestAnimationFrame).toHaveBeenCalledTimes(2);
    expect(app.streamingRenderFrame).toBe(7);
    expect(app.scrollFrame).toBe(7);

    app.streamingRenderFrame = null;
    app.scrollFrame = null;
    app.clearStreamingTail();
    app.cancelTranscriptScroll();

    expect(cancelAnimationFrame).not.toHaveBeenCalled();
  });

  it("does not let a queued trailing scroll override a user scroll-away", async () => {
    const frames = [];
    const cancelled = new Set();
    vi.spyOn(window, "requestAnimationFrame").mockImplementation((callback) => {
      const frame = { id: frames.length + 1, callback };
      frames.push(frame);
      return frame.id;
    });
    const cancelAnimationFrame = vi.spyOn(window, "cancelAnimationFrame").mockImplementation((id) => {
      cancelled.add(id);
    });

    const app = await connectPiApp();
    frames.length = 0;
    app.scrollFrame = undefined;
    let scrollTop = 900;
    Object.defineProperty(app.term, "clientHeight", { configurable: true, value: 100 });
    Object.defineProperty(app.term, "scrollHeight", { configurable: true, value: 1000 });
    Object.defineProperty(app.term, "scrollTop", {
      configurable: true,
      get: () => scrollTop,
      set: (value) => { scrollTop = value; },
    });

    app.scrollTerm();
    frames.shift().callback(0);
    scrollTop = 100;
    app.term.dispatchEvent(new WheelEvent("wheel", { deltaY: -100 }));
    app.term.dispatchEvent(new Event("scroll"));
    frames.splice(0).forEach(({ id, callback }) => {
      if (!cancelled.has(id)) callback(0);
    });

    expect(cancelAnimationFrame).toHaveBeenCalledWith(expect.any(Number));
    expect(scrollTop).toBe(100);
  });

  it("cancels pending transcript scrolling when the window resets", async () => {
    const requestAnimationFrame = vi.spyOn(window, "requestAnimationFrame").mockReturnValue(42);
    const cancelAnimationFrame = vi.spyOn(window, "cancelAnimationFrame").mockImplementation(() => undefined);
    const app = await connectPiApp();
    app.scrollFrame = undefined;

    app.scrollTerm();
    app.resetTranscriptWindow();

    expect(requestAnimationFrame).toHaveBeenCalled();
    expect(cancelAnimationFrame).toHaveBeenCalledWith(42);
    expect(app.scrollFrame).toBeUndefined();
  });

  it("resumes streaming bottom follow after returning with the bottom button", async () => {
    const app = await connectPiApp();
    app.userScrolledAway = true;
    app.transcriptFollowBottom = false;

    app.scrollTranscriptToBottom();

    expect(app.userScrolledAway).toBe(false);
    expect(app.transcriptFollowBottom).toBe(true);
  });

  it("stops following when the user scrolls up and resumes only from the bottom button", async () => {
    const frames = [];
    vi.spyOn(window, "requestAnimationFrame").mockImplementation((callback) => {
      frames.push(callback);
      return frames.length;
    });

    const app = await connectPiApp();
    Object.defineProperty(app.term, "clientHeight", { configurable: true, value: 100 });
    Object.defineProperty(app.term, "scrollHeight", { configurable: true, value: 1000 });
    app.renderMessages(Array.from({ length: 35 }, (_, index) => ({ kind: "pi", text: `message ${index}` })));
    frames.splice(0).forEach((callback) => callback(0));
    frames.splice(0).forEach((callback) => callback(0));

    app.term.scrollTop = 100;
    app.userGestureScroll = true;
    app.handleTranscriptScroll();
    app.scrollTerm();
    frames.splice(0).forEach((callback) => callback(0));

    expect(app.term.scrollTop).toBe(100);
    expect(app.transcriptScrollButton.hidden).toBe(false);

    app.transcriptScrollButton.click();
    frames.splice(0).forEach((callback) => callback(0));
    frames.splice(0).forEach((callback) => callback(0));

    expect(app.term.scrollTop).toBe(1000);
    expect(app.transcriptFollowBottom).toBe(true);
  });
});
