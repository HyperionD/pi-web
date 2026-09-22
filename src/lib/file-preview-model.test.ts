import { describe, expect, it } from "vitest";
import {
  fileDisplayKind,
  isHTMLPath,
  isMarkdownPath,
  isMediaPath,
  previewStateKey,
} from "./file-preview-model";

describe("file preview model", () => {
  it("classifies previewable file kinds without treating Office files as previewable", () => {
    expect(fileDisplayKind({ path: "logo.png", mime: "image/png", previewKind: "image" })).toBe("image");
    expect(fileDisplayKind({ path: "voice.mp3", mime: "audio/mpeg", previewKind: "audio" })).toBe("audio");
    expect(fileDisplayKind({ path: "movie.mp4", mime: "video/mp4", previewKind: "video" })).toBe("video");
    expect(fileDisplayKind({ path: "manual.pdf", mime: "application/pdf", previewKind: "pdf" })).toBe("pdf");
    expect(fileDisplayKind({ path: "report.docx", mime: "application/vnd.openxmlformats-officedocument.wordprocessingml.document" })).toBe("unsupported");
    expect(fileDisplayKind({ path: ".gitignore", mime: "application/octet-stream", previewKind: "unsupported" })).toBe("text");
  });

  it("detects Markdown, HTML, and media paths case-insensitively", () => {
    expect(isMarkdownPath("docs/README.MD")).toBe(true);
    expect(isHTMLPath("docs/index.HTML")).toBe(true);
    expect(isMediaPath("clips/demo.WEBM")).toBe(true);
    expect(isMediaPath("docs/report.pdf")).toBe(false);
  });

  it("creates a stable encoded storage key per workspace and path", () => {
    expect(previewStateKey("workspace/1", "docs/read me.md")).toBe(
      "pi-web:file-preview:workspace%2F1:docs%2Fread%20me.md",
    );
  });
});
