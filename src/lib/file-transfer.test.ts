// @ts-nocheck
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  downloadWorkspaceFile,
  formatTransferBytes,
  transferPercent,
  uploadWorkspaceFile,
} from "./file-transfer";

type FakeResponse = {
  status: number;
  statusText: string;
  response?: unknown;
  responseText?: string;
  headers?: Record<string, string>;
};

class FakeXMLHttpRequest {
  static nextResponse: FakeResponse = { status: 200, statusText: "OK", responseText: "{}" };
  static instances: FakeXMLHttpRequest[] = [];
  upload = {};
  method = "";
  url = "";
  responseType = "";
  response: unknown;
  responseText = "";
  status = 0;
  statusText = "";
  requestBody: unknown;
  requestHeaders: Record<string, string> = {};
  onload: (() => void) | null = null;
  onerror: (() => void) | null = null;
  onabort: (() => void) | null = null;
  ontimeout: (() => void) | null = null;
  onprogress: ((event: ProgressEvent) => void) | null = null;

  constructor() {
    FakeXMLHttpRequest.instances.push(this);
  }

  open(method: string, url: string) {
    this.method = method;
    this.url = url;
  }

  setRequestHeader(name: string, value: string) {
    this.requestHeaders[name] = value;
  }

  getResponseHeader(name: string) {
    return FakeXMLHttpRequest.nextResponse.headers?.[name.toLowerCase()] ?? null;
  }

  send(body: unknown) {
    this.requestBody = body;
    const response = FakeXMLHttpRequest.nextResponse;
    this.status = response.status;
    this.statusText = response.statusText;
    this.response = response.response;
    this.responseText = response.responseText || "";
    if (this.upload.onprogress) this.upload.onprogress({ loaded: 4, total: 10, lengthComputable: true } as ProgressEvent);
    if (this.onprogress) this.onprogress({ loaded: 8, total: 20, lengthComputable: true } as ProgressEvent);
    this.onload?.();
  }
}

describe("file transfer transport", () => {
  beforeEach(() => {
    globalThis.PI_WEB_API_BASE = "http://backend.test";
    FakeXMLHttpRequest.instances = [];
    FakeXMLHttpRequest.nextResponse = { status: 200, statusText: "OK", responseText: '{"uploaded":true}' };
    globalThis.XMLHttpRequest = FakeXMLHttpRequest as any;
  });

  afterEach(() => {
    delete globalThis.PI_WEB_API_BASE;
    vi.restoreAllMocks();
  });

  it("uploads multipart fields in the backend contract order and reports progress", async () => {
    const progress = vi.fn();
    const file = new File(["hello"], "hello.txt", { type: "text/plain" });

    await expect(uploadWorkspaceFile("w/1", "docs/hello.txt", file, true, progress)).resolves.toEqual({ uploaded: true });

    const xhr = FakeXMLHttpRequest.instances[0];
    expect(xhr.method).toBe("POST");
    expect(xhr.url).toBe("http://backend.test/api/workspaces/w%2F1/files/upload");
    expect(Array.from((xhr.requestBody as FormData).entries())).toEqual([
      ["path", "docs/hello.txt"],
      ["overwrite", "true"],
      ["file", file],
    ]);
    expect(xhr.requestHeaders["Content-Type"]).toBeUndefined();
    expect(progress).toHaveBeenCalledWith(4, 10);
  });

  it("rejects uploads with the JSON server error and status", async () => {
    FakeXMLHttpRequest.nextResponse = {
      status: 413,
      statusText: "Payload Too Large",
      responseText: '{"error":"file exceeds limit"}',
    };

    await expect(uploadWorkspaceFile("w1", "large.bin", new File(["x"], "large.bin"), false)).rejects.toMatchObject({
      message: "file exceeds limit",
      status: 413,
    });
  });

  it("downloads a Blob, filename, and download progress", async () => {
    const progress = vi.fn();
    const blob = new Blob(["downloaded"], { type: "text/plain" });
    FakeXMLHttpRequest.nextResponse = {
      status: 200,
      statusText: "OK",
      response: blob,
      headers: { "content-disposition": 'attachment; filename="hello.txt"' },
    };

    await expect(downloadWorkspaceFile("w1", "docs/hello.txt", progress)).resolves.toEqual({ blob, filename: "hello.txt" });
    expect(progress).toHaveBeenCalledWith(8, 20);
    expect(FakeXMLHttpRequest.instances[0].responseType).toBe("blob");
  });

  it("formats transfer sizes and percentages", () => {
    expect(formatTransferBytes(0)).toBe("0 B");
    expect(formatTransferBytes(1024)).toBe("1 KiB");
    expect(formatTransferBytes(12.4 * 1024 * 1024)).toBe("12.4 MiB");
    expect(transferPercent(25, 100)).toBe(25);
    expect(transferPercent(25)).toBeUndefined();
    expect(transferPercent(25, 0)).toBeUndefined();
  });
});
