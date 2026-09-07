// @ts-nocheck
import { beforeEach, describe, expect, it, vi } from "vitest";
import "../index";

function renderApp() {
  document.body.innerHTML = `
    <pi-app data-active-workspace-id="w1">
      <div class="attach-chips" hidden></div>
      <div data-attachment-guidance></div>
      <div data-attachment-warning aria-live="polite" hidden></div>
      <textarea class="prompt-textarea"></textarea>
      <button class="send-btn" aria-disabled="true">send</button>
      <button class="attach-btn">attach</button>
      <input data-file-input type="file" />
    </pi-app>
  `;
}

describe("pi-app prompt input", () => {
  beforeEach(() => {
    renderApp();
  });

  it("renders image attachments in user messages", async () => {
    const app = document.querySelector("pi-app");
    await customElements.whenDefined("pi-app");
    app.connectedCallback();
    app.termInner = document.createElement("div");
    app.append(app.termInner);

    app.appendMessage({
      kind: "user",
      text: "잘 보이나? 이미지?",
      attachments: [{ type: "image", name: "clip.png", dataUrl: "data:image/png;base64,ZmFrZQ==" }],
    });

    expect(app.querySelector(".msg[data-kind='user'] .body").textContent).toContain("잘 보이나? 이미지?");
    expect(app.querySelector(".msg-image").getAttribute("src")).toBe("data:image/png;base64,ZmFrZQ==");
  });

  it("accepts an ordinary file at 256 KiB but rejects the next byte", async () => {
    const app = document.querySelector("pi-app");
    await customElements.whenDefined("pi-app");
    app.connectedCallback();

    await app.addFiles([new File(["a".repeat(256 * 1024)], "boundary.txt", { type: "text/plain" })]);
    expect(app.attachmentContents.filter(Boolean)).toHaveLength(1);
    expect(app.querySelector(".attach-chip").dataset.sendable).toBe("true");

    await app.addFiles([new File(["a".repeat(256 * 1024 + 1)], "over.txt", { type: "text/plain" })]);
    expect(app.attachmentContents.filter(Boolean)).toHaveLength(1);
    expect(app.querySelectorAll(".attach-chip")[1].dataset.sendable).toBe("false");
  });

  it("rejects ordinary attachments when their aggregate prompt JSON exceeds the budget", async () => {
    const app = document.querySelector("pi-app");
    await customElements.whenDefined("pi-app");
    app.connectedCallback();

    const files = [1, 2, 3].map((index) => new File(
      ["a".repeat(256 * 1024)],
      `part-${index}.txt`,
      { type: "text/plain" },
    ));
    await app.addFiles(files);

    expect(app.attachmentContents.filter(Boolean)).toHaveLength(2);
    expect(app.querySelectorAll(".attach-chip")[2].dataset.sendable).toBe("false");
    expect(app.querySelector("[data-attachment-warning]").textContent).toContain("上传到工作区");
  });

  it("applies the aggregate budget to mixed attachments in either order", async () => {
    const image = new File([new Uint8Array(400 * 1024)], "large.png", { type: "image/png" });
    const file = new File(["a".repeat(256 * 1024)], "part.txt", { type: "text/plain" });
    const orders = [[image, file], [file, image]];

    for (const files of orders) {
      renderApp();
      const app = document.querySelector("pi-app");
      await customElements.whenDefined("pi-app");
      app.connectedCallback();
      await app.addFiles(files);

      expect(app.attachmentContents.filter(Boolean)).toHaveLength(1);
      expect(app.querySelectorAll(".attach-chip")[1].dataset.sendable).toBe("false");
    }
  });

  it("rejects ordinary files over 256 KiB without making them sendable", async () => {
    const app = document.querySelector("pi-app");
    await customElements.whenDefined("pi-app");
    app.connectedCallback();

    const file = new File([new Uint8Array(256 * 1024 + 1)], "large.txt", { type: "text/plain" });
    await app.addFiles([file]);

    expect(app.attachmentContents.filter(Boolean)).toHaveLength(0);
    expect(app.querySelector(".attach-chip").dataset.sendable).toBe("false");
    expect(app.querySelector("[data-attachment-warning]").textContent).toContain("上传到工作区");
    expect(app.querySelector(".send-btn").getAttribute("aria-disabled")).toBe("true");
    expect(app.querySelector(".send-btn").disabled).toBe(true);
  });

  it("renders the current workspace upload limit with a 100 MiB default", async () => {
    const app = document.querySelector("pi-app");
    await customElements.whenDefined("pi-app");
    app.connectedCallback();

    expect(app.querySelector("[data-attachment-guidance]").textContent).toContain("100 MiB");
    app.appConfigState = { config: { workspace: { maxUploadBytes: 256 * 1024 * 1024 } } };
    app.updatePrompt();
    expect(app.querySelector("[data-attachment-guidance]").textContent).toContain("256 MiB");
  });

  it("rejects an image when the conservative prompt request budget would overflow", async () => {
    const app = document.querySelector("pi-app");
    await customElements.whenDefined("pi-app");
    app.connectedCallback();
    const fileDataUrl = vi.spyOn(app, "fileDataUrl");

    const image = new File([new Uint8Array(800 * 1024)], "huge.png", { type: "image/png" });
    await app.addFiles([image]);

    expect(app.attachmentContents.filter(Boolean)).toHaveLength(0);
    expect(fileDataUrl).not.toHaveBeenCalled();
    expect(app.querySelector("[data-attachment-warning]").textContent).toContain("上传到工作区");
    expect(app.querySelector(".send-btn").getAttribute("aria-disabled")).toBe("true");
  });

  it("clears attachment warnings after removing an invalid chip or adding a valid file", async () => {
    const app = document.querySelector("pi-app");
    await customElements.whenDefined("pi-app");
    app.connectedCallback();

    await app.addFiles([new File([new Uint8Array(256 * 1024 + 1)], "large.txt", { type: "text/plain" })]);
    const remove = app.querySelector("[data-remove-attachment]");
    remove.click();
    expect(app.querySelector("[data-attachment-warning]").hidden).toBe(true);

    await app.addFiles([new File([new Uint8Array(800 * 1024)], "huge.png", { type: "image/png" })]);
    await app.addFiles([new File(["small image"], "small.png", { type: "image/png" })]);
    expect(app.querySelector("[data-attachment-warning]").hidden).toBe(true);
  });

  it("attaches pasted clipboard images instead of inserting them into the prompt", async () => {
    const app = document.querySelector("pi-app");
    await customElements.whenDefined("pi-app");
    app.connectedCallback();

    const image = new File(["fake image"], "clip.png", { type: "image/png" });
    const paste = new Event("paste", { cancelable: true });
    Object.defineProperty(paste, "clipboardData", {
      value: {
        items: [{ kind: "file", type: "image/png", getAsFile: () => image }],
      },
    });

    app.prompt.dispatchEvent(paste);
    await new Promise((resolve) => setTimeout(resolve, 0));

    expect(paste.defaultPrevented).toBe(true);
    expect(app.prompt.value).toBe("");
    expect(app.querySelector(".attach-chips").hidden).toBe(false);
    expect(app.querySelector(".attach-chip .ac-preview").getAttribute("src")).toContain("data:image/png;base64,");
    expect(app.querySelector(".attach-chip .ac-name").textContent).toBe("clip.png");
    expect(app.attachmentContents[0]).toMatchObject({
      type: "image",
      name: "clip.png",
      mimeType: "image/png",
    });
    expect(app.attachmentContents[0].dataUrl).toContain("data:image/png;base64,");
    expect(app.querySelector(".send-btn").getAttribute("aria-disabled")).toBe("false");
  });
});
