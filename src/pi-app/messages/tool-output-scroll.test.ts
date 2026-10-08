// @ts-nocheck
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanupPiAppFixture, connectPiApp, installPiAppFixture } from "../test-helper";

function installOutputGeometry(body, contentHeight = 400) {
  const geometry = { contentHeight, scrollTop: 0 };
  const visible = () => body.isConnected && body.closest("details").open;
  Object.defineProperties(body, {
    clientHeight: { configurable: true, get: () => visible() ? 100 : 0 },
    scrollHeight: { configurable: true, get: () => visible() ? geometry.contentHeight : 0 },
    scrollTop: {
      configurable: true,
      get: () => visible() ? geometry.scrollTop : 0,
      set: (value) => {
        geometry.scrollTop = Math.max(0, Math.min(value, body.scrollHeight - body.clientHeight));
      },
    },
  });
  return geometry;
}

function flushFrames(frames) {
  for (let index = 0; index < 5 && frames.length; index += 1) {
    frames.splice(0).forEach((callback) => callback(0));
  }
}

function toggleCard(card, open) {
  card.open = open;
  card.dispatchEvent(new Event("toggle"));
}

describe("tool output scroll position", () => {
  let frames;
  beforeEach(() => {
    installPiAppFixture();
    frames = [];
    vi.spyOn(window, "requestAnimationFrame").mockImplementation((callback) => {
      frames.push(callback);
      return frames.length;
    });
  });
  afterEach(cleanupPiAppFixture);

  for (const status of ["running", "ok", "err"]) {
    it(`defaults ${status} output to its latest position after insertion`, async () => {
      const app = await connectPiApp();
      app.renderMessages([{ kind: "tool", id: "call-1", tool: "bash", status, body: "old\nlatest" }]);
      const body = app.querySelector(".tc-body");
      installOutputGeometry(body);
      flushFrames(frames);

      expect(body.scrollTop).toBe(300);
    });
  }

  it("restores the latest output after a collapsed card receives more content", async () => {
    const app = await connectPiApp();
    app.renderMessages([{ kind: "tool", id: "call-1", tool: "bash", status: "running", body: "old" }]);
    const card = app.querySelector(".tool-card");
    const body = card.querySelector(".tc-body");
    const geometry = installOutputGeometry(body);
    flushFrames(frames);
    toggleCard(card, false);
    app.appendToolOutput({ id: "call-1", tool: "bash", chunk: "\nlatest" });
    geometry.contentHeight = 600;
    toggleCard(card, true);
    flushFrames(frames);

    expect(body.textContent).toContain("latest");
    expect(body.scrollTop).toBe(500);
  });

  it("keeps a reader's earlier position through hidden updates and virtualized remounts", async () => {
    const app = await connectPiApp();
    app.renderMessages([{ kind: "tool", id: "call-1", tool: "bash", status: "running", body: "old" }]);
    const card = app.querySelector(".tool-card");
    const body = card.querySelector(".tc-body");
    installOutputGeometry(body);
    flushFrames(frames);
    body.scrollTop = 30;
    body.dispatchEvent(new Event("scroll"));
    toggleCard(card, false);
    app.appendToolOutput({ id: "call-1", tool: "bash", chunk: "\nlatest" });
    toggleCard(card, true);
    flushFrames(frames);
    expect(body.scrollTop).toBe(30);

    const wrapper = app.renderVirtualTranscriptItem(app.transcriptItems[0]);
    const remountedBody = wrapper.querySelector(".tc-body");
    installOutputGeometry(remountedBody, 600);
    card.closest(".transcript-item").replaceWith(wrapper);
    flushFrames(frames);
    expect(remountedBody.scrollTop).toBe(30);
  });

  it("keeps the output position when a legacy transcript is rebuilt for a new reply", async () => {
    const app = await connectPiApp();
    app.renderMessages([{ kind: "tool", id: "call-1", tool: "bash", status: "ok", body: "old\nlatest" }]);
    const body = app.querySelector(".tc-body");
    installOutputGeometry(body);
    flushFrames(frames);
    body.scrollTop = 30;
    body.dispatchEvent(new Event("scroll"));
    app.appendMessage({ kind: "pi", text: "reply" });
    const remountedBody = app.querySelector(".tc-body");
    installOutputGeometry(remountedBody);
    flushFrames(frames);

    expect(remountedBody.scrollTop).toBe(30);
  });
});
