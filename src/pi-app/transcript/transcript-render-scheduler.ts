export const MAX_PENDING_ENTITY_RENDERS = 128;
export const MAX_UPDATES_BEFORE_FLUSH = 32;
export const MAX_RENDER_DELAY_MS = 16;

export type RenderQueue = {
  ids: Set<string>;
  updates: number;
  startedAt?: number;
};

export function createRenderQueue(): RenderQueue {
  return { ids: new Set(), updates: 0 };
}

export function enqueueEntityRender(queue: RenderQueue, itemId: string, now: number): "queued" | "flush" {
  queue.ids.add(itemId);
  queue.updates += 1;
  if (queue.startedAt === undefined) queue.startedAt = now;
  if (queue.ids.size >= MAX_PENDING_ENTITY_RENDERS || queue.updates >= MAX_UPDATES_BEFORE_FLUSH) {
    return "flush";
  }
  return "queued";
}

export function shouldFlushRenderQueue(queue: RenderQueue, now: number): boolean {
  return queue.startedAt !== undefined && now - queue.startedAt >= MAX_RENDER_DELAY_MS;
}

export function drainRenderQueue(queue: RenderQueue): string[] {
  const ids = [...queue.ids];
  queue.ids = new Set();
  queue.updates = 0;
  queue.startedAt = undefined;
  return ids;
}

export type RenderSchedulerOptions = {
  onFlush: (ids: string[]) => void;
  requestFrame?: (callback: (time: number) => void) => number;
  cancelFrame?: (handle: number) => void;
  setTimer?: (callback: () => void, ms: number) => number;
  clearTimer?: (handle: number) => void;
  now?: () => number;
};

export type TranscriptRenderScheduler = {
  enqueue: (itemId: string) => void;
  flush: () => void;
  reset: () => void;
  pendingCount: () => number;
};

export function createTranscriptRenderScheduler(options: RenderSchedulerOptions): TranscriptRenderScheduler {
  const requestFrame = options.requestFrame || ((callback) => window.requestAnimationFrame(callback));
  const cancelFrame = options.cancelFrame || ((handle) => window.cancelAnimationFrame(handle));
  const setTimer = options.setTimer || ((callback, ms) => window.setTimeout(callback, ms));
  const clearTimer = options.clearTimer || ((handle) => window.clearTimeout(handle));
  const now = options.now || (() => Date.now());
  const queue = createRenderQueue();
  let frame: number | undefined;
  let timer: number | undefined;

  const cancelPending = () => {
    if (frame !== undefined) {
      cancelFrame(frame);
      frame = undefined;
    }
    if (timer !== undefined) {
      clearTimer(timer);
      timer = undefined;
    }
  };

  const flush = () => {
    cancelPending();
    if (queue.ids.size === 0) return;
    options.onFlush(drainRenderQueue(queue));
  };

  const schedule = () => {
    if (frame === undefined && queue.ids.size > 0) {
      frame = requestFrame(() => {
        frame = undefined;
        flush();
      });
    }
    if (timer === undefined && queue.ids.size > 0) {
      timer = setTimer(() => {
        timer = undefined;
        flush();
      }, MAX_RENDER_DELAY_MS);
    }
  };

  return {
    enqueue(itemId: string) {
      const decision = enqueueEntityRender(queue, itemId, now());
      if (decision === "flush") {
        flush();
        return;
      }
      schedule();
    },
    flush,
    reset() {
      cancelPending();
      drainRenderQueue(queue);
    },
    pendingCount() {
      return queue.ids.size;
    },
  };
}
