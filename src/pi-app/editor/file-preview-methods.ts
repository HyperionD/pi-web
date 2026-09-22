import { getWorkspaceFile, getWorkspaceFileAssetURL, getWorkspaceFileWatchURL, getWorkspaceGitDiff, saveWorkspaceFile } from "../../lib/api"
import { fileDisplayKind, isHTMLPath, isMarkdownPath, previewStateKey } from "../../lib/file-preview-model";
import { renderMarkdownBody } from "../../lib/renderers";
import { CodeMirrorFileEditor, codeMirrorLanguageName, editableFileState, isTextFile } from "./file-editor";

export const filePreviewMethods = {
  async openFile(button) {
    return this.openFilePath?.(button?.dataset.filePath, button);
  },

  async openFilePath(path, button = undefined, pdfPage = undefined) {
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
      this.renderFilePreview(pdfPage ? { ...file, initialPage: pdfPage } : file);
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
    this.closeFilePreviewWatch?.();
    this.destroyFilePreviewEditor?.();
    preview.hidden = false;
    this.toggleFilePreviewPanel?.(true);
    const workspaceId = this.dataset.activeWorkspaceId;
    const savedState = readPreviewState(workspaceId, file.path);
    this.filePreview = {
      workspaceId,
      file,
      mode: restoredPreviewMode(file, savedState?.mode),
      cleanContent: file.content || "",
      originalContent: gitOriginalContent(file),
      dirty: false,
      editor: undefined,
      saveStatus: "",
    };
    setPreviewHeader(preview, this.filePreview);
    this.renderFilePreviewBody();
    savePreviewState(this.filePreview);
    if (file.previewKind !== "loading") this.startFilePreviewWatch?.();
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
    if (mode === "diff") {
      body.append(diffPreviewNode(state.diff || ""));
      return;
    }
    if (mode === "markdown" && isMarkdownFile(file)) {
      body.classList.add("markdown-body");
      body.innerHTML = renderMarkdownBody(`${file.content || ""}${file.truncated ? "\n\n[truncated]" : ""}`);
      resolveMarkdownImages(body, file, state.workspaceId);
      resolveMarkdownLinks(body, file);
      return;
    }
    if (kind === "html" && mode === "preview") {
      body.append(htmlPreviewNode(file));
      return;
    }
    if (kind === "image" && mode === "image") {
      body.append(imagePreviewNode(file, state.workspaceId));
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

  startFilePreviewWatch() {
    this.closeFilePreviewWatch?.();
    const state = this.filePreview;
    if (!state?.workspaceId || !state.file?.path || typeof EventSource === "undefined") return;
    const source = new EventSource(getWorkspaceFileWatchURL(state.workspaceId, state.file.path), { withCredentials: true });
    source.addEventListener("change", () => void this.refreshFilePreviewFromWatch?.());
    source.addEventListener("error", () => undefined);
    state.watchSource = source;
  },

  closeFilePreviewWatch() {
    this.filePreview?.watchSource?.close?.();
    if (this.filePreview) this.filePreview.watchSource = undefined;
  },

  async refreshFilePreviewFromWatch() {
    const state = this.filePreview;
    if (!state?.workspaceId || !state.file?.path) return;
    if (state.dirty) {
      state.saveStatus = "external change";
      updatePreviewActions(this.querySelector("[data-file-preview]"), state);
      return;
    }
    const next = await getWorkspaceFile(state.workspaceId, state.file.path);
    if (this.filePreview !== state || state.file.path !== next.path || state.dirty) return;
    state.file = next;
    state.cleanContent = next.content || "";
    state.originalContent = gitOriginalContent(next);
    state.saveStatus = "";
    this.renderFilePreviewBody();
    savePreviewState(state);
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
    this.closeFilePreviewWatch?.();
    this.beginInspectorRequest?.();
    this.destroyFilePreviewEditor?.();
    this.filePreview = undefined;
    this.toggleFilePreviewPanel?.(false);
    this.querySelector("[data-file-preview]")?.setAttribute("hidden", "");
  },

  async toggleFilePreviewDiff() {
    const state = this.filePreview;
    const workspaceId = this.dataset.activeWorkspaceId;
    if (!state?.file?.path || !workspaceId || !this.confirmCleanFilePreview?.()) return;
    if (state.mode === "diff") {
      state.mode = defaultPreviewMode(state.file);
      this.renderFilePreviewBody();
      return;
    }
    const result = await getWorkspaceGitDiff(workspaceId, state.file.path);
    if (this.filePreview !== state || this.dataset.activeWorkspaceId !== workspaceId) return;
    if (result.supported === false) return;
    state.diffSupported = true;
    state.diff = result.patch || result.diff || result.content || "";
    state.mode = "diff";
    this.renderFilePreviewBody();
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
    savePreviewState(this.filePreview);
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
  const diff = preview.querySelector("[data-action='toggle-file-preview-diff']");
  const workspaceChanged = state.saveStatus === "workspace changed";
  save.hidden = !canEdit || mode !== "text";
  save.disabled = !state.dirty || workspaceChanged;
  save.textContent = workspaceChanged
    ? "workspace changed"
    : state.dirty ? "save *" : state.saveStatus === "saved" ? "saved" : "save";
  save.title = state.saveStatus && state.saveStatus !== "saved" ? state.saveStatus : "";
  toggle.hidden = !canToggle;
  if (diff) {
    diff.hidden = (!file.originalContent && !state.diffSupported) || mode === "diff";
    diff.textContent = mode === "diff" ? "文件" : "Diff";
  }
  const switchToText = mode === "markdown" || mode === "image" || mode === "preview";
  toggle.textContent = switchToText ? "文本" : "预览";
  toggle.setAttribute("aria-label", switchToText ? "切换到文本模式" : "切换到预览模式");
  toggle.title = switchToText ? "切换到文本模式" : "切换到预览模式";
  setPreviewHeader(preview, state);
}

function gitOriginalContent(file) {
  return typeof file?.originalContent === "string" ? file.originalContent : file?.content || "";
}

function restoredPreviewMode(file, savedMode) {
  const defaultMode = defaultPreviewMode(file);
  if (!savedMode || savedMode === "unsupported") return defaultMode;
  const kind = fileDisplayKind(file);
  if (kind === "image" && file.mime !== "image/svg+xml") return "image";
  if (["audio", "video", "pdf"].includes(kind)) return kind;
  if (kind === "html" && savedMode !== "preview" && savedMode !== "text") return defaultMode;
  if (kind === "markdown" && savedMode !== "markdown" && savedMode !== "text") return defaultMode;
  if (kind === "text" && savedMode !== "text") return defaultMode;
  return savedMode;
}

function defaultPreviewMode(file) {
  const kind = fileDisplayKind(file);
  if (kind === "image") return "image";
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

function resolveMarkdownLinks(body, file) {
  if (!file?.path) return;
  const baseURL = new URL(workspaceFileURLPath(file.path), "https://pi-web-workspace.local/");
  body.querySelectorAll("a[href]").forEach((anchor) => {
    const source = anchor.getAttribute("href");
    if (!source || !isRelativeWorkspaceImageSource(source)) return;
    let resolved;
    try {
      resolved = new URL(source, baseURL);
    } catch {
      return;
    }
    if (resolved.origin !== baseURL.origin) return;
    const path = decodeURIComponent(resolved.pathname).replace(/^\/+/, "");
    if (!path) return;
    anchor.addEventListener("click", (event) => {
      event.preventDefault();
      const page = resolved.hash.match(/^#page=(\d+)$/i)?.[1];
      window.dispatchEvent(new CustomEvent("pi-workspace-file:open", {
        detail: { path, page: page ? Number(page) : undefined },
      }));
    });
  });
}

function workspaceFileURLPath(path) {
  return String(path).split("/").map(encodeURIComponent).join("/");
}

function imagePreviewNode(file, workspaceId) {
  const image = document.createElement("img");
  image.src = file.dataUrl || getWorkspaceFileAssetURL(workspaceId, file.path);
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
  const page = Number(file.initialPage || 0);
  frame.src = `${getWorkspaceFileAssetURL(workspaceId, file.path)}${page > 0 ? `#page=${page}` : ""}`;
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

function diffPreviewNode(diff) {
  const pre = document.createElement("pre");
  pre.className = "fp-diff-preview";
  pre.textContent = diff;
  return pre;
}

function fallbackMessage(file) {
  if (file.previewKind === "error") return file.content;
  if (file.truncated) return "文件过大，无法编辑。";
  if (file.previewKind === "image") return "图片仅支持预览。";
  return "不支持预览此文件。";
}

function readPreviewState(workspaceId, path) {
  if (!workspaceId || !path) return undefined;
  try {
    const value = JSON.parse(localStorage.getItem(previewStateKey(workspaceId, path)) || "null");
    return value && typeof value === "object" ? value : undefined;
  } catch {
    return undefined;
  }
}

function savePreviewState(state) {
  if (!state?.workspaceId || !state.file?.path) return;
  try {
    localStorage.setItem(previewStateKey(state.workspaceId, state.file.path), JSON.stringify({ mode: state.mode }));
  } catch {
    // Storage can be unavailable in private browsing or restricted documents.
  }
}

function errorMessage(error) {
  return error instanceof Error ? error.message : String(error);
}
