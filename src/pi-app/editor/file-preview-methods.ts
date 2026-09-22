import { getWorkspaceFile, getWorkspaceFileAssetURL, saveWorkspaceFile } from "../../lib/api";
import { fileDisplayKind, isHTMLPath, isMarkdownPath } from "../../lib/file-preview-model";
import { renderMarkdownBody } from "../../lib/renderers";
import { CodeMirrorFileEditor, codeMirrorLanguageName, editableFileState, isTextFile } from "./file-editor";

export const filePreviewMethods = {
  async openFile(button) {
    return this.openFilePath?.(button?.dataset.filePath, button);
  },

  async openFilePath(path, button = undefined) {
    const workspaceId = this.dataset.activeWorkspaceId;
    if (!path || !workspaceId || !this.apiConnected || !this.confirmCleanFilePreview?.()) return;
    const inspectorRequestId = this.beginInspectorRequest?.();
    try {
      this.querySelectorAll(".tree-node.selected").forEach((node) => node.classList.remove("selected"));
      button?.classList.add("selected");
      window.dispatchEvent(new CustomEvent("pi-workspace-tree:update", {
        detail: { files: this.workspaceFiles || [], statusMap: this.workspaceFileStatuses || {}, selectedPath: path },
      }));
      this.renderFilePreview({ path, previewKind: "loading" });
      const file = await getWorkspaceFile(workspaceId, path);
      if (!this.inspectorRequestIsCurrent?.(inspectorRequestId, workspaceId)) return;
      this.renderFilePreview(file);
    } catch (error) {
      if (!this.inspectorRequestIsCurrent?.(inspectorRequestId, workspaceId)) return;
      this.renderFilePreview({ path, previewKind: "error", content: errorMessage(error) });
      this.setConnection("err");
    }
  },

  renderFilePreview(file) {
    const preview = this.querySelector("[data-file-preview]");
    const body = preview?.querySelector(".fp-body");
    if (!preview || !body) return;
    this.closeGitDetail?.(false);
    this.destroyFilePreviewEditor?.();
    preview.hidden = false;
    this.toggleFilePreviewPanel?.(true);
    this.filePreview = {
      workspaceId: this.dataset.activeWorkspaceId,
      file,
      mode: defaultPreviewMode(file),
      cleanContent: file.content || "",
      originalContent: gitOriginalContent(file),
      dirty: false,
      editor: undefined,
      saveStatus: "",
    };
    setPreviewHeader(preview, this.filePreview);
    this.renderFilePreviewBody();
  },

  renderFilePreviewBody() {
    const preview = this.querySelector("[data-file-preview]");
    const body = preview?.querySelector(".fp-body");
    const state = this.filePreview;
    if (!preview || !body || !state) return;
    const { file, mode } = state;
    this.destroyFilePreviewEditor?.();
    body.replaceChildren();
    body.classList.remove("markdown-body");
    updatePreviewActions(preview, state);
    if (file.previewKind === "loading") {
      body.textContent = "loading preview…";
      return;
    }
    const kind = fileDisplayKind(file);
    if (mode === "markdown" && isMarkdownFile(file)) {
      body.classList.add("markdown-body");
      body.innerHTML = renderMarkdownBody(`${file.content || ""}${file.truncated ? "\n\n[truncated]" : ""}`);
      resolveMarkdownImages(body, file, state.workspaceId);
      return;
    }
    if (kind === "html" && mode === "preview") {
      body.append(htmlPreviewNode(file));
      return;
    }
    if (kind === "image" && mode === "image" && file.dataUrl) {
      body.append(imagePreviewNode(file));
      return;
    }
    if (kind === "audio") {
      body.append(mediaPreviewNode(file, "audio", state.workspaceId));
      return;
    }
    if (kind === "video") {
      body.append(mediaPreviewNode(file, "video", state.workspaceId));
      return;
    }
    if (kind === "pdf") {
      body.append(pdfPreviewNode(file, state.workspaceId));
      return;
    }
    if (mode === "text" && isTextFile(file)) {
      const node = textPreviewNode(this, state);
      body.append(node);
      return;
    }
    body.textContent = fallbackMessage(file);
  },

  async loadMoreFilePreview() {
    const state = this.filePreview;
    const workspaceId = this.dataset.activeWorkspaceId;
    const file = state?.file;
    const offset = Number(file?.nextOffset || 0);
    if (!state || !workspaceId || state.workspaceId !== workspaceId || !file?.path || !file.truncated || !offset) return;
    const next = await getWorkspaceFile(workspaceId, file.path, offset);
    if (this.filePreview !== state || this.dataset.activeWorkspaceId !== workspaceId || state.file.path !== next.path) return;
    state.file = {
      ...state.file,
      ...next,
      content: `${state.file.content || ""}${next.content || ""}`,
      originalContent: state.file.originalContent || next.originalContent,
    };
    state.cleanContent = state.file.content || "";
    state.dirty = false;
    this.renderFilePreviewBody();
  },

  downloadFilePreview() {
    const workspaceId = this.dataset.activeWorkspaceId;
    const file = this.filePreview?.file;
    if (!workspaceId || !file?.path) return;
    const anchor = document.createElement("a");
    anchor.href = getWorkspaceFileAssetURL(workspaceId, file.path, { download: true });
    anchor.download = file.path.split("/").pop() || "download";
    anchor.hidden = true;
    document.body.append(anchor);
    try {
      anchor.click();
    } finally {
      anchor.remove();
    }
  },

  closeFilePreview(skipConfirmation = false) {
    if (!skipConfirmation && !this.confirmCleanFilePreview?.()) return;
    this.beginInspectorRequest?.();
    this.destroyFilePreviewEditor?.();
    this.filePreview = undefined;
    this.toggleFilePreviewPanel?.(false);
    this.querySelector("[data-file-preview]")?.setAttribute("hidden", "");
  },

  toggleFilePreviewMode() {
    if (!this.filePreview?.file || !this.confirmCleanFilePreview?.()) return;
    this.filePreview.mode = isMarkdownFile(this.filePreview.file)
      ? this.filePreview.mode === "markdown" ? "text" : "markdown"
      : isHTMLFile(this.filePreview.file)
        ? this.filePreview.mode === "preview" ? "text" : "preview"
        : this.filePreview.mode === "image" ? "text" : "image";
    this.filePreview.cleanContent = this.filePreview.file.content || "";
    this.filePreview.originalContent = gitOriginalContent(this.filePreview.file);
    this.filePreview.dirty = false;
    this.filePreview.saveStatus = "";
    this.renderFilePreviewBody();
  },

  async saveFilePreview() {
    const workspaceId = this.dataset.activeWorkspaceId;
    const state = this.filePreview;
    const file = state?.file;
    if (!workspaceId || !file?.path || !state?.editor || !this.apiConnected) return;
    if (state.workspaceId && state.workspaceId !== workspaceId) {
      state.saveStatus = "workspace changed";
      updatePreviewActions(this.querySelector("[data-file-preview]"), state);
      return;
    }
    const save = this.querySelector("[data-action='save-file-preview']");
    if (save) save.disabled = true;
    try {
      const content = state.editor.getValue();
      const next = await saveWorkspaceFile(workspaceId, file.path, content);
      const currentContent = state.editor?.getValue?.() || "";
      if (currentContent !== content) {
        state.file = next;
        state.cleanContent = next.content || content;
        state.originalContent = gitOriginalContent(next);
        state.dirty = currentContent !== state.cleanContent;
        state.saveStatus = "saved";
        updatePreviewActions(this.querySelector("[data-file-preview]"), state);
        await this.loadWorkspaceMeta(workspaceId);
        return;
      }
      this.destroyFilePreviewEditor?.();
      this.filePreview = {
        workspaceId,
        file: next,
        mode: defaultPreviewMode(next),
        cleanContent: next.content || content,
        originalContent: gitOriginalContent(next),
        dirty: false,
        editor: undefined,
        saveStatus: "saved",
      };
      this.renderFilePreviewBody();
      await this.loadWorkspaceMeta(workspaceId);
    } catch (error) {
      state.saveStatus = errorMessage(error);
      updatePreviewActions(this.querySelector("[data-file-preview]"), state);
      alert(errorMessage(error));
      this.setConnection("err");
    } finally {
      if (save) save.disabled = !this.filePreview?.dirty;
    }
  },

  destroyFilePreviewEditor() {
    this.filePreview?.editor?.destroy?.();
    if (this.filePreview) this.filePreview.editor = undefined;
  },

  hasDirtyFilePreview() {
    return !!this.filePreview?.dirty;
  },

  confirmCleanFilePreview() {
    if (!this.hasDirtyFilePreview?.()) return true;
    return window.confirm("Discard unsaved file changes?");
  },

  installFilePreviewUnloadGuard() {
    if (this.uninstallFilePreviewUnloadGuard) return;
    const guard = (event) => {
      if (!this.hasDirtyFilePreview?.()) return;
      event.preventDefault();
      event.returnValue = "";
    };
    window.addEventListener("beforeunload", guard);
    this.uninstallFilePreviewUnloadGuard = () => window.removeEventListener("beforeunload", guard);
  },
};

function setPreviewHeader(preview, state) {
  const file = state.file || state;
  const meta = [file.mime, file.truncated ? "truncated" : "", state.dirty ? "modified" : state.saveStatus]
    .filter(Boolean)
    .join(" · ");
  preview.querySelector(".fp-path").textContent = file.path || "file";
  preview.querySelector("small").textContent = meta;
}

function updatePreviewActions(preview, state) {
  if (!preview || !state?.file) return;
  const file = state.file;
  const mode = state.mode;
  const eligibility = editableFileState(file);
  const canEdit = eligibility.editable;
  const canToggle = isMarkdownFile(file) || isHTMLFile(file) || file.mime === "image/svg+xml" && file.dataUrl && file.content;
  const save = preview.querySelector("[data-action='save-file-preview']");
  const toggle = preview.querySelector("[data-action='toggle-file-preview-mode']");
  const workspaceChanged = state.saveStatus === "workspace changed";
  save.hidden = !canEdit || mode !== "text";
  save.disabled = !state.dirty || workspaceChanged;
  save.textContent = workspaceChanged
    ? "workspace changed"
    : state.dirty ? "save *" : state.saveStatus === "saved" ? "saved" : "save";
  save.title = state.saveStatus && state.saveStatus !== "saved" ? state.saveStatus : "";
  toggle.hidden = !canToggle;
  const switchToText = mode === "markdown" || mode === "image" || mode === "preview";
  toggle.textContent = switchToText ? "文本" : "预览";
  toggle.setAttribute("aria-label", switchToText ? "切换到文本模式" : "切换到预览模式");
  toggle.title = switchToText ? "切换到文本模式" : "切换到预览模式";
  setPreviewHeader(preview, state);
}

function gitOriginalContent(file) {
  return typeof file?.originalContent === "string" ? file.originalContent : file?.content || "";
}

function defaultPreviewMode(file) {
  const kind = fileDisplayKind(file);
  if (kind === "image" && file.dataUrl) return "image";
  if (kind === "markdown") return "markdown";
  if (kind === "html") return "preview";
  if (["audio", "video", "pdf"].includes(kind)) return kind;
  if (isTextFile(file)) return "text";
  return "unsupported";
}

function isMarkdownFile(file) {
  return isTextFile(file) && (file?.mime === "text/markdown" || isMarkdownPath(file?.path || ""));
}

function isHTMLFile(file) {
  return fileDisplayKind(file) === "html" || file?.mime === "text/html" || isHTMLPath(file?.path || "");
}

function textPreviewNode(app, state) {
  const file = state.file;
  const container = document.createElement("div");
  container.dataset.filePreviewEditor = "";
  container.dataset.language = codeMirrorLanguageName(file);

  const readOnly = editableFileState(file).readOnly;
  const content = `${file.content || ""}${file.truncated ? "\n\n[truncated]" : ""}`;
  state.editor = new CodeMirrorFileEditor(container, {
    file,
    content,
    originalContent: state.originalContent,
    readOnly,
    onChange: (nextContent) => {
      state.dirty = nextContent !== state.cleanContent;
      state.saveStatus = "";
      updatePreviewActions(app.querySelector("[data-file-preview]"), state);
    },
    onSave: () => void app.saveFilePreview?.(),
  });
  container.setAttribute("aria-label", `${readOnly ? "view" : "edit"} ${file.path || "file"}`);
  return container;
}

function resolveMarkdownImages(body, file, workspaceId) {
  if (!workspaceId || !file?.path) return;
  const baseURL = new URL(workspaceFileURLPath(file.path), "https://pi-web-workspace.local/");
  body.querySelectorAll("img[src]").forEach((image) => {
    const source = image.getAttribute("src");
    if (!source || !isRelativeWorkspaceImageSource(source)) return;
    let resolved;
    try {
      resolved = new URL(source, baseURL);
    } catch {
      return;
    }
    if (resolved.origin !== baseURL.origin) return;
    let path;
    try {
      path = decodeURIComponent(resolved.pathname).replace(/^\/+/, "");
    } catch {
      return;
    }
    if (!path) return;
    image.setAttribute("src", getWorkspaceFileAssetURL(workspaceId, path));
  });
}

function isRelativeWorkspaceImageSource(source) {
  return !/^[a-z][a-z\d+.-]*:/i.test(source)
    && !source.startsWith("//") && !source.startsWith("#") && !source.startsWith("?");
}

function workspaceFileURLPath(path) {
  return String(path).split("/").map(encodeURIComponent).join("/");
}

function imagePreviewNode(file) {
  const image = document.createElement("img");
  image.src = file.dataUrl;
  image.alt = file.path || "file preview";
  return image;
}

function mediaPreviewNode(file, kind, workspaceId) {
  const media = document.createElement(kind);
  media.controls = true;
  media.preload = "metadata";
  media.src = getWorkspaceFileAssetURL(workspaceId, file.path);
  media.setAttribute("aria-label", file.path || `${kind} preview`);
  return media;
}

function pdfPreviewNode(file, workspaceId) {
  const frame = document.createElement("iframe");
  frame.src = getWorkspaceFileAssetURL(workspaceId, file.path);
  frame.title = file.path || "PDF preview";
  return frame;
}

function htmlPreviewNode(file) {
  const frame = document.createElement("iframe");
  frame.setAttribute("sandbox", "");
  frame.srcdoc = file.content || "";
  frame.title = file.path || "HTML preview";
  return frame;
}

function fallbackMessage(file) {
  if (file.previewKind === "error") return file.content;
  if (file.truncated) return "文件过大，无法编辑。";
  if (file.previewKind === "image") return "图片仅支持预览。";
  return "不支持预览此文件。";
}

function errorMessage(error) {
  return error instanceof Error ? error.message : String(error);
}
