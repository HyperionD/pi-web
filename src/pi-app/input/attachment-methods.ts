const INLINE_LIMIT = 256 * 1024;
const MAX_JSON_BODY_BYTES = 1 << 20;
const PROMPT_REQUEST_BUDGET = Math.floor(MAX_JSON_BODY_BYTES * 0.75);
const LUCIDE_X_ICON = `<svg xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M18 6 6 18"></path><path d="m6 6 12 12"></path></svg>`;

export const attachmentMethods = {
  async handlePromptPaste(event) {
    const imageFiles = this.pastedImageFiles(event.clipboardData);
    if (!imageFiles.length) return;
    event.preventDefault();
    await this.addFiles(imageFiles);
  },

  pastedImageFiles(clipboardData) {
    const items = [...(clipboardData?.items || [])];
    const files = items.length ? items.map((item) => item.getAsFile?.()) : [...(clipboardData?.files || [])];
    return files.filter((file) => file?.type?.startsWith("image/")).map((file, index) => {
      if (file.name || typeof File !== "function") return file;
      return new File([file], `pasted-image-${index + 1}.${this.imageExtension(file.type)}`, {
        type: file.type,
        lastModified: file.lastModified || Date.now(),
      });
    });
  },

  imageExtension(type = "") {
    const subtype = type.split("/")[1]?.split(";")[0] || "png";
    if (subtype === "jpeg") return "jpg";
    if (subtype === "svg+xml") return "svg";
    return subtype;
  },

  async addFiles(files) {
    if (!this.attachments) return;
    let warning = "";
    for (const file of files || []) {
      if (!file) continue;
      const isImage = file.type?.startsWith("image/");
      if (!isImage && file.size > INLINE_LIMIT) {
        this.addInvalidAttachment(file, "不会内联");
        warning = "普通文件超过 256 KiB 不会内联；请先上传到工作区。";
        continue;
      }

      const candidate = isImage
        ? this.estimatedImageAttachment(file)
        : await this.fileAttachment(file);
      if (!this.promptFitsAttachment(candidate)) {
        this.addInvalidAttachment(file, "超出请求预算");
        warning = "图片或合计附件超过约 768 KiB 请求预算；请先上传到工作区。";
        continue;
      }

      const attachment = isImage ? await this.imageAttachment(file) : candidate;
      this.attachmentContents.push(attachment);
      this.addAttachmentChip(file.name, file.size, file, attachment.dataUrl);
      warning = "";
    }
    this.attachments.hidden = !this.attachments.children.length;
    this.setAttachmentWarning(warning);
    this.updatePrompt();
  },

  addInvalidAttachment(file, status) {
    this.attachmentContents.push("");
    this.addAttachmentChip(file.name, file.size, file, "", {
      sendable: false,
      status,
    });
  },

  setAttachmentWarning(message = "") {
    const warning = this.querySelector("[data-attachment-warning]");
    if (!warning) return;
    warning.textContent = message;
    warning.hidden = !message;
  },

  estimatedImageAttachment(file) {
    const mimeType = file.type || "image/png";
    return {
      type: "image",
      name: file.name,
      mimeType,
      dataUrl: "",
      dataUrlByteLength: this.utf8ByteLength(`data:${mimeType};base64,`) + 4 * Math.ceil(file.size / 3),
    };
  },

  promptFitsAttachment(candidate) {
    const attachments = [...(this.attachmentContents || []).filter(Boolean), candidate];
    return this.estimatePromptJsonBytes(this.prompt?.value || "", attachments) <= PROMPT_REQUEST_BUDGET;
  },

  normalizePromptText(text = "") {
    return String(text ?? "").trim();
  },

  canSendPrompt(text = this.prompt?.value || "", attachments = (this.attachmentContents || []).filter(Boolean)) {
    const normalizedText = this.normalizePromptText(text);
    const sendableAttachments = attachments.filter(Boolean);
    return (!!normalizedText || sendableAttachments.length > 0)
      && this.estimatePromptJsonBytes(normalizedText, sendableAttachments) <= PROMPT_REQUEST_BUDGET;
  },

  estimatePromptJsonBytes(text, attachments) {
    return this.estimateNormalPromptJsonBytes(text, attachments);
  },

  estimateNormalPromptJsonBytes(text, attachments) {
    const normalizedText = this.normalizePromptText(text);
    const serializedAttachments = this.serializePromptAttachments(attachments);
    const skeletonBytes = this.utf8ByteLength(JSON.stringify({ text: normalizedText, attachments: serializedAttachments }));
    return skeletonBytes + this.projectedDataUrlBytes(attachments);
  },

  serializePromptAttachments(attachments = []) {
    return attachments.map(({ dataUrlByteLength, ...attachment }) => attachment);
  },

  projectedDataUrlBytes(attachments = []) {
    return attachments.reduce(
      (total, attachment) => total + (attachment.dataUrlByteLength || 0),
      0,
    );
  },

  utf8ByteLength(value) {
    if (typeof TextEncoder === "function") return new TextEncoder().encode(value).length;
    return value.length;
  },

  async fileAttachment(file) {
    const content = file.size <= INLINE_LIMIT ? await file.text() : "[file too large to inline]";
    return {
      type: "file",
      name: file.name,
      mimeType: file.type || "text/plain",
      content,
    };
  },

  async imageAttachment(file) {
    return {
      type: "image",
      name: file.name,
      mimeType: file.type || "image/png",
      dataUrl: await this.fileDataUrl(file),
    };
  },

  async fileDataUrl(file) {
    const base64 = this.arrayBufferToBase64(await file.arrayBuffer());
    return `data:${file.type || "application/octet-stream"};base64,${base64}`;
  },

  arrayBufferToBase64(buffer) {
    const bytes = new Uint8Array(buffer);
    const chunkSize = 0x8000;
    let binary = "";
    for (let offset = 0; offset < bytes.length; offset += chunkSize) {
      binary += String.fromCharCode(...bytes.subarray(offset, offset + chunkSize));
    }
    return btoa(binary);
  },

  addAttachmentChip(name, size, file, previewUrl, options: { sendable?: boolean; status?: string } = {}) {
    const sendable = options.sendable !== false;
    const chip = document.createElement("span");
    chip.className = "attach-chip";
    chip.dataset.attachmentIndex = String(this.attachmentContents.length - 1);
    chip.dataset.sendable = String(sendable);
    chip.innerHTML = [
      this.attachmentPreviewMarkup(file, previewUrl),
      `<span class="ac-name"></span>`,
      `<span class="ac-size">${this.formatBytes(size)}</span>`,
      `<button class="ac-remove" type="button" data-remove-attachment aria-label="remove">${LUCIDE_X_ICON}</button>`,
    ].join("");
    chip.querySelector(".ac-name").textContent = name;
    if (!sendable) {
      chip.classList.add("is-invalid");
      const status = document.createElement("span");
      status.className = "ac-status";
      status.textContent = options.status || "不可发送";
      chip.append(status);
    }
    this.attachments.append(chip);
  },

  attachmentPreviewMarkup(file, previewUrl) {
    if (previewUrl && file?.type?.startsWith("image/")) {
      return `<img class="ac-preview" src="${previewUrl}" alt="">`;
    }
    return `<span class="ac-glyph">${file ? this.kindGlyph(file) : "file"}</span>`;
  },

  kindGlyph(file) {
    if (file.type?.startsWith("image")) return "img";
    if (file.name.endsWith(".pdf")) return "pdf";
    if (/\.(js|ts|jsx|tsx|astro|py|go|rs)$/.test(file.name)) return "&lt;/&gt;";
    return "txt";
  },

  formatBytes(size) {
    if (size < 1024) return `${size} B`;
    if (size < 1024 * 1024) return `${(size / 1024).toFixed(1)} KB`;
    return `${(size / 1024 / 1024).toFixed(1)} MB`;
  },
};
