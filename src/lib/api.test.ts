// @ts-nocheck
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  cancelSession,
  cloneWorkspace,
  createSession,
  deleteSession,
  closeWorkspace,
  deleteWorkspaceSessions,
  getGitCommit,
  getGitHistory,
  getGitStatus,
  getSession,
  getSessionCommands,
  getSessionContextUsage,
  setSessionModel,
  getWorkspaceCommands,
  getWorkspaceFile,
  getWorkspaceFiles,
  getWorkspaceModels,
  getWorkspaceRuntimeModel,
  getWorkspaceRuntimeQuota,
  getWorkspaceRuntimeStatus,
  getWorkspaceSettings,
  getWorkspaces,
  health,
  listFolders,
  openWorkspace,
  postPrompt,
  renameSession,
  steerSession,
  runShellCommand,
  saveWorkspaceFile,
  saveWorkspaceSettings,
  searchWorkspaceFiles,
  sendSessionCommand,
  sessionEvents,
  uploadWorkspaceFile,
} from "./api";

let originalXMLHttpRequest: typeof XMLHttpRequest;

describe("api adapter", () => {
  beforeEach(() => {
    originalXMLHttpRequest = globalThis.XMLHttpRequest;
    globalThis.PI_WEB_API_BASE = "http://backend.test";
    globalThis.fetch = vi.fn(async (url, options) => ({
      ok: true,
      status: 200,
      statusText: "OK",
      json: async () => ({ url, options }),
    }));
  });

  afterEach(() => {
    globalThis.XMLHttpRequest = originalXMLHttpRequest;
    delete globalThis.PI_WEB_API_BASE;
    vi.restoreAllMocks();
  });

  it("fetches workspaces from the configured backend", async () => {
    const result = await getWorkspaces();
    expect(result.url).toBe("http://backend.test/api/workspaces");
  });

  it("defaults to same-origin API paths for the embedded app", async () => {
    delete globalThis.PI_WEB_API_BASE;
    const result = await getWorkspaces();
    expect(result.url).toBe("/api/workspaces");
  });

  it("uses the development backend for known dev ports", async () => {
    delete globalThis.PI_WEB_API_BASE;
    const original = globalThis.location;
    Object.defineProperty(globalThis, "location", { value: { port: "4321" }, configurable: true });
    expect((await health()).url).toBe("http://127.0.0.1:8732/api/health");
    Object.defineProperty(globalThis, "location", { value: { port: "6006" }, configurable: true });
    expect((await health()).url).toBe("http://127.0.0.1:8732/api/health");
    Object.defineProperty(globalThis, "location", { value: original, configurable: true });
  });

  it("escapes session ids in paths and supports message paging", async () => {
    const result = await getSession("a/b", { limit: 25, before: "123" });
    expect(result.url).toBe("http://backend.test/api/sessions/a%2Fb?limit=25&before=123");
  });

  it("fetches context usage for an escaped session id", async () => {
    const result = await getSessionContextUsage("a/b");
    expect(result.url).toBe("http://backend.test/api/sessions/a%2Fb/context-usage");
  });

  it("sets the active session model", async () => {
    const result = await setSessionModel("s1", "openai-codex", "gpt-5.6-luna");
    expect(result.url).toBe("http://backend.test/api/sessions/s1/model");
    expect(result.options.method).toBe("POST");
    expect(JSON.parse(result.options.body)).toEqual({ provider: "openai-codex", model: "gpt-5.6-luna" });
  });

  it("lists folders from the backend browser", async () => {
    const result = await listFolders("~");
    expect(result.url).toBe("http://backend.test/api/system/folders?path=~");
  });

  it("creates sessions in a workspace", async () => {
    const result = await createSession("w1");
    expect(result.url).toBe("http://backend.test/api/workspaces/w1/sessions");
    expect(result.options.method).toBe("POST");
  });

  it("supports workspace and session management", async () => {
    expect((await closeWorkspace("w1")).options.method).toBe("DELETE");
    const deletedWorkspaceSessions = await deleteWorkspaceSessions("w1");
    expect(deletedWorkspaceSessions.url).toBe("http://backend.test/api/workspaces/w1/sessions");
    expect(deletedWorkspaceSessions.options.method).toBe("DELETE");
    expect((await deleteSession("s1")).options.method).toBe("DELETE");
    expect((await cancelSession("s1")).options.method).toBe("POST");
    const renamed = await renameSession("s1", "next");
    expect(renamed.options.method).toBe("PATCH");
    expect(JSON.parse(renamed.options.body)).toEqual({ title: "next" });
  });

  it("fetches session runtime commands and sends allowlisted commands", async () => {
    const commands = await getSessionCommands("s/1");
    expect(commands.url).toBe("http://backend.test/api/sessions/s%2F1/commands");
    const command = await sendSessionCommand("s/1", "copy");
    expect(command.url).toBe("http://backend.test/api/sessions/s%2F1/command");
    expect(command.options.method).toBe("POST");
    expect(JSON.parse(command.options.body)).toEqual({ command: "copy" });
  });

  it("fetches workspace slash commands and runtime status", async () => {
    const opened = await openWorkspace("/repo");
    expect(opened.url).toBe("http://backend.test/api/workspaces/open");
    expect(JSON.parse(opened.options.body)).toEqual({ path: "/repo" });
    const commands = await getWorkspaceCommands("w1");
    expect(commands.url).toBe("http://backend.test/api/workspaces/w1/commands");
    const models = await getWorkspaceModels("w1");
    expect(models.url).toBe("http://backend.test/api/workspaces/w1/models");
    const files = await getWorkspaceFiles("w/1");
    expect(files.url).toBe("http://backend.test/api/workspaces/w%2F1/files");
    const search = await searchWorkspaceFiles("w/1", "hello world");
    expect(search.url).toBe("http://backend.test/api/workspaces/w%2F1/files/search?q=hello%20world");
    const git = await getGitStatus("w/1");
    expect(git.url).toBe("http://backend.test/api/workspaces/w%2F1/git/status");
    const gitHistory = await getGitHistory("w/1", 25);
    expect(gitHistory.url).toBe("http://backend.test/api/workspaces/w%2F1/git/history?limit=25");
    const gitCommit = await getGitCommit("w/1", "a/b");
    expect(gitCommit.url).toBe("http://backend.test/api/workspaces/w%2F1/git/commit?hash=a%2Fb");
    const status = await getWorkspaceRuntimeStatus("w1");
    expect(status.url).toBe("http://backend.test/api/workspaces/w1/runtime-status");
    const model = await getWorkspaceRuntimeModel("w1");
    expect(model.url).toBe("http://backend.test/api/workspaces/w1/runtime-model");
    const personalModel = await getWorkspaceRuntimeModel("w1", "session/1");
    expect(personalModel.url).toBe("http://backend.test/api/workspaces/w1/runtime-model?sessionID=session%2F1");
    const quota = await getWorkspaceRuntimeQuota("w1", "GPT-5.5");
    expect(quota.url).toBe("http://backend.test/api/workspaces/w1/runtime-quota?model=GPT-5.5");
    const quotaDefault = await getWorkspaceRuntimeQuota("w1");
    expect(quotaDefault.url).toBe("http://backend.test/api/workspaces/w1/runtime-quota");
    const personalStatus = await getWorkspaceRuntimeStatus("w1", "session/1");
    expect(personalStatus.url).toBe("http://backend.test/api/workspaces/w1/runtime-status?sessionID=session%2F1");
  });

  it("reads and saves workspace settings and files", async () => {
    const settings = await getWorkspaceSettings("w1");
    expect(settings.url).toBe("http://backend.test/api/workspaces/w1/settings");
    const savedSettings = await saveWorkspaceSettings("w1", "project", { theme: "dark" });
    expect(savedSettings.url).toBe("http://backend.test/api/workspaces/w1/settings");
    expect(savedSettings.options.method).toBe("PUT");
    expect(JSON.parse(savedSettings.options.body)).toEqual({ scope: "project", settings: { theme: "dark" } });

    const result = await getWorkspaceFile("w1", "src/main.go");
    expect(result.url).toBe("http://backend.test/api/workspaces/w1/files/read?path=src%2Fmain.go");
    const saved = await saveWorkspaceFile("w1", "src/main.go", "package main");
    expect(saved.url).toBe("http://backend.test/api/workspaces/w1/files/write?path=src%2Fmain.go");
    expect(saved.options.method).toBe("PUT");
    expect(JSON.parse(saved.options.body)).toEqual({ content: "package main" });
  });

  it("uploads workspace files as multipart without overriding Content-Type", async () => {
    let requestBody;
    globalThis.XMLHttpRequest = class {
      upload = {};
      requestHeaders = {};
      status = 201;
      statusText = "Created";
      responseText = '{"uploaded":true}';
      open(method, url) {
        this.method = method;
        this.url = url;
      }
      setRequestHeader(name, value) {
        this.requestHeaders[name] = value;
      }
      send(body) {
        requestBody = body;
        this.onload?.();
      }
      getResponseHeader() {
        return null;
      }
    };

    const file = new File(["hello"], "hello.txt", { type: "text/plain" });
    await expect(uploadWorkspaceFile("w1", "hello.txt", file, false)).resolves.toEqual({ uploaded: true });
    expect(Array.from(requestBody.entries())).toEqual([
      ["path", "hello.txt"],
      ["overwrite", "false"],
      ["file", file],
    ]);
    expect(globalThis.fetch).not.toHaveBeenCalled();
  });

  it("posts prompts as json", async () => {
    const result = await postPrompt("s1", "hello");
    expect(result.options.method).toBe("POST");
    expect(JSON.parse(result.options.body)).toEqual({ text: "hello", attachments: [] });
  });

  it("includes the client request id for durable prompt retries", async () => {
    const result = await postPrompt("s1", "hello", [], "request-1");
    expect(JSON.parse(result.options.body)).toEqual({
      text: "hello",
      attachments: [],
      clientRequestId: "request-1",
    });
  });

  it("posts steering messages as json", async () => {
    const result = await steerSession("s1", "look here");
    expect(result.url).toBe("http://backend.test/api/sessions/s1/steer");
    expect(result.options.method).toBe("POST");
    expect(JSON.parse(result.options.body)).toEqual({ text: "look here", attachments: [] });
  });

  it("clones workspaces and runs shell commands", async () => {
    const cloned = await cloneWorkspace("/tmp", "https://example.test/repo.git", "repo");
    expect(cloned.url).toBe("http://backend.test/api/workspaces/clone");
    expect(JSON.parse(cloned.options.body)).toEqual({ parentPath: "/tmp", gitUrl: "https://example.test/repo.git", name: "repo" });
    const shell = await runShellCommand("w/1", "pwd");
    expect(shell.url).toBe("http://backend.test/api/workspaces/w%2F1/shell");
    expect(JSON.parse(shell.options.body)).toEqual({ command: "pwd" });
  });

  it("surfaces backend errors", async () => {
    globalThis.fetch = vi.fn(async () => ({
      ok: false,
      status: 418,
      statusText: "Teapot",
      json: async () => ({ error: "short and stout" }),
    }));
    await expect(getWorkspaces()).rejects.toThrow("short and stout");
    globalThis.fetch = vi.fn(async () => ({
      ok: false,
      status: 500,
      statusText: "Server Error",
      json: async () => {
        throw new Error("bad json");
      },
    }));
    await expect(getWorkspaces()).rejects.toThrow("500 Server Error");
  });

  it("creates EventSource connections for session streams", () => {
    const sources = [];
    const onEvent = vi.fn();
    const onOpen = vi.fn();
    const onError = vi.fn();
    globalThis.EventSource = class {
      constructor(url) {
        this.url = url;
        this.listeners = {};
        sources.push(this);
      }
      addEventListener(type, cb) {
        this.listeners[type] = cb;
      }
      close() {}
    };
    const source = sessionEvents("s1", { onEvent, onOpen, onError });
    expect(source.url).toBe("http://backend.test/api/sessions/s1/events");
    expect(sources[0].listeners["session.message"]).toBeTypeOf("function");
    source.onopen();
    source.onerror("err");
    sources[0].listeners["session.message"]({ data: JSON.stringify({ type: "session.message" }) });
    sources[0].listeners.heartbeat({ data: "not-json" });
    sources[0].listeners["session.message"]({
      lastEventId: "9007199254740993",
      data: JSON.stringify({ type: "session.message", id: "7" }),
    });
    expect(onOpen).toHaveBeenCalled();
    expect(onError).toHaveBeenCalledTimes(2);
    expect(onEvent).toHaveBeenCalledWith({ type: "session.message" });
    expect(onEvent).toHaveBeenCalledWith({ type: "session.message", id: "9007199254740993" });

    const activeReplay = sessionEvents("s1", { replay: "active", after: "42", snapshotAfter: "99" });
    expect(activeReplay.url).toBe("http://backend.test/api/sessions/s1/events?replay=active&after=42&snapshot=99");
    const noReplay = sessionEvents("s1", { replay: false });
    expect(noReplay.url).toBe("http://backend.test/api/sessions/s1/events?replay=false");
    const safeCursor = sessionEvents("s1", { replay: false, after: "9007199254740993" });
    expect(safeCursor.url).toBe("http://backend.test/api/sessions/s1/events?replay=false&after=9007199254740993");
  });
});
