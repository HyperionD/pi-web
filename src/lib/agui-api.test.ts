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
  it("does not treat streamed tool arguments as tool output", async () => {
    runAgent.mockImplementation(async (_run, handlers) => {
      handlers.onToolCallStartEvent({ event: { toolCallId: "call-1", toolCallName: "bash" } });
      handlers.onToolCallArgsEvent({
        event: { toolCallId: "call-1", delta: '{"command":"pwd"}' },
        toolCallName: "bash",
      });
      handlers.onToolCallResultEvent({ event: { toolCallId: "call-1", content: "partial" } });
      handlers.onToolCallEndEvent({
        event: { toolCallId: "call-1" },
        toolCallName: "bash",
        toolCallArgs: { command: "pwd" },
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
    expect(subscriber.onToolResult).toHaveBeenCalledWith({ id: "call-1", name: "bash", content: "result" });
  });
});
