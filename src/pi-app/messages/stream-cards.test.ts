// @ts-nocheck
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { applyUiLocale } from "../../i18n/client";
import { UI_LOCALE_STORAGE_KEY } from "../../i18n/locales";
import { emptyTranscriptState } from "../sessions/transcript-state";
import { cleanupPiAppFixture, connectPiApp, installPiAppFixture } from "../test-helper";

function toggleCard(card, open) {
  card.open = open;
  card.dispatchEvent(new Event("toggle"));
}

function applyThinkingEvent(app, seq, type, payload) {
  app.applyDurableEvent({
    sessionId: "s1", seq: String(seq), type, entityId: "thinking:run-1:1", payload,
  });
  app.flushDurableTranscriptRender();
}

describe("OpenDesign transcript stream cards", () => {
  beforeEach(() => {
    installPiAppFixture();
    localStorage.setItem(UI_LOCALE_STORAGE_KEY, "zh-CN");
  });
  afterEach(cleanupPiAppFixture);

  it("renders completed thinking as a closed disclosure with escaped content and a lightbulb icon", async () => {
    const app = await connectPiApp();
    app.runtimeStatus.model = "GPT-5.5";
    const row = app.thinkingMessageNode({ kind: "think", text: "<script>plan</script>\nnext" });
    const card = row.querySelector("details.thinking-block.thinking-card");

    expect(card).not.toBeNull();
    expect(card.open).toBe(false);
    expect(card.querySelector("summary .label").textContent).toBe("思考过程");
    expect(card.querySelector(".thinking-state").textContent).toBe("已完成 · 可展开查看");
    expect(card.querySelector(".thinking-copy").textContent).toBe("<script>plan</script>\nnext");
    expect(card.querySelector("script")).toBeNull();
    expect(row.dataset.rawText).toBe("<script>plan</script>\nnext");
    expect(row.querySelector(".prefix").textContent).toBe("");
    expect(row.querySelector("[data-message-icon='lightbulb']")).not.toBeNull();
  });

  it("keeps legacy thinking deltas inside the open content region, not the summary", async () => {
    const app = await connectPiApp();
    app.renderMessages([]);
    app.appendStreamingDelta("think", "first");
    app.appendStreamingDelta("think", "\nsecond");
    const card = app.querySelector("details.thinking-card");

    expect(card.open).toBe(true);
    expect(card.querySelector(".thinking-state").textContent).toBe("思考中");
    expect(card.querySelector(".thinking-copy").textContent).toBe("first\nsecond");
    expect(card.querySelector("summary").textContent).not.toContain("first");
  });

  it("renders an open command card with a separate command line and bounded output region", async () => {
    const app = await connectPiApp();
    const command = "printf '<script>unsafe</script>'";
    const card = app.toolCard({
      kind: "tool", tool: "functions.bash", args: JSON.stringify({ command }),
      status: "running", body: "<img src=x onerror=alert(1)>\nnext",
    });

    expect(card.matches("details.tool-card.command-card.stream-card")).toBe(true);
    expect(card.open).toBe(true);
    expect(card.querySelector("summary .tc-title").textContent).toBe("命令执行");
    expect(card.querySelector("summary .tc-name").textContent).toBe("functions.bash");
    expect(card.querySelector("summary .tc-args")).toBeNull();
    expect(card.querySelector(".tc-command .tc-prompt").textContent).toBe("$");
    expect(card.querySelector(".tc-command .tc-args").textContent).toBe(command);
    expect(card.querySelector(".tc-meta .stream-state.running").textContent).toBe("运行中");
    expect(card.querySelector(".tc-output-label").textContent).toBe("实时输出");
    expect(card.querySelector(".tc-body").getAttribute("aria-label")).toBe("实时输出");
    expect(card.querySelector(".tc-body").textContent).toBe("<img src=x onerror=alert(1)>\nnext");
    expect(card.querySelector("img, script")).toBeNull();
  });

  it("retains non-shell tool names and icons without a shell prompt", async () => {
    const app = await connectPiApp();
    const card = app.toolCard({ tool: "read", args: JSON.stringify({ path: "src/App.astro" }), status: "ok" });

    expect(card.querySelector(".tc-title").textContent).toBe("工具执行");
    expect(card.querySelector(".tc-name").textContent).toBe("read");
    expect(card.querySelector("[data-tool-icon='book-open']")).not.toBeNull();
    expect(card.querySelector(".tc-prompt").textContent).toBe("→");
    expect(card.querySelector(".tc-args").textContent).toBe("src/App.astro");
  });

  it("reveals streamed arguments once a readable command becomes available", async () => {
    const app = await connectPiApp();
    app.renderMessages([{ kind: "tool", id: "call-1", tool: "bash", status: "running" }]);
    const card = app.querySelector(".tool-card");
    expect(card.querySelector(".tc-command").hidden).toBe(true);

    app.updateToolArgs({ id: "call-1", tool: "bash", chunk: '{"command":"bun test"}' });

    expect(card.querySelector(".tc-command").hidden).toBe(false);
    expect(card.querySelector(".tc-args").textContent).toBe("bun test");
  });

  it("shows localized success and failure states while escaping result metadata", async () => {
    const app = await connectPiApp();
    const success = app.toolCard({ tool: "bash", status: "ok", durationMs: 0, resultMeta: "<done>" });
    const failure = app.toolCard({ tool: "bash", status: "err", resultMeta: "<script>failed</script>" });

    expect(success.querySelector(".stream-state.ok").textContent).toBe("已完成");
    expect(success.querySelector(".tc-meta").textContent).toContain("0 ms");
    expect(success.querySelector(".tc-meta").textContent).toContain("<done>");
    expect(failure.querySelector(".stream-state.err").textContent).toBe("执行失败");
    expect(failure.querySelector(".tc-meta").textContent).toContain("<script>failed</script>");
    expect(failure.querySelector("script")).toBeNull();
  });

  it("relocalizes card titles, completion hints, and output labels", async () => {
    const app = await connectPiApp();
    app.renderMessages([
      { kind: "think", text: "plan" },
      { kind: "tool", tool: "bash", status: "ok" },
    ]);
    applyUiLocale("en", app);

    expect(app.querySelector(".thinking-state").textContent).toBe("Completed · Expand to view");
    expect(app.querySelector(".tc-title").textContent).toBe("Command execution");
    expect(app.querySelector(".stream-state.ok").textContent).toBe("Completed");
    expect(app.querySelector(".tc-output-label").textContent).toBe("Live output");
    expect(app.querySelector(".tc-body").getAttribute("aria-label")).toBe("Live output");
  });

  it("remembers disclosure state when a virtualized card remounts and notifies height changes", async () => {
    const app = await connectPiApp();
    app.renderMessages([{ kind: "tool", id: "call-1", tool: "bash", status: "running" }]);
    const heightChanged = vi.spyOn(app, "notifyTranscriptNodeHeightDidChange");
    toggleCard(app.querySelector(".tool-card"), false);

    expect(heightChanged).toHaveBeenCalled();
    expect(app.renderVirtualTranscriptItem(app.transcriptItems[0]).querySelector(".tool-card").open).toBe(false);
  });

  it("retains a collapsed command during durable argument, output, and completion patches", async () => {
    const app = await connectPiApp();
    app.renderMessages([{ kind: "tool", key: "tool:1", tool: "bash", status: "running" }]);
    const wrapper = app.querySelector(".transcript-item");
    const card = wrapper.querySelector(".tool-card");
    toggleCard(card, false);
    const body = card.querySelector(".tc-body");
    const next = { kind: "tool", key: "tool:1", tool: "bash", args: "bun test", status: "ok", body: "passed" };
    app.patchDurableTranscriptMessages([next]);

    expect(wrapper.querySelector(".tool-card")).toBe(card);
    expect(card.open).toBe(false);
    expect(card.querySelector(".tc-command").hidden).toBe(false);
    expect(card.querySelector(".tc-body")).toBe(body);
    expect(body.textContent).toBe("passed");
    expect(card.querySelector(".stream-state.ok").textContent).toBe("已完成");
  });

  it("keeps thinking text stable across durable deltas and preserves an explicit collapse at commit", async () => {
    const app = await connectPiApp();
    app.dataset.activeSessionId = "s1";
    app.transcriptState = emptyTranscriptState("s1");
    app.renderDurableTranscript();
    applyThinkingEvent(app, 1, "assistant.segment.started", { kind: "think" });
    const card = app.querySelector(".thinking-card");
    const copy = card.querySelector(".thinking-copy");
    toggleCard(card, false);
    applyThinkingEvent(app, 2, "assistant.segment.delta", { kind: "think", delta: "plan" });

    expect(app.querySelector(".thinking-copy")).toBe(copy);
    expect(copy.textContent).toBe("plan");
    expect(card.open).toBe(false);
    applyThinkingEvent(app, 3, "assistant.segment.committed", { kind: "think", text: "plan complete" });
    expect(app.querySelector(".thinking-card").open).toBe(false);
    expect(app.querySelector(".thinking-copy").textContent).toBe("plan complete");
    expect(app.querySelector(".thinking-state").textContent).toBe("已完成 · 可展开查看");
  });

  it("closes completed thinking by default but retains the reader's explicit expanded choice", async () => {
    const app = await connectPiApp();
    app.renderMessages([{ kind: "think", key: "think:1", text: "plan", status: "streaming" }]);
    app.patchDurableTranscriptMessages([{ kind: "think", key: "think:1", text: "done", status: "committed" }]);
    expect(app.querySelector(".thinking-card").open).toBe(false);
    toggleCard(app.querySelector(".thinking-card"), true);
    app.patchDurableTranscriptMessages([{ kind: "think", key: "think:1", text: "final", status: "committed" }]);
    expect(app.querySelector(".thinking-card").open).toBe(true);
  });

  it("preserves a reader's collapsed legacy tool after completion and later message rendering", async () => {
    const app = await connectPiApp();
    app.renderMessages([{ kind: "tool", id: "call-1", tool: "bash", status: "running" }]);
    toggleCard(app.querySelector(".tool-card"), false);
    app.finishTool({ kind: "tool", id: "call-1", tool: "bash", status: "ok", body: "done" });
    expect(app.querySelector(".tool-card").open).toBe(false);
    app.appendMessage({ kind: "pi", text: "next reply" });
    expect(app.querySelector(".tool-card").open).toBe(false);
  });
});
