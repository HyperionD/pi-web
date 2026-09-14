import type { DurableSessionEvent } from "../sessions/transcript-state";

export const STREAM_RECONNECT_BASE_MS = 250;
export const STREAM_RECONNECT_MAX_MS = 5000;

export type EventSourceLike = {
  addEventListener: (type: string, listener: (message: { data?: string }) => void) => void;
  close: () => void;
};

export type SessionStreamClientOptions = {
  sessionId: string;
  appliedSeq: () => string;
  onEvent: (event: DurableSessionEvent) => void;
  onReset: (reason: string) => void;
  onOpen?: () => void;
  onError?: (error: unknown) => void;
  createSource?: (url: string) => EventSourceLike;
  setTimer?: (callback: () => void, ms: number) => number;
  clearTimer?: (handle: number) => void;
  baseUrl?: () => string;
};

export type SessionStreamClient = {
  start: (initialSeq?: string) => void;
  stop: () => void;
  reset: () => void;
  nextDelay: () => number;
};

export function createSessionStreamClient(options: SessionStreamClientOptions): SessionStreamClient {
  const createSource = options.createSource || ((url: string) => new EventSource(url) as unknown as EventSourceLike);
  const setTimer = options.setTimer || ((callback, ms) => window.setTimeout(callback, ms));
  const clearTimer = options.clearTimer || ((handle) => window.clearTimeout(handle));
  const baseUrl = options.baseUrl || (() => "");
  let source: EventSourceLike | undefined;
  let timer: number | undefined;
  let delay = STREAM_RECONNECT_BASE_MS;
  let active = false;

  const cancelTimer = () => {
    if (timer !== undefined) {
      clearTimer(timer);
      timer = undefined;
    }
  };

  const closeSource = () => {
    source?.close();
    source = undefined;
  };

  const scheduleReconnect = () => {
    if (!active || timer !== undefined) return;
    const wait = delay;
    delay = Math.min(delay * 2, STREAM_RECONNECT_MAX_MS);
    timer = setTimer(() => {
      timer = undefined;
      open();
    }, wait);
  };

  const open = () => {
    const seq = options.appliedSeq() || "0";
    const url = `${baseUrl()}/api/sessions/${encodeURIComponent(options.sessionId)}/events-v2?after=${seq}`;
    const next = createSource(url);
    source = next;
    next.addEventListener("open", () => {
      delay = STREAM_RECONNECT_BASE_MS;
      options.onOpen?.();
    });
    next.addEventListener("session.event", (message) => {
      if (!active) return;
      try {
        const event = JSON.parse(String(message.data || "")) as DurableSessionEvent;
        options.onEvent(event);
      } catch (error) {
        closeSource();
        options.onError?.(error);
        scheduleReconnect();
      }
    });
    next.addEventListener("stream.reset", (message) => {
      closeSource();
      cancelTimer();
      delay = STREAM_RECONNECT_BASE_MS;
      let reason = "cursor_expired";
      try {
        reason = (JSON.parse(String(message.data || "")) as { payload?: { reason?: string } })?.payload?.reason || reason;
      } catch {
        reason = "cursor_expired";
      }
      options.onReset(reason);
    });
    next.addEventListener("heartbeat", () => {
      delay = STREAM_RECONNECT_BASE_MS;
    });
    next.addEventListener("error", () => {
      closeSource();
      options.onError?.(new Error("session event stream disconnected"));
      scheduleReconnect();
    });
  };

  return {
    start() {
      active = true;
      delay = STREAM_RECONNECT_BASE_MS;
      closeSource();
      cancelTimer();
      open();
    },
    stop() {
      active = false;
      cancelTimer();
      closeSource();
    },
    reset() {
      delay = STREAM_RECONNECT_BASE_MS;
      if (!active) return;
      closeSource();
      cancelTimer();
      open();
    },
    nextDelay() {
      return delay;
    },
  };
}
