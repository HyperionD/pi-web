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
    this.after = BigInt(new URL(url, window.location.href).searchParams.get("after") || "0");
    MockEventSource.instances.push(this);
    window.__mockSources = MockEventSource.instances;
    setTimeout(() => {
      for (const event of window.__mockCommitted || []) {
        if (BigInt(event.seq) > this.after && !this.closed) this.dispatch("session.event", event, event.seq);
      }
    }, 0);
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
window.__mockCommitted = [];
window.__mockSources = [];
window.MockEventSource = MockEventSource;
window.EventSource = MockEventSource;
`;

function projectItems(committed: Array<Record<string, any>>) {
  const items = new Map<string, Record<string, any>>();
  const order: string[] = [];
  let status = "idle";
  let runId: string | undefined;
  const upsert = (item: Record<string, any>) => {
    if (!items.has(item.itemId)) order.push(item.itemId);
    items.set(item.itemId, { ...items.get(item.itemId), ...item });
  };
  for (const event of committed) {
    const payload = event.payload || {};
    if (event.type === "run.started") {
      status = "running";
      runId = event.runId;
    } else if (event.type === "user.message.created") {
      upsert({ itemId: `user:${event.seq}`, ordinal: order.length + 1, kind: "user", origin: "runtime", text: payload.text || "" });
    } else if (event.type === "assistant.segment.started" || event.type === "assistant.segment.delta" || event.type === "assistant.segment.committed") {
      const itemId = event.entityId;
      upsert({
        itemId,
        ordinal: order.length + 1,
        kind: payload.kind === "think" ? "think" : "pi",
        origin: "runtime",
        runId: event.runId,
        entityId: itemId,
        text: event.type === "assistant.segment.committed" ? payload.text || "" : `${items.get(itemId)?.text || ""}${payload.delta || ""}`,
        status: event.type === "assistant.segment.committed" ? "committed" : "streaming",
      });
    } else if (event.type === "tool.started" || event.type === "tool.output" || event.type === "tool.finished") {
      const itemId = event.entityId;
      const existing = items.get(itemId) || {};
      upsert({
        itemId,
        ordinal: existing.ordinal || order.length + 1,
        kind: "tool",
        origin: "runtime",
        runId: event.runId,
        entityId: itemId,
        tool: payload.tool || existing.tool || "bash",
        toolCallId: payload.toolCallId || existing.toolCallId || itemId.split(":").slice(2).join(":"),
        args: payload.args || existing.args || "",
        body: event.type === "tool.finished" ? payload.body || "" : existing.body || (event.type === "tool.output" ? `${existing.body || ""}${payload.chunk || ""}` : ""),
        status: event.type === "tool.finished" ? payload.status || "ok" : "running",
      });
    } else if (event.type === "run.finished") {
      status = "idle";
      runId = undefined;
    }
  }
  return { items: order.map((id) => items.get(id)), status, runId };
}

export async function installSessionStreamFixture(
  page: Page,
  boundary: LifecycleBoundary,
  options: { seedTranscript?: boolean } = {},
): Promise<SessionStreamFixture> {
  await page.addInitScript(MOCK_EVENT_SOURCE);

  let seq = options.seedTranscript ? 1 : 0;
  const committed: Array<Record<string, any>> = options.seedTranscript
    ? [{
        seq: "1",
        sessionId: "e2e-session",
        type: "user.message.created",
        sourceId: "history:user",
        payload: { text: "prior message" },
      }]
    : [];
  const waitForStream = () => page.waitForFunction(
    () => customElements.get("pi-app") && (window as any).__mockSources?.some((source) => !source.closed),
  );
  const push = async (event: Record<string, unknown>) => {
    seq += 1;
    const stored = { ...event, seq: String(seq), sessionId: "e2e-session" };
    committed.push(stored);
    await page.evaluate((events) => {
      window.__mockCommitted = events;
      for (const source of window.__mockSources || []) {
        if (!source.closed) source.dispatch("session.event", events.at(-1), events.at(-1).seq);
      }
    }, committed);
  };

  await page.route("**/api/**", async (route) => {
    const url = route.request().url();
    if (url.includes("/events-v2") || url.includes("/events")) {
      await route.fulfill({ status: 200, contentType: "text/event-stream", body: "" });
      return;
    }
    if (url.includes("/api/account/me")) {
      await route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({ user: { id: "e2e-user", username: "e2e", role: "admin" } }) });
      return;
    }
    if (url.includes("/api/app-config")) {
      await route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({ config: { version: 1, brand: "pi.dev / web", workspace: { allowedRoots: [], maxUploadBytes: 104857600 } }, path: "e2e" }) });
      return;
    }
    if (url.includes("/api/prompt-presets")) {
      await route.fulfill({ json: { presets: [] } });
      return;
    }
    if (url.includes("/api/health")) {
      await route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({ ok: true, durableEvents: true }) });
      return;
    }
    if (url.includes("/api/workspaces") && !url.includes("/api/workspaces/w1/")) {
      await route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({ workspaces: [{ id: "w1", name: "e2e", path: "/e2e", title: "e2e", sessionCount: 1, sessions: [{ id: "e2e-session", title: "e2e", lastUsed: "now" }] }] }) });
      return;
    }
    if (url.includes("/state")) {
      const projected = projectItems(committed);
      await route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({
        session: { id: "e2e-session", title: "e2e", workspaceId: "w1" },
        items: projected.items, status: projected.status, runId: projected.runId,
        seq: String(seq), cursor: "", hasMore: false, limit: 120,
        projectionSchemaVersion: 1, projectionGeneration: "1",
      }) });
      return;
    }
    if (url.includes("/content")) {
      await route.fulfill({ status: 200, contentType: "application/octet-stream", body: "final bash output" });
      return;
    }
    await route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({ accepted: true, runId: "run-1" }) });
  });

  await page.goto("/");
  await waitForStream();

  return {
    async runUntilBoundary() {
      await push({ type: "run.started", runId: "run-1" });
      await push({ type: "assistant.segment.started", entityId: "assistant:run-1:1", payload: { kind: "pi" } });
      await push({ type: "assistant.segment.delta", entityId: "assistant:run-1:1", payload: { delta: "hello" } });
      if (boundary === "run-started" || boundary === "assistant-delta") return;
      await push({ type: "tool.started", entityId: "tool:run-1:call-1", payload: { tool: "bash", toolCallId: "call-1" } });
      if (boundary === "tool-start") return;
      await push({ type: "tool.output", entityId: "tool:run-1:call-1", payload: { chunk: "partial output", mode: "append" } });
      if (boundary === "tool-output") return;
      await push({ type: "tool.finished", entityId: "tool:run-1:call-1", payload: { body: "final bash output", status: "ok" } });
      if (boundary === "tool-finish") return;
      await push({ type: "assistant.segment.committed", entityId: "assistant:run-1:1", payload: { kind: "pi", text: "hello" } });
      await push({ type: "run.finished", runId: "run-1" });
    },
    async finishRun() {
      await waitForStream();
      if (!committed.some((event) => event.type === "tool.finished")) {
        await push({ type: "tool.finished", entityId: "tool:run-1:call-1", payload: { body: "final bash output", status: "ok" } });
      }
      if (!committed.some((event) => event.type === "run.finished")) {
        await push({ type: "assistant.segment.committed", entityId: "assistant:run-1:1", payload: { kind: "pi", text: "hello" } });
        await push({ type: "run.finished", runId: "run-1" });
      }
    },
  };
}
