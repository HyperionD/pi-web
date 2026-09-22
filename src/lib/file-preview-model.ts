export type FileDisplayKind = "text" | "markdown" | "html" | "image" | "audio" | "video" | "pdf" | "unsupported";

type FilePreviewLike = {
  path?: string;
  mime?: string;
  previewKind?: string;
};

export function fileDisplayKind(file: FilePreviewLike): FileDisplayKind {
  const previewKind = String(file.previewKind || "");
  if (["image", "audio", "video", "pdf", "html"].includes(previewKind)) {
    return previewKind as FileDisplayKind;
  }
  const mime = String(file.mime || "").toLowerCase();
  if (mime.startsWith("image/")) return "image";
  if (mime.startsWith("audio/")) return "audio";
  if (mime.startsWith("video/")) return "video";
  if (mime === "application/pdf") return "pdf";
  if (previewKind === "text" || mime.startsWith("text/") || mime === "application/json" || isPlainTextPath(file.path || "")) {
    return isMarkdownPath(file.path || "") ? "markdown" : isHTMLPath(file.path || "") ? "html" : "text";
  }
  return "unsupported";
}

export function isMarkdownPath(path: string): boolean {
  return /\.(?:md|markdown|mdx)$/i.test(normalizePath(path));
}

export function isHTMLPath(path: string): boolean {
  return /\.(?:html?|xhtml)$/i.test(normalizePath(path));
}

export function isMediaPath(path: string): boolean {
  return /\.(?:png|jpe?g|gif|webp|svg|bmp|ico|avif|mp3|wav|ogg|oga|opus|m4a|aac|flac|weba|mp4|m4v|webm|mov|ogv)$/i.test(normalizePath(path));
}

export function previewStateKey(workspaceId: string, path: string): string {
  return `pi-web:file-preview:${encodeURIComponent(workspaceId)}:${encodeURIComponent(path)}`;
}

function normalizePath(path: string): string {
  return String(path || "").replaceAll("\\", "/");
}

function isPlainTextPath(path: string): boolean {
  const name = normalizePath(path).split("/").pop()?.toLowerCase() || "";
  if ([".gitignore", ".npmignore", ".dockerignore", ".editorconfig", ".env", "license", "readme", "makefile", "dockerfile"].includes(name)) {
    return true;
  }
  return /\.(?:md|markdown|mdx|txt|log|json|ya?ml|toml|ini|conf|cfg|csv|tsv|xml|css|scss|less|js|jsx|mjs|cjs|ts|tsx|vue|svelte|astro|go|rs|py|rb|php|java|kt|swift|c|h|cpp|hpp|sh|bash|zsh|fish|sql)$/i.test(name);
}
