// @ts-nocheck
import { describe, expect, it, vi } from "vitest";

const runAgent = vi.hoisted(() => vi.fn());

vi.mock("@ag-ui/client", () => ({
  HttpAgent: class {
    async runAgent(...args) {
      return runAgent(...args);
    }
  },
}));

import { runAguiSessionPrompt } from "./api";

describe("AG-UI API adapter", () => {
  it("forwards the consumed Broker cursor from AG-UI custom events", async () => {
    runAgent.mockImplementation(async (_run, handlers) => {
      handlers.onCustomEvent({
        event: { name: "piweb.broker-cursor", value: { eventId: "9007199254740993" } },
      });
    });
    const subscriber = { onBrokerEventCursor: vi.fn() };

    vi.stubGlobal("EventSource", class {});
    globalThis.PI_WEB_API_BASE = "http://backend.test";
    await runAguiSessionPrompt("session", "prompt", [], subscriber);

    expect(subscriber.onBrokerEventCursor).toHaveBeenCalledWith("9007199254740993");
  });

  it("preserves append semantics from AG-UI tool metadata", async () => {
    runAgent.mockImplementation(async (_run, handlers) => {
      handlers.onCustomEvent({
        event: { name: "piweb.tool-output", value: { toolCallId: "call-1", mode: "append" } },
      });
      handlers.onToolCallResultEvent({ event: { toolCallId: "call-1", content: "streamed output" } });
    });
    const subscriber = { onToolResult: vi.fn() };

    vi.stubGlobal("EventSource", class {});
    globalThis.PI_WEB_API_BASE = "http://backend.test";
    await runAguiSessionPrompt("session", "prompt", [], subscriber);

    expect(subscriber.onToolResult).toHaveBeenCalledWith({
      id: "call-1",
      name: "tool",
      content: "streamed output",
      mode: "append",
    });
  });

  it("preserves replacement semantics for cumulative tool results", async () => {
    runAgent.mockImplementation(async (_run, handlers) => {
      handlers.onCustomEvent({
        event: { name: "piweb.tool-output", value: { toolCallId: "call-1", mode: "replace" } },
      });
      handlers.onToolCallResultEvent({ event: { toolCallId: "call-1", content: "latest output" } });
    });
    const subscriber = { onToolResult: vi.fn() };

    vi.stubGlobal("EventSource", class {});
    globalThis.PI_WEB_API_BASE = "http://backend.test";
    await runAguiSessionPrompt("session", "prompt", [], subscriber);

    expect(subscriber.onToolResult).toHaveBeenCalledWith({
      id: "call-1",
      name: "tool",
      content: "latest output",
      mode: "replace",
    });
  });

  it("does not treat streamed tool arguments as tool output", async () => {
    runAgent.mockImplementation(async (_run, handlers) => {
      handlers.onToolCallStartEvent({ event: { toolCallId: "call-1", toolCallName: "bash" } });
      handlers.onToolCallArgsEvent({
        event: { toolCallId: "call-1", delta: '{"command":"pwd"}' },
        toolCallName: "bash",
      });
      handlers.onCustomEvent({
        event: { name: "piweb.tool-output", value: { toolCallId: "call-1", mode: "append" } },
      });
      handlers.onToolCallResultEvent({ event: { toolCallId: "call-1", content: "partial" } });
      handlers.onToolCallEndEvent({
        event: { toolCallId: "call-1" },
        toolCallName: "bash",
        toolCallArgs: { command: "pwd" },
      });
      handlers.onCustomEvent({
        event: { name: "piweb.tool-output", value: { toolCallId: "call-1", mode: "replace" } },
      });
      handlers.onToolCallResultEvent({ event: { toolCallId: "call-1", content: "result" } });
    });
    const subscriber = {
      onToolEnd: vi.fn(),
      onToolArgs: vi.fn(),
      onToolResult: vi.fn(),
    };

    vi.stubGlobal("EventSource", class {});
    globalThis.PI_WEB_API_BASE = "http://backend.test";
    await runAguiSessionPrompt("session", "prompt", [], subscriber);

    expect(subscriber.onToolArgs).toHaveBeenCalledWith({ id: "call-1", name: "bash", chunk: '{"command":"pwd"}' });
    expect(subscriber.onToolEnd).toHaveBeenCalledWith({
      id: "call-1",
      name: "bash",
      args: '{"command":"pwd"}',
      body: "",
    });
    expect(subscriber.onToolResult).toHaveBeenLastCalledWith({
      id: "call-1",
      name: "bash",
      content: "result",
      mode: "replace",
    });
  });
});
