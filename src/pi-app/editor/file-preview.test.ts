import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import * as api from "../../lib/api";
import * as officePreview from "../../lib/office-preview";
import "../index";

vi.mock("../../lib/api", async () => {
  const actual = await vi.importActual<typeof import("../../lib/api")>("../../lib/api");
  return {
    ...actual,
    getWorkspaceFile: vi.fn(),
    getWorkspaceGitDiff: vi.fn(),
    saveWorkspaceFile: vi.fn(),
  };
});

vi.mock("../../lib/office-preview", async () => {
  const actual = await vi.importActual<typeof import("../../lib/office-preview")>("../../lib/office-preview");
  return { ...actual, mountOfficePreview: vi.fn() };
});

function mountPreview() {
  document.body.innerHTML = `
    <pi-app data-active-workspace-id="workspace-1">
      <div class="file-preview-panel" data-file-preview hidden>
        <div class="fp-dialog">
          <div class="fp-head">
            <div><strong class="fp-path">file</strong><small></small></div>
            <div class="fp-actions">
              <div class="fp-save-slot"><button type="button" data-action="save-file-preview" hidden>save</button></div>
              <div class="fp-mode-actions">
                <button type="button" data-action="download-file-preview">download</button>
                <button type="button" data-action="toggle-file-preview-mode" hidden>text</button>
                <button type="button" data-action="close-file-preview">×</button>
              </div>
            </div>
          </div>
          <div class="fp-body"></div>
        </div>
      </div>
    </pi-app>
  `;
  const app = document.querySelector("pi-app");
  app.apiConnected = true;
  app.loadWorkspaceMeta = vi.fn();
  return app;
}

function dispatchEditorChange(app, text: string) {
  const view = app.filePreview.editor.view;
  view.dispatch({ changes: { from: 0, to: view.state.doc.length, insert: text } });
}

describe("file preview CodeMirror editor", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.spyOn(window, "alert").mockImplementation(() => undefined);
    vi.spyOn(window, "confirm").mockReturnValue(true);
  });

  afterEach(() => {
    vi.restoreAllMocks();
    localStorage.clear();
    document.body.innerHTML = "";
  });

  it("opens Markdown files in rendered preview mode by default", () => {
    const app = mountPreview();
    app.renderFilePreview({
      path: "README.md",
      mime: "text/markdown",
      previewKind: "text",
      content: "# Hello\n\n**world**",
    });

    expect(app.filePreview.mode).toBe("markdown");
    expect(app.querySelector(".fp-body.markdown-body h1")?.textContent).toBe("Hello");
    expect(app.querySelector(".fp-body.markdown-body strong")?.textContent).toBe("world");
    expect(app.querySelector("[data-file-preview-editor]")).toBeFalsy();
    expect(app.querySelector("[data-action='toggle-file-preview-mode']").textContent).toBe("文本");
    expect(app.querySelector("[data-action='save-file-preview']").hidden).toBe(true);
  });

  it("routes Office files to the lazy viewer and destroys it on file switch", async () => {
    const app = mountPreview();
    const destroy = vi.fn();
    vi.mocked(officePreview.mountOfficePreview).mockResolvedValue({ ready: Promise.resolve(), destroy });

    app.renderFilePreview({ path: "docs/report.docx", mime: "application/vnd.openxmlformats-officedocument.wordprocessingml.document", previewKind: "unsupported" });
    await Promise.resolve();
    expect(app.querySelector("[data-file-preview-office]")).toBeTruthy();
    expect(officePreview.mountOfficePreview).toHaveBeenCalledWith(
      expect.any(HTMLElement),
      expect.objectContaining({ workspaceId: "workspace-1", path: "docs/report.docx" }),
      expect.any(Object),
    );

    app.renderFilePreview({ path: "README.md", mime: "text/markdown", previewKind: "text", content: "# readme" });
    expect(destroy).toHaveBeenCalledOnce();
  });

  it("keeps the API error view for an Office path when file loading fails", () => {
    const app = mountPreview();
    app.renderFilePreview({ path: "report.docx", previewKind: "error", content: "file missing" });

    expect(app.querySelector(".fp-body")?.textContent).toContain("file missing");
    expect(officePreview.mountOfficePreview).not.toHaveBeenCalled();
  });

  it("shows a download-friendly error when the Office viewer cannot load", async () => {
    const app = mountPreview();
    vi.mocked(officePreview.mountOfficePreview).mockRejectedValueOnce(new Error("worker missing"));

    app.renderFilePreview({ path: "report.xlsx", previewKind: "unsupported" });
    await Promise.resolve();
    await Promise.resolve();

    expect(app.querySelector("[data-file-preview-error]")?.textContent).toContain("worker missing");
    expect(app.querySelector("[data-action='download-file-preview']")).toBeTruthy();
  });

  it("destroys a stale Office handle when mounting finishes after a file switch", async () => {
    const app = mountPreview();
    let resolveMount!: (handle: { ready: Promise<void>; destroy: () => void }) => void;
    const destroy = vi.fn();
    vi.mocked(officePreview.mountOfficePreview).mockReturnValueOnce(new Promise((resolve) => { resolveMount = resolve; }));

    app.renderFilePreview({ path: "report.pptx", previewKind: "unsupported" });
    app.renderFilePreview({ path: "README.md", mime: "text/markdown", previewKind: "text", content: "# readme" });
    resolveMount({ ready: Promise.resolve(), destroy });
    await Promise.resolve();

    expect(destroy).toHaveBeenCalledOnce();
  });

  it("keeps the mode toggle beside the close button", () => {
    const app = mountPreview();
    app.renderFilePreview({
      path: "README.md",
      mime: "text/markdown",
      previewKind: "text",
      content: "# Hello",
    });

    const modeActions = app.querySelector(".fp-mode-actions");
    const toggle = modeActions?.querySelector("[data-action='toggle-file-preview-mode']");
    const close = modeActions?.querySelector("[data-action='close-file-preview']");
    expect(toggle).toBeTruthy();
    expect(close).toBeTruthy();
    expect(toggle!.compareDocumentPosition(close!) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    expect(modeActions?.previousElementSibling?.classList.contains("fp-save-slot")).toBe(true);
  });

  it("resolves relative Markdown images through the active workspace", () => {
    const app = mountPreview();
    app.renderFilePreview({
      path: "README.md",
      mime: "text/markdown",
      previewKind: "text",
      content: "![logo](docs/assets/logo.png)",
    });

    expect(app.querySelector(".fp-body.markdown-body img")?.getAttribute("src")).toBe(
      "/api/workspaces/workspace-1/files/raw?path=docs%2Fassets%2Flogo.png",
    );
  });

  it("preserves external Markdown image URLs", () => {
    const app = mountPreview();
    app.renderFilePreview({
      path: "README.md",
      mime: "text/markdown",
      previewKind: "text",
      content: "![logo](https://example.com/logo.png)",
    });

    expect(app.querySelector(".fp-body.markdown-body img")?.getAttribute("src")).toBe("https://example.com/logo.png");
  });

  it("loads a Git diff view for a previewed file", async () => {
    const app = mountPreview();
    vi.mocked(api.getWorkspaceGitDiff).mockResolvedValue({ diff: "-old\\n+new" });
    app.renderFilePreview({ path: "src/app.ts", mime: "text/plain", previewKind: "text", content: "new", originalContent: "old" });

    await app.toggleFilePreviewDiff();

    expect(api.getWorkspaceGitDiff).toHaveBeenCalledWith("workspace-1", "src/app.ts");
    expect(app.filePreview.mode).toBe("diff");
    expect(app.querySelector(".fp-body pre")?.textContent).toBe("-old\\n+new");
  });

  it("opens relative Markdown file links in the active workspace", async () => {
    const app = mountPreview();
    vi.mocked(api.getWorkspaceFile).mockResolvedValueOnce({
      path: "docs/guide.md",
      mime: "text/markdown",
      previewKind: "text",
      content: "# Guide",
    });
    app.renderFilePreview({
      path: "README.md",
      mime: "text/markdown",
      previewKind: "text",
      content: "[Guide](docs/guide.md)",
    });

    const link = app.querySelector<HTMLAnchorElement>(".fp-body.markdown-body a");
    expect(link).toBeTruthy();
    link!.dispatchEvent(new MouseEvent("click", { bubbles: true, cancelable: true }));
    await Promise.resolve();

    expect(api.getWorkspaceFile).toHaveBeenCalledWith("workspace-1", "docs/guide.md");
  });

  it("restores the selected preview mode from local storage", () => {
    const app = mountPreview();
    localStorage.setItem("pi-web:file-preview:workspace-1:README.md", JSON.stringify({ mode: "text" }));
    app.renderFilePreview({ path: "README.md", mime: "text/markdown", previewKind: "text", content: "# Hello" });

    expect(app.filePreview.mode).toBe("text");
    expect(app.querySelector("[data-file-preview-editor] .cm-editor")).toBeTruthy();
  });

  it("switches Markdown between rendered preview and text editing", () => {
    const app = mountPreview();
    app.renderFilePreview({ path: "README.md", mime: "text/markdown", previewKind: "text", content: "# Hello" });

    app.toggleFilePreviewMode();

    expect(app.filePreview.mode).toBe("text");
    expect(app.querySelector("[data-file-preview-editor] .cm-editor")).toBeTruthy();
    expect(app.querySelector("[data-action='toggle-file-preview-mode']").textContent).toBe("预览");
    expect(app.querySelector("[data-action='save-file-preview']").hidden).toBe(false);

    app.toggleFilePreviewMode();

    expect(app.filePreview.mode).toBe("markdown");
    expect(app.querySelector(".fp-body.markdown-body h1")?.textContent).toBe("Hello");
  });

  it("renders editable text files in CodeMirror and tracks dirty state", () => {
    const app = mountPreview();
    app.renderFilePreview({
      path: "src/demo.ts",
      mime: "text/typescript",
      previewKind: "text",
      content: "const answer = 1;",
    });

    const preview = app.querySelector("[data-file-preview]");
    const editor = app.querySelector("[data-file-preview-editor]");
    const save = app.querySelector("[data-action='save-file-preview']");

    expect(preview.hidden).toBe(false);
    expect(editor.querySelector(".cm-editor")).toBeTruthy();
    expect(editor.dataset.language).toBe("typescript");
    expect(app.filePreview.editor.getValue()).toBe("const answer = 1;");
    expect(save.hidden).toBe(false);
    expect(save.disabled).toBe(true);

    dispatchEditorChange(app, "const answer = 2;");

    expect(app.filePreview.dirty).toBe(true);
    expect(editor.querySelector(".cm-gitChangeGutter")).toBeTruthy();
    expect(editor.querySelector(".cm-git-modified")).toBeTruthy();
    expect(save.disabled).toBe(false);
    expect(save.textContent).toBe("save *");
    expect(app.querySelector(".fp-head small").textContent).toContain("modified");
  });

  it("searches file content with highlighted next and previous matches", () => {
    const app = mountPreview();
    app.renderFilePreview({ path: "demo.txt", mime: "text/plain", previewKind: "text", content: "alpha beta alpha" });

    const input = app.querySelector<HTMLInputElement>(".fp-editor-search input");
    expect(app.querySelector(".fp-editor-search span")?.textContent).toBe("搜索");
    expect(input?.placeholder).toBe("搜索文件内容");
    input!.value = "alpha";
    input!.dispatchEvent(new Event("input", { bubbles: true }));
    app.querySelector<HTMLButtonElement>("[aria-label='next search match']")!.click();

    expect(app.querySelector(".fp-editor-search")).toBeTruthy();
    expect(app.filePreview.editor.view.state.selection.main.from).toBe(0);

    app.querySelector<HTMLButtonElement>("[aria-label='next search match']")!.click();
    expect(app.filePreview.editor.view.state.selection.main.from).toBe(11);

    app.querySelector<HTMLButtonElement>("[aria-label='previous search match']")!.click();
    expect(app.filePreview.editor.view.state.selection.main.from).toBe(0);
  });

  it("shows added and deleted git gutter marker types", () => {
    const app = mountPreview();
    app.renderFilePreview({ path: "new.js", mime: "text/javascript", previewKind: "text", content: "let x = 1;", originalContent: "" });
    expect(app.querySelector(".cm-git-added")).toBeTruthy();

    app.renderFilePreview({ path: "short.js", mime: "text/javascript", previewKind: "text", content: "let x = 1;\n", originalContent: "let x = 1;\nlet y = 2;\n" });
    expect(app.querySelector(".cm-git-deleted")).toBeTruthy();
  });

  it("shows git-based gutter markers without treating opened git changes as unsaved", () => {
    const app = mountPreview();
    app.renderFilePreview({
      path: "demo.js",
      mime: "text/javascript",
      previewKind: "text",
      content: "let x = 2;",
      originalContent: "let x = 1;",
      gitStatus: "tracked",
    });

    expect(app.filePreview.dirty).toBe(false);
    expect(app.filePreview.cleanContent).toBe("let x = 2;");
    expect(app.filePreview.originalContent).toBe("let x = 1;");
    expect(app.querySelector(".cm-git-modified")).toBeTruthy();
    expect(app.querySelector("[data-action='save-file-preview']").disabled).toBe(true);
  });

  it("does not save a preview after the active workspace changes", async () => {
    const app = mountPreview();
    app.renderFilePreview({ path: "demo.js", mime: "text/javascript", previewKind: "text", content: "let x = 1;" });
    dispatchEditorChange(app, "let x = 2;");
    app.dataset.activeWorkspaceId = "workspace-2";

    await app.saveFilePreview();

    expect(api.saveWorkspaceFile).not.toHaveBeenCalled();
    expect(app.filePreview.dirty).toBe(true);
    expect(app.querySelector("[data-action='save-file-preview']").textContent).toBe("workspace changed");
  });

  it("ignores file previews that finish loading after a workspace switch", async () => {
    const app = mountPreview();
    vi.mocked(api.getWorkspaceFile).mockResolvedValueOnce({
      path: "demo.js",
      mime: "text/javascript",
      previewKind: "text",
      content: "let x = 1;",
    });

    const loading = app.openFilePath("demo.js");
    app.dataset.activeWorkspaceId = "workspace-2";
    await loading;

    expect(api.getWorkspaceFile).toHaveBeenCalledWith("workspace-1", "demo.js");
    expect(app.filePreview.file.previewKind).toBe("loading");
  });

  it("saves current CodeMirror document and keeps edits after save failure", async () => {
    const app = mountPreview();
    vi.mocked(api.saveWorkspaceFile).mockRejectedValueOnce(new Error("permission denied"));
    app.renderFilePreview({ path: "demo.js", mime: "text/javascript", previewKind: "text", content: "let x = 1;" });
    dispatchEditorChange(app, "let x = 2;");

    await app.saveFilePreview();

    expect(api.saveWorkspaceFile).toHaveBeenCalledWith("workspace-1", "demo.js", "let x = 2;");
    expect(app.filePreview.editor.getValue()).toBe("let x = 2;");
    expect(app.filePreview.dirty).toBe(true);
    expect(window.alert).toHaveBeenCalledWith("permission denied");
  });

  it("updates clean baseline after save success", async () => {
    const app = mountPreview();
    vi.mocked(api.saveWorkspaceFile).mockResolvedValueOnce({
      path: "demo.js",
      mime: "text/javascript",
      previewKind: "text",
      content: "let x = 2;",
    });
    app.renderFilePreview({ path: "demo.js", mime: "text/javascript", previewKind: "text", content: "let x = 1;" });
    dispatchEditorChange(app, "let x = 2;");

    await app.saveFilePreview();

    expect(app.filePreview.dirty).toBe(false);
    expect(app.filePreview.cleanContent).toBe("let x = 2;");
    expect(app.filePreview.editor.getValue()).toBe("let x = 2;");
    expect(app.querySelector("[data-action='save-file-preview']").textContent).toBe("saved");
  });

  it("keeps newer edits typed while save is in flight", async () => {
    const app = mountPreview();
    let resolveSave: (file: unknown) => void = () => undefined;
    vi.mocked(api.saveWorkspaceFile).mockReturnValueOnce(new Promise((resolve) => { resolveSave = resolve; }) as never);
    app.renderFilePreview({ path: "demo.js", mime: "text/javascript", previewKind: "text", content: "let x = 1;" });
    dispatchEditorChange(app, "let x = 2;");

    const saving = app.saveFilePreview();
    dispatchEditorChange(app, "let x = 3;");
    resolveSave({ path: "demo.js", mime: "text/javascript", previewKind: "text", content: "let x = 2;" });
    await saving;

    expect(app.filePreview.editor.getValue()).toBe("let x = 3;");
    expect(app.filePreview.cleanContent).toBe("let x = 2;");
    expect(app.filePreview.dirty).toBe(true);
  });

  it("asks before switching files with unsaved changes and guards reload", () => {
    const app = mountPreview();
    app.installFilePreviewUnloadGuard();
    app.renderFilePreview({ path: "a.txt", mime: "text/plain", previewKind: "text", content: "a" });
    dispatchEditorChange(app, "changed");
    vi.mocked(window.confirm).mockReturnValueOnce(false);

    const clean = app.confirmCleanFilePreview();
    const event = new Event("beforeunload", { cancelable: true });
    window.dispatchEvent(event);

    expect(clean).toBe(false);
    expect(window.confirm).toHaveBeenCalledWith("Discard unsaved file changes?");
    expect(event.defaultPrevented).toBe(true);
  });

  it("defaults plain dotfiles to text preview even with an unknown MIME", () => {
    const app = mountPreview();
    app.renderFilePreview({ path: ".gitignore", mime: "application/octet-stream", previewKind: "unsupported", content: "node_modules/" });

    expect(app.filePreview.mode).toBe("text");
    expect(app.querySelector("[data-file-preview-editor] .cm-editor")).toBeTruthy();
  });

  it("defaults images to raw preview when no inline data URL is present", () => {
    const app = mountPreview();
    app.renderFilePreview({ path: "large.png", mime: "image/png", previewKind: "image" });

    expect(app.filePreview.mode).toBe("image");
    expect(app.querySelector(".fp-body img")?.getAttribute("src")).toContain("/files/raw?");
  });

  it("ignores stale unsupported preview state for previewable files", () => {
    const app = mountPreview();
    localStorage.setItem("pi-web:file-preview:workspace-1:large.png", JSON.stringify({ mode: "unsupported" }));
    app.renderFilePreview({ path: "large.png", mime: "image/png", previewKind: "image" });

    expect(app.filePreview.mode).toBe("image");
    expect(app.querySelector(".fp-body img")).toBeTruthy();
  });

  it("renders audio, video, PDF, and sandboxed HTML previews", () => {
    const app = mountPreview();
    app.renderFilePreview({ path: "voice.mp3", mime: "audio/mpeg", previewKind: "audio" });
    expect(app.querySelector(".fp-body audio")).toBeTruthy();
    expect(app.querySelector(".fp-body audio")?.getAttribute("src")).toContain("/files/raw?");

    app.renderFilePreview({ path: "movie.mp4", mime: "video/mp4", previewKind: "video" });
    expect(app.querySelector(".fp-body video")).toBeTruthy();

    app.renderFilePreview({ path: "manual.pdf", mime: "application/pdf", previewKind: "pdf" });
    const pdf = app.querySelector(".fp-body iframe");
    expect(pdf).toBeTruthy();
    expect(pdf?.getAttribute("src")).toContain("/files/raw?");
    expect(pdf?.hasAttribute("sandbox")).toBe(false);

    app.renderFilePreview({ path: "index.html", mime: "text/html", previewKind: "html", content: "<h1>Hello</h1>" });
    const html = app.querySelector(".fp-body iframe");
    expect(html?.getAttribute("srcdoc")).toContain("<h1>Hello</h1>");
    expect(html?.getAttribute("sandbox")).toBe("");
  });

  it("loads the next text chunk without replacing the current file", async () => {
    const app = mountPreview();
    vi.mocked(api.getWorkspaceFile)
      .mockResolvedValueOnce({
        path: "large.txt",
        mime: "text/plain",
        previewKind: "text",
        content: "first",
        size: 10,
        offset: 0,
        nextOffset: 5,
        truncated: true,
      })
      .mockResolvedValueOnce({
        path: "large.txt",
        mime: "text/plain",
        previewKind: "text",
        content: "second",
        size: 10,
        offset: 5,
        truncated: false,
      });
    await app.openFilePath("large.txt");
    await app.loadMoreFilePreview();

    expect(api.getWorkspaceFile).toHaveBeenLastCalledWith("workspace-1", "large.txt", 5);
    expect(app.filePreview.file.content).toBe("firstsecond");
    expect(app.filePreview.file.truncated).toBe(false);
  });

  it("keeps image, large, and unsupported files on preview fallback paths", () => {
    const app = mountPreview();
    app.renderFilePreview({ path: "logo.png", mime: "image/png", previewKind: "image", dataUrl: "data:image/png;base64,aa" });
    expect(app.querySelector(".fp-body img")).toBeTruthy();
    expect(app.querySelector("[data-file-preview-editor]")).toBeFalsy();

    app.renderFilePreview({ path: "big.txt", mime: "text/plain", previewKind: "text", content: "abc", truncated: true });
    expect(app.querySelector("[data-file-preview-editor] .cm-editor")).toBeTruthy();
    expect(app.querySelector("[data-action='save-file-preview']").hidden).toBe(true);

    app.renderFilePreview({ path: "app.bin", mime: "application/octet-stream", previewKind: "unsupported" });
    expect(app.querySelector(".fp-body").textContent).toContain("不支持预览此文件");
  });

  it("clears discarded dirty state when closing preview", () => {
    const app = mountPreview();
    app.renderFilePreview({ path: "a.txt", mime: "text/plain", previewKind: "text", content: "a" });
    dispatchEditorChange(app, "changed");

    app.closeFilePreview();

    expect(app.filePreview).toBeUndefined();
    expect(app.hasDirtyFilePreview()).toBe(false);
  });
});
