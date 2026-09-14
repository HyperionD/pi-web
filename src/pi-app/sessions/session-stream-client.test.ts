import { describe, expect, it, vi } from "vitest";
import {
  STREAM_RECONNECT_BASE_MS,
  STREAM_RECONNECT_MAX_MS,
  createSessionStreamClient,
  type EventSourceLike,
} from "./session-stream-client";

class FakeSource implements EventSourceLike {
  listeners = new Map<string, (message: { data?: string }) => void>();
  closed = false;

  constructor(public url: string) {}

  addEventListener(type: string, listener: (message: { data?: string }) => void) {
    this.listeners.set(type, listener);
  }

  emit(type: string, data?: unknown) {
    this.listeners.get(type)?.({ data: data === undefined ? undefined : JSON.stringify(data) });
  }

  close() {
    this.closed = true;
  }
}

function harness(appliedSeq = () => "7") {
  const sources: FakeSource[] = [];
  const timers: Array<{ callback: () => void; ms: number }> = [];
  const events: unknown[] = [];
  const resets: string[] = [];
  const errors: unknown[] = [];
  const opens: number[] = [];
  const client = createSessionStreamClient({
    sessionId: "s1",
    appliedSeq,
    onEvent: (event) => events.push(event),
    onReset: (reason) => resets.push(reason),
    onError: (error) => errors.push(error),
    onOpen: () => opens.push(1),
    createSource: (url) => {
      const source = new FakeSource(url);
      sources.push(source);
      return source;
    },
    setTimer: (callback, ms) => {
      timers.push({ callback, ms });
      return timers.length;
    },
    clearTimer: vi.fn(),
    baseUrl: () => "http://backend.test",
  });
  return {
    client,
    sources,
    timers,
    events,
    resets,
    errors,
    opens,
    runTimer(index = 0) {
      const timer = timers.splice(index, 1)[0];
      timer?.callback();
      return timer?.ms;
    },
  };
}

describe("session stream client", () => {
  it("opens after the reducer-applied sequence", () => {
    const api = harness();
    api.client.start();
    expect(api.sources[0].url).toBe("http://backend.test/api/sessions/s1/events-v2?after=7");
  });

  it("defaults to zero when no applied sequence exists", () => {
    const api = harness(() => "");
    api.client.start();
    expect(api.sources[0].url).toContain("after=0");
  });

  it("reports network recovery when the replacement stream opens", () => {
    const api = harness();
    api.client.start();
    api.sources[0].emit("error");
    expect(api.errors).toHaveLength(1);
    api.runTimer(0);
    api.sources[1].emit("open");
    expect(api.opens).toHaveLength(1);
  });

  it("reconnects from the applied sequence with bounded exponential backoff", () => {
    const api = harness();
    api.client.start();
    api.sources[0].emit("error");
    expect(api.timers[0].ms).toBe(STREAM_RECONNECT_BASE_MS);
    api.runTimer(0);
    expect(api.sources[1].url).toContain("after=7");
    api.sources[1].emit("error");
    expect(api.timers[0].ms).toBe(STREAM_RECONNECT_BASE_MS * 2);
  });

  it("caps the backoff delay", () => {
    const api = harness();
    api.client.start();
    for (let attempt = 0; attempt < 12; attempt += 1) {
      api.sources.at(-1)?.emit("error");
      api.runTimer(0);
    }
    api.sources.at(-1)?.emit("error");
    expect(api.timers[0].ms).toBe(STREAM_RECONNECT_MAX_MS);
  });

  it("parses committed events and forwards them", () => {
    const api = harness();
    api.client.start();
    api.sources[0].emit("session.event", { seq: "7", sessionId: "s1", type: "run.started" });
    expect(api.events).toEqual([{ seq: "7", sessionId: "s1", type: "run.started" }]);
  });

  it("resets on malformed payloads and reconnects", () => {
    const api = harness();
    api.client.start();
    api.sources[0].emit("session.event", undefined);
    api.sources[0].listeners.get("session.event")?.({ data: "{" });
    expect(api.errors.length).toBeGreaterThan(0);
    api.runTimer(0);
    expect(api.sources.length).toBeGreaterThan(1);
  });

  it("handles stream reset with a reason and stops reconnecting", () => {
    const api = harness();
    api.client.start();
    api.sources[0].emit("stream.reset", { type: "stream.reset", payload: { reason: "cursor_expired" } });
    expect(api.resets).toEqual(["cursor_expired"]);
    api.sources[0].emit("stream.reset", undefined);
    api.sources[0].emit("stream.reset", { type: "stream.reset", payload: {} });
    expect(api.resets).toEqual(["cursor_expired"]);
  });

  it("honours the fallback reason for a non-JSON reset payload", () => {
    const api = harness();
    api.client.start();
    api.sources[0].listeners.get("stream.reset")?.({ data: "not-json" });
    expect(api.resets).toEqual(["cursor_expired"]);
  });

  it("resets the backoff after a heartbeat", () => {
    const api = harness();
    api.client.start();
    api.sources[0].emit("error");
    api.runTimer(0);
    api.sources[1].emit("heartbeat", {});
    api.sources[1].emit("error");
    expect(api.timers[0].ms).toBe(STREAM_RECONNECT_BASE_MS);
  });

  it("stops and resets without opening when inactive", () => {
    const api = harness();
    api.client.start();
    api.client.stop();
    expect(api.sources[0].closed).toBe(true);
    api.client.reset();
    expect(api.sources).toHaveLength(1);
    expect(api.client.nextDelay()).toBe(STREAM_RECONNECT_BASE_MS);
  });

  it("cancels a scheduled reconnect timer on stop", () => {
    const api = harness();
    api.client.start();
    api.sources[0].emit("error");
    expect(api.timers).toHaveLength(1);
    api.client.stop();
    expect(api.sources[0].closed).toBe(true);
  });

  it("reopens immediately on reset while active", () => {
    const api = harness();
    api.client.start();
    api.client.reset();
    expect(api.sources).toHaveLength(2);
    expect(api.sources[1].url).toContain("after=7");
  });

  it("ignores late events after stop", () => {
    const api = harness();
    api.client.start();
    api.client.stop();
    api.sources[0].emit("session.event", { seq: "8", sessionId: "s1", type: "run.finished" });
    expect(api.events).toHaveLength(0);
  });

  it("ignores callbacks from a replaced source", () => {
    const api = harness();
    api.client.start();
    const first = api.sources[0];
    api.client.reset();
    const second = api.sources[1];

    first.emit("error");
    first.emit("stream.reset", { type: "stream.reset", payload: { reason: "cursor_expired" } });
    second.emit("session.event", { seq: "8", sessionId: "s1", type: "run.finished" });

    expect(second.closed).toBe(false);
    expect(api.events).toEqual([{ seq: "8", sessionId: "s1", type: "run.finished" }]);
    expect(api.resets).toHaveLength(0);
    expect(api.timers).toHaveLength(0);
  });

  it("does not close the replacement source after a parse error from the old source", () => {
    const api = harness();
    api.client.start();
    const first = api.sources[0];
    api.client.reset();
    const second = api.sources[1];

    first.listeners.get("session.event")?.({ data: "{" });

    expect(first.closed).toBe(true);
    expect(second.closed).toBe(false);
    expect(api.errors).toHaveLength(0);
  });

  it("uses browser EventSource and timer defaults", () => {
    vi.useFakeTimers();
    const created: string[] = [];
    const sources: BrowserSource[] = [];
    class BrowserSource implements EventSourceLike {
      listeners = new Map<string, (message: { data?: string }) => void>();
      constructor(public url: string) {
        created.push(url);
        sources.push(this);
      }
      addEventListener(type: string, listener: (message: { data?: string }) => void) {
        this.listeners.set(type, listener);
      }
      emit(type: string) {
        this.listeners.get(type)?.({});
      }
      close() {}
    }
    vi.stubGlobal("EventSource", BrowserSource);
    const client = createSessionStreamClient({
      sessionId: "s1",
      appliedSeq: () => "1",
      onEvent: vi.fn(),
      onReset: vi.fn(),
    });
    client.start();
    sources[0].emit("error");
    vi.advanceTimersByTime(STREAM_RECONNECT_BASE_MS);
    sources[1].emit("error");
    client.stop();
    vi.useRealTimers();
    vi.unstubAllGlobals();
    expect(created).toEqual([
      "/api/sessions/s1/events-v2?after=1",
      "/api/sessions/s1/events-v2?after=1",
    ]);
  });
});
