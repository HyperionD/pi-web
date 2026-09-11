import { describe, expect, it, vi } from "vitest";
import {
  MAX_PENDING_ENTITY_RENDERS,
  MAX_RENDER_DELAY_MS,
  MAX_UPDATES_BEFORE_FLUSH,
  createRenderQueue,
  createTranscriptRenderScheduler,
  drainRenderQueue,
  enqueueEntityRender,
  shouldFlushRenderQueue,
} from "./transcript-render-scheduler";

describe("render queue", () => {
  it("collapses duplicate entity ids and tracks updates", () => {
    const queue = createRenderQueue();
    expect(enqueueEntityRender(queue, "a", 0)).toBe("queued");
    expect(enqueueEntityRender(queue, "a", 1)).toBe("queued");
    expect(queue.ids.size).toBe(1);
    expect(queue.updates).toBe(2);
    expect(queue.startedAt).toBe(0);
  });

  it("forces a flush on the update threshold and entity cap", () => {
    const updates = createRenderQueue();
    for (let index = 0; index < MAX_UPDATES_BEFORE_FLUSH - 1; index += 1) {
      enqueueEntityRender(updates, `e${index}`, 0);
    }
    expect(enqueueEntityRender(updates, "last", 0)).toBe("flush");
    const capacity = createRenderQueue();
    for (let index = 0; index < MAX_PENDING_ENTITY_RENDERS - 1; index += 1) {
      enqueueEntityRender(capacity, `e${index}`, 0);
    }
    expect(enqueueEntityRender(capacity, "last", 0)).toBe("flush");
  });

  it("detects time-based flushes and drains state", () => {
    const queue = createRenderQueue();
    expect(shouldFlushRenderQueue(queue, 100)).toBe(false);
    enqueueEntityRender(queue, "a", 10);
    expect(shouldFlushRenderQueue(queue, 10 + MAX_RENDER_DELAY_MS - 1)).toBe(false);
    expect(shouldFlushRenderQueue(queue, 10 + MAX_RENDER_DELAY_MS)).toBe(true);
    expect(drainRenderQueue(queue)).toEqual(["a"]);
    expect(queue.ids.size).toBe(0);
    expect(queue.updates).toBe(0);
    expect(queue.startedAt).toBeUndefined();
  });
});

describe("transcript render scheduler", () => {
  function fakeClockScheduler() {
    let now = 0;
    let frameCallback: ((time: number) => void) | undefined;
    let timerCallback: (() => void) | undefined;
    const flushed: string[][] = [];
    const scheduler = createTranscriptRenderScheduler({
      onFlush: (ids) => flushed.push(ids),
      requestFrame: (callback) => {
        frameCallback = callback;
        return 1;
      },
      cancelFrame: vi.fn(),
      setTimer: (callback) => {
        timerCallback = callback;
        return 2;
      },
      clearTimer: vi.fn(),
      now: () => now,
    });
    return {
      scheduler,
      flushed,
      tick(ms: number) {
        now += ms;
      },
      runFrame() {
        frameCallback?.(now);
      },
      runTimer() {
        timerCallback?.();
      },
    };
  }

  it("flushes on the animation frame", () => {
    const clock = fakeClockScheduler();
    clock.scheduler.enqueue("a");
    clock.scheduler.enqueue("b");
    expect(clock.scheduler.pendingCount()).toBe(2);
    clock.runFrame();
    expect(clock.flushed).toEqual([["a", "b"]]);
    expect(clock.scheduler.pendingCount()).toBe(0);
  });

  it("flushes on the fallback timer", () => {
    const clock = fakeClockScheduler();
    clock.scheduler.enqueue("a");
    clock.tick(MAX_RENDER_DELAY_MS);
    clock.runTimer();
    expect(clock.flushed).toEqual([["a"]]);
  });

  it("flushes immediately when the update budget is exceeded", () => {
    const clock = fakeClockScheduler();
    for (let index = 0; index < MAX_UPDATES_BEFORE_FLUSH - 1; index += 1) {
      clock.scheduler.enqueue(`e${index}`);
    }
    expect(clock.flushed).toHaveLength(0);
    clock.scheduler.enqueue("last");
    expect(clock.flushed).toHaveLength(1);
    expect(clock.scheduler.pendingCount()).toBe(0);
  });

  it("resets pending work without flushing", () => {
    const clock = fakeClockScheduler();
    clock.scheduler.enqueue("a");
    clock.scheduler.reset();
    expect(clock.scheduler.pendingCount()).toBe(0);
    clock.runFrame();
    clock.runTimer();
    expect(clock.flushed).toHaveLength(0);
  });

  it("does not flush when nothing is pending", () => {
    const clock = fakeClockScheduler();
    clock.scheduler.flush();
    expect(clock.flushed).toHaveLength(0);
  });

  it("uses browser frame and timer defaults when none are injected", () => {
    const flushed: string[][] = [];
    const scheduler = createTranscriptRenderScheduler({ onFlush: (ids) => flushed.push(ids) });
    expect(typeof globalThis.requestAnimationFrame).toBe("function");
    scheduler.enqueue("default-a");
    scheduler.reset();
    scheduler.enqueue("default-b");
    scheduler.flush();
    expect(flushed).toEqual([["default-b"]]);
  });
});
