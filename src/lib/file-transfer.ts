import { apiBase } from "./api";

export type TransferProgress = (loaded: number, total?: number) => void;

type TransferOptions = {
  body?: FormData | null;
  onProgress?: TransferProgress;
  responseType?: XMLHttpRequestResponseType;
  uploadProgress?: boolean;
};

function transferRequest<T>(
  method: string,
  path: string,
  options: TransferOptions = {},
): Promise<{ response: T; xhr: XMLHttpRequest }> {
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest();
    xhr.open(method, `${apiBase()}${path}`);
    if (options.responseType) xhr.responseType = options.responseType;
    if (options.uploadProgress && options.onProgress) {
      xhr.upload.onprogress = (event) => options.onProgress?.(event.loaded, event.lengthComputable === false ? undefined : event.total || undefined);
    }
    if (!options.uploadProgress && options.onProgress) {
      xhr.onprogress = (event) => options.onProgress?.(event.loaded, event.lengthComputable === false ? undefined : event.total || undefined);
    }
    xhr.onload = () => {
      if (xhr.status < 200 || xhr.status >= 300) {
        reject(createTransferError(xhr));
        return;
      }
      resolve({ response: readSuccess<T>(xhr), xhr });
    };
    xhr.onerror = () => reject(new Error("Network request failed"));
    xhr.onabort = () => reject(new Error("Request aborted"));
    xhr.ontimeout = () => reject(new Error("Request timed out"));
    xhr.send(options.body ?? null);
  });
}

function readSuccess<T>(xhr: XMLHttpRequest): T {
  if (xhr.responseType === "blob") return xhr.response as T;
  const text = xhr.responseText;
  return text ? (JSON.parse(text) as T) : (undefined as T);
}

function createTransferError(xhr: XMLHttpRequest): Error & { status?: number } {
  let message = `${xhr.status} ${xhr.statusText}`.trim();
  const text = readResponseText(xhr);
  if (text) {
    try {
      const body = JSON.parse(text);
      if (typeof body?.error === "string") message = body.error;
    } catch {
      // Keep the HTTP status when the server response is not JSON.
    }
  }
  const error = new Error(message) as Error & { status?: number };
  error.status = xhr.status;
  return error;
}

function readResponseText(xhr: XMLHttpRequest): string {
  try {
    return typeof xhr.responseText === "string" ? xhr.responseText : "";
  } catch {
    return typeof xhr.response === "string" ? xhr.response : "";
  }
}

function transferFilename(xhr: XMLHttpRequest, path: string): string {
  const disposition = xhr.getResponseHeader("Content-Disposition") || "";
  const encoded = disposition.match(/filename\*=UTF-8''([^;]+)/i)?.[1];
  if (encoded) return decodeURIComponent(encoded);
  const plain = disposition.match(/filename\s*=\s*"([^"]+)"/i)?.[1] || disposition.match(/filename\s*=\s*([^;]+)/i)?.[1];
  return plain?.trim() || path.split("/").pop() || "download";
}

export function uploadWorkspaceFile(
  workspaceId: string,
  path: string,
  file: File,
  overwrite: boolean,
  onProgress?: TransferProgress,
): Promise<unknown> {
  const formData = new FormData();
  formData.append("path", path);
  formData.append("overwrite", String(overwrite));
  formData.append("file", file);
  return transferRequest("POST", `/api/workspaces/${encodeURIComponent(workspaceId)}/files/upload`, {
    body: formData,
    onProgress,
    uploadProgress: true,
  }).then(({ response }) => response);
}

export function downloadWorkspaceFile(
  workspaceId: string,
  path: string,
  onProgress?: TransferProgress,
): Promise<{ blob: Blob; filename: string }> {
  return transferRequest<Blob>(
    "GET",
    `/api/workspaces/${encodeURIComponent(workspaceId)}/files/raw?path=${encodeURIComponent(path)}`,
    { responseType: "blob", onProgress },
  ).then(({ response, xhr }) => ({ blob: response, filename: transferFilename(xhr, path) }));
}

export function formatTransferBytes(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes <= 0) return "0 B";
  const units = ["B", "KiB", "MiB", "GiB"];
  const unitIndex = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1);
  const value = bytes / 1024 ** unitIndex;
  const formatted = value >= 100 ? value.toFixed(0) : value >= 10 ? value.toFixed(1) : value.toFixed(2);
  return `${Number(formatted)} ${units[unitIndex]}`;
}

export function transferPercent(loaded: number, total?: number): number | undefined {
  if (!Number.isFinite(total) || !total || total < 0) return undefined;
  return Math.round((loaded / total) * 100);
}
