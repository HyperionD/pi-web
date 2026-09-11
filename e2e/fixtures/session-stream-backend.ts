import type { Page } from "@playwright/test";

export type LifecycleBoundary =
  | "run-started"
  | "assistant-delta"
  | "tool-start"
  | "tool-output"
  | "tool-finish"
  | "run-finish";

export type SessionStreamFixture = {
  runUntilBoundary: () => Promise<void>;
  finishRun: () => Promise<void>;
};

const MOCK_EVENT_SOURCE = `
class MockEventSource {
  static instances = [];
  constructor(url) {
    this.url = url;
    this.listeners = new Map();
    this.closed = false;
    MockEventSource.instances.push(this);
    window.__mockSources = MockEventSource.instances;
  }
  addEventListener(type, listener) {
    if (!this.listeners.has(type)) this.listeners.set(type, []);
    this.listeners.get(type).push(listener);
  }
  dispatch(type, payload, id) {
    const event = { data: JSON.stringify(payload), lastEventId: id === undefined ? "" : String(id) };
    for (const listener of this.listeners.get(type) || []) listener(event);
  }
  emitError() {
    for (const listener of this.listeners.get("error") || []) listener({ type: "error" });
  }
  close() {
    this.closed = true;
  }
}
window.MockEventSource = MockEventSource;
window.EventSource = MockEventSource;
`;

export async function installSessionStreamFixture(
  page: Page,
  boundary: LifecycleBoundary,
): Promise<SessionStreamFixture> {
  await page.addInitScript(MOCK_EVENT_SOURCE);

  let seq = 0;
  const committed: Array<Record<string, unknown>> = [];
  const push = (event: Record<string, unknown>) => {
    seq += 1;
    committed.push({ ...event, seq: String(seq), sessionId: "e2e-session" });
  };
  const projectState: Record<string, unknown> = {
    session: { id: "e2e-session", title: "e2e" },
    items: [],
    status: "idle",
    runId: undefined,
    seq: "0",
    cursor: "",
    hasMore: false,
    limit: 120,
    projectionSchemaVersion: 1,
    projectionGeneration: "1",
  };

  await page.route("**/api/**", async (route) => {
    const url = route.request().url();
    if (url.includes("/events-v2") || url.includes("/events")) {
      await route.fulfill({ status: 200, contentType: "text/event-stream", body: "" });
      return;
    }
    if (url.includes("/state")) {
      await route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(projectState) });
      return;
    }
    if (url.includes("/content")) {
      await route.fulfill({ status: 200, contentType: "application/octet-stream", body: "final bash output" });
      return;
    }
    await route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({ accepted: true, runId: "run-1" }) });
  });

  await page.goto("/");

  return {
    async runUntilBoundary() {
      push({ type: "run.started", runId: "run-1" });
      push({ type: "assistant.segment.started", entityId: "assistant:run-1:1", payload: { kind: "pi" } });
      push({ type: "assistant.segment.delta", entityId: "assistant:run-1:1", payload: { delta: "hello" } });
      if (boundary === "run-started" || boundary === "assistant-delta") return;
      push({ type: "tool.started", entityId: "tool:run-1:call-1", payload: { tool: "bash", toolCallId: "call-1" } });
      if (boundary === "tool-start") return;
      push({ type: "tool.output", entityId: "tool:run-1:call-1", payload: { chunk: "partial output", mode: "append" } });
      if (boundary === "tool-output") return;
      push({ type: "tool.finished", entityId: "tool:run-1:call-1", payload: { body: "final bash output", status: "ok" } });
      if (boundary === "tool-finish") return;
      push({ type: "assistant.segment.committed", entityId: "assistant:run-1:1", payload: { kind: "pi", text: "hello" } });
      push({ type: "run.finished", runId: "run-1" });
    },
    async finishRun() {
      if (!committed.some((event) => event.type === "tool.finished")) {
        push({ type: "tool.finished", entityId: "tool:run-1:call-1", payload: { body: "final bash output", status: "ok" } });
      }
      if (!committed.some((event) => event.type === "run.finished")) {
        push({ type: "assistant.segment.committed", entityId: "assistant:run-1:1", payload: { kind: "pi", text: "hello" } });
        push({ type: "run.finished", runId: "run-1" });
      }
    },
  };
}
