// @ts-nocheck
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanupPiAppFixture, connectPiApp, installPiAppFixture } from "../test-helper";

function ok(body = {}) {
  return { ok: true, status: 200, statusText: "OK", json: async () => body };
}

function err(message = "boom") {
  return { ok: false, status: 500, statusText: "ERR", json: async () => ({ error: message }) };
}

describe("pi-app input methods coverage", () => {
  beforeEach(() => {
    installPiAppFixture();
    globalThis.PI_WEB_API_BASE = "http://backend.test";
  });
  afterEach(cleanupPiAppFixture);

  it("covers prompt submit guards, local echo, creation failure, and post failure", async () => {
    const app = await connectPiApp();
    app.submitSteeringPrompt = vi.fn();
    app.running = true;
    app.prompt.value = "steer";
    await app.submitPrompt();
    expect(app.submitSteeringPrompt).toHaveBeenCalledWith("steer");

    app.running = false;
    app.submitSteeringPrompt = vi.fn();
    app.prompt.value = "";
    await app.submitPrompt();
    expect(app.submitSteeringPrompt).not.toHaveBeenCalled();

    app.apiConnected = false;
    app.prompt.value = "offline";
    await app.submitPrompt();
    expect(app.querySelector(".msg[data-kind='user'] .body").textContent).toBe("offline");

    app.apiConnected = true;
    app.dataset.activeSessionId = "";
    app.dataset.activeWorkspaceId = "w1";
    app.setConnection = vi.fn();
    globalThis.fetch = vi.fn(async () => err("cannot create"));
    app.prompt.value = "create";
    await app.submitPrompt();
    expect(app.setConnection).toHaveBeenCalledWith("err");

    globalThis.fetch = vi.fn(async (url) => String(url).includes("/workspaces/w1/sessions")
      ? ok({ session: { id: "s-new", title: "new" } })
      : ok({ accepted: true }));
    app.activateCreatedSession = vi.fn((workspaceId, session) => { app.dataset.activeSessionId = session.id; });
    app.prompt.value = "created";
    await app.submitPrompt();
    expect(app.activateCreatedSession).toHaveBeenCalledWith("w1", { id: "s-new", title: "new" });

    app.dataset.activeSessionId = "s1";
    app.setMode = vi.fn();
    globalThis.fetch = vi.fn(async () => err("cannot post"));
    app.prompt.value = "post";
    await app.submitPrompt();
    expect(app.setConnection).toHaveBeenCalledWith("err");
    app.prompt = null;
    app.attachments.append(document.createElement("span"));
    app.apiConnected = false;
    await app.submitPrompt();
  });

  it("uses the durable prompt endpoint when durable events are enabled", async () => {
    const app = await connectPiApp();
    app.apiConnected = true;
    app.durableEvents = true;
    app.dataset.activeSessionId = "s1";
    app.dataset.activeWorkspaceId = "w1";
    app.prompt.value = "durable prompt";
    app.invalidateEventStream = vi.fn();
    app.connectEvents = vi.fn();
    app.appendLoadingMessage = vi.fn();
    app.setMode = vi.fn();
    globalThis.EventSource = class {};
    globalThis.fetch = vi.fn(async () => ok({ accepted: true, runId: "run-1" }));

    await app.submitPrompt();

    expect(app.invalidateEventStream).toHaveBeenCalledTimes(1);
    expect(globalThis.fetch).toHaveBeenCalledWith(
      "http://backend.test/api/sessions/s1/prompt",
      expect.objectContaining({ method: "POST" }),
    );
    expect(app.connectEvents).not.toHaveBeenCalled();
    globalThis.EventSource = undefined;
  });

  it("covers optional DOM absences and outside clicks", async () => {
    const app = await connectPiApp();
    const prompt = app.prompt;
    const attachments = app.attachments;
    app.prompt = null;
    app.attachments = document.createElement("div");
    app.attachments.append(document.createElement("span"));
    app.apiConnected = true;
    app.dataset.activeSessionId = "s1";
    globalThis.fetch = vi.fn(async () => ok({ accepted: true }));
    await app.submitPrompt();
    await app.submitSteeringPrompt("x");
    const form = document.createElement("form");
    form.innerHTML = `<input name="command" value="pwd">`;
    app.dataset.activeWorkspaceId = "w1";
    await app.submitShellCommand({ preventDefault: vi.fn(), currentTarget: form });
    app.findSessionRow = vi.fn(() => null);
    const row = document.createElement("button");
    row.dataset.session = "s-fallback";
    row.dataset.workspace = "w1";
    app.append(row);
    expect(app.activeSessionBelongsToWorkspace("s-fallback", "w1")).toBe(true);
    const outside = document.createElement("button");
    outside.dataset.action = "route-picker";
    app.handleAppClick({ target: outside });
    const actionOnly = document.createElement("span");
    actionOnly.dataset.action = "route-picker";
    app.append(actionOnly);
    actionOnly.click();
    const chip = document.createElement("span");
    chip.className = "attach-chip";
    chip.dataset.attachmentIndex = "nan";
    chip.innerHTML = `<button data-remove-attachment></button>`;
    app.append(chip);
    chip.querySelector("button").click();
    app.prompt = prompt;
    app.attachments = attachments;
  });

  it("covers connected prompt and steering error exits", async () => {
    const app = await connectPiApp();
    app.apiConnected = true;
    app.dataset.activeSessionId = "s1";
    app.prompt.value = "fail";
    globalThis.fetch = vi.fn(async () => err("post fail"));
    await app.submitPrompt();
    expect(app.querySelector(".msg.loading")).toBeNull();
    app.dataset.activeSessionId = "";
    await app.submitSteeringPrompt("ignored");
    app.dataset.activeSessionId = "s1";
    app.prompt = null;
    await app.submitSteeringPrompt("no prompt");
  });

  it("reports durable prompt failures without a completion notification", async () => {
    const app = await connectPiApp();
    app.apiConnected = true;
    app.durableEvents = true;
    app.dataset.activeSessionId = "s-post";
    app.prompt.value = "post failure";
    app.notifyResponseFailure = vi.fn((detail) => {
      app.responseFailureToastShown = true;
      app.responseFailureDetail = String(detail);
    });
    app.notifyResponseCompletedOnce = vi.fn();
    globalThis.fetch = vi.fn(async () => err("post failed"));

    await app.submitPrompt();

    expect(app.notifyResponseFailure).toHaveBeenCalledWith(expect.any(Error));
    expect(app.notifyResponseCompletedOnce).not.toHaveBeenCalled();
  });

  it("marks post failures while waiting for server echo", async () => {
    const app = await connectPiApp();
    app.apiConnected = true;
    app.dataset.activeSessionId = "s-post";
    app.setMode = vi.fn();
    app.setConnection = vi.fn();
    globalThis.fetch = vi.fn(async () => err("post failed"));
    app.prompt.value = "post failure";

    await app.submitPrompt();

    expect(app.setMode).toHaveBeenCalledWith("running");
    expect(app.setMode).toHaveBeenCalledWith("idle");
    expect(app.setConnection).toHaveBeenCalledWith("err");
  });

  it("creates a session in the visible workspace when the active session belongs to another workspace", async () => {
    const app = await connectPiApp();
    app.apiConnected = true;
    app.dataset.activeWorkspaceId = "ws-a";
    app.dataset.activeSessionId = "s-b";
    const oldRow = document.createElement("div");
    oldRow.dataset.session = "s-b";
    oldRow.dataset.workspace = "ws-b";
    app.append(oldRow);
    app.activateCreatedSession = vi.fn((workspaceId, session) => { app.dataset.activeSessionId = session.id; });
    globalThis.fetch = vi.fn(async (url) => String(url).includes("/workspaces/ws-a/sessions")
      ? ok({ session: { id: "s-a", title: "new" } })
      : ok({ accepted: true }));
    app.prompt.value = "work in A";

    await app.submitPrompt();

    const urls = globalThis.fetch.mock.calls.map(([url]) => String(url));
    expect(urls.some((url) => url.includes("/workspaces/ws-a/sessions"))).toBe(true);
    expect(urls.some((url) => url.includes("/sessions/s-a/"))).toBe(true);
    expect(urls.some((url) => url.includes("/sessions/s-b/"))).toBe(false);
  });

  it("prevents duplicate initial submits while a session is being created", async () => {
    const app = await connectPiApp();
    app.apiConnected = true;
    app.dataset.activeWorkspaceId = "w1";
    app.dataset.activeSessionId = "";
    let resolveCreate = () => undefined;
    globalThis.EventSource = undefined;
    globalThis.fetch = vi.fn(async (url) => {
      if (String(url).includes("/workspaces/w1/sessions")) {
        return new Promise((resolve) => {
          resolveCreate = () => resolve(ok({ session: { id: "s-new", title: "new" } }));
        });
      }
      return ok({ accepted: true });
    });
    app.activateCreatedSession = vi.fn((workspaceId, session) => { app.dataset.activeSessionId = session.id; });
    app.prompt.value = "create once";

    const firstSubmit = app.submitPrompt();
    const secondSubmit = app.submitPrompt();
    await Promise.resolve();
    const createCalls = globalThis.fetch.mock.calls.filter(([url]) => String(url).includes("/workspaces/w1/sessions"));
    expect(createCalls).toHaveLength(1);
    resolveCreate();
    await Promise.all([firstSubmit, secondSubmit]);
  });

  it("allows M007 prompts in another session while the first submit is pending", async () => {
    const app = await connectPiApp();
    app.apiConnected = true;
    app.dataset.activeWorkspaceId = "w1";
    app.dataset.activeSessionId = "s1";
    globalThis.EventSource = undefined;
    let resolveFirst;
    globalThis.fetch = vi.fn(async (url) => {
      if (String(url).includes("/sessions/s1/prompt")) {
        return new Promise((resolve) => { resolveFirst = resolve; });
      }
      return ok({ accepted: true });
    });

    app.prompt.value = "first session";
    const firstSubmit = app.submitPrompt();
    await Promise.resolve();

    app.dataset.activeSessionId = "s2";
    app.resetActiveSessionState();
    app.prompt.value = "second session";
    await app.submitPrompt();

    expect(globalThis.fetch.mock.calls.map(([url]) => String(url))).toContain(
      "http://backend.test/api/sessions/s2/prompt",
    );
    resolveFirst(ok({ accepted: true }));
    await firstSubmit;
  });

  it("does not submit a prompt that only has a real non-sendable attachment chip", async () => {
    const app = await connectPiApp();
    app.apiConnected = true;
    app.dataset.activeWorkspaceId = "w1";
    app.dataset.activeSessionId = "s1";
    await app.addFiles([new File(["a".repeat(256 * 1024 + 1)], "large.txt", { type: "text/plain" })]);
    globalThis.fetch = vi.fn(async () => ok({ accepted: true }));

    expect(app.querySelector(".attach-chip.is-invalid")?.dataset.sendable).toBe("false");
    await app.submitPrompt();

    expect(globalThis.fetch).not.toHaveBeenCalled();
    expect(app.querySelector(".send-btn").disabled).toBe(true);
    expect(app.querySelector(".send-btn").getAttribute("aria-disabled")).toBe("true");
  });


  it("normalizes trailing whitespace before prompt budget checks and submit", async () => {
    const app = await connectPiApp();
    app.apiConnected = true;
    app.dataset.activeWorkspaceId = "w1";
    app.dataset.activeSessionId = "s1";
    globalThis.EventSource = undefined;
    const normalizedText = "x".repeat(350 * 1024);
    app.prompt.value = normalizedText + " ".repeat(450 * 1024);
    globalThis.fetch = vi.fn(async () => ok({ accepted: true }));

    app.updatePrompt();
    expect(app.querySelector(".send-btn").disabled).toBe(false);
    await app.submitPrompt();

    const body = JSON.parse(globalThis.fetch.mock.calls.at(-1)[1].body);
    expect(body.text).toBe(normalizedText);
  });


  it("guards oversized steering payloads before calling the API", async () => {
    const app = await connectPiApp();
    app.apiConnected = true;
    app.dataset.activeWorkspaceId = "w1";
    app.dataset.activeSessionId = "s1";
    app.attachmentContents = [{ type: "file", name: "accepted.txt", mimeType: "text/plain", content: "a".repeat(256 * 1024) }];
    app.prompt.value = "x".repeat(600 * 1024);
    globalThis.fetch = vi.fn(async () => ok({ accepted: true }));

    await app.submitSteeringPrompt(app.prompt.value);

    expect(globalThis.fetch).not.toHaveBeenCalled();
  });

  it("renders attachment-only prompts in the transcript", async () => {
    const app = await connectPiApp();
    app.apiConnected = false;
    app.prompt.value = "";
    app.attachmentContents = [{ type: "image", dataUrl: "data:image/png;base64,aa", name: "shot.png" }];
    app.attachments.append(document.createElement("span"));
    app.attachments.hidden = false;

    await app.submitPrompt();

    expect(app.querySelector(".msg[data-kind='user'] .msg-image")).toBeTruthy();
    expect(app.querySelector(".msg.loading")).toBeTruthy();
    expect(app.attachments.hidden).toBe(true);
  });

  it("submits prompts and steering with attachments", async () => {
    const app = await connectPiApp();
    app.apiConnected = true;
    app.dataset.activeSessionId = "s1";
    app.attachmentContents = ["", "image-data"];
    app.attachments.hidden = false;
    app.attachments.append(document.createElement("span"));
    globalThis.fetch = vi.fn(async () => ok({ accepted: true }));
    app.prompt.value = "hello";

    await app.submitPrompt();
    expect(JSON.parse(globalThis.fetch.mock.calls.at(-1)[1].body)).toMatchObject({ text: "hello", attachments: ["image-data"] });
    expect(app.attachments.hidden).toBe(true);

    app.setMode("running");
    app.prompt.value = "more";
    app.attachmentContents = ["attach"];
    app.attachments.hidden = false;
    app.attachments.append(document.createElement("span"));
    await app.submitPrompt();
    expect(String(globalThis.fetch.mock.calls.at(-1)[0])).toContain("/steer");
    expect([...app.querySelectorAll(".msg[data-kind='user'] .body")].at(-1).textContent).toBe("more");
    expect(app.prompt.value).toBe("");

    globalThis.fetch = vi.fn(async () => err("steer fail"));
    app.setConnection = vi.fn();
    app.prompt.value = "bad";
    await app.submitSteeringPrompt("bad");
    expect(app.setConnection).toHaveBeenCalledWith("err");
  });


  it("submits fallback choices through durable prompt transport", async () => {
    const app = await connectPiApp();
    app.apiConnected = true;
    app.durableEvents = true;
    app.dataset.activeSessionId = "s1";
    app.connectEvents = vi.fn();
    app.connectDurableEvents = vi.fn();
    globalThis.EventSource = class {};
    const panel = document.createElement("div");
    panel.innerHTML = `<button></button><input>`;
    globalThis.fetch = vi.fn(async () => ok({ accepted: true, runId: "run-1" }));

    await app.submitFallbackChoice("choice-1", "A", panel);

    expect(String(globalThis.fetch.mock.calls[0][0])).toContain("/api/sessions/s1/prompt");
    expect(app.connectEvents).not.toHaveBeenCalled();
    expect(app.connectDurableEvents).not.toHaveBeenCalled();
  });


  it("parses every settings input before starting either save", async () => {
    const app = await connectPiApp();
    app.apiConnected = true;
    app.dataset.activeWorkspaceId = "w1";
    app.settingsState = { project: {}, global: {}, effective: {}, paths: {} };
    app.querySelector("[data-app-setting='brand']").value = "Custom";
    app.settingsPatchFromForm = vi.fn(() => ({ defaultThinkingLevel: "not-valid" }));
    globalThis.fetch = vi.fn(async () => ok({ config: { version: 1, brand: "Custom" }, path: "/config" }));

    await app.saveSettingsForm({ preventDefault: vi.fn() });

    const writes = globalThis.fetch.mock.calls.filter(([, options]) => ["PATCH", "PUT"].includes(options?.method));
    expect(writes).toHaveLength(0);
    expect(app.querySelector("[data-settings-status]").textContent).toContain("invalid settings patch");
  });

  it("starts workspace and app-config bootstrap requests while both are pending", async () => {
    const app = await connectPiApp();
    let releaseAppConfig;
    let releaseWorkspaces;
    let appConfigStarted = false;
    let workspacesStarted = false;
    let bootstrapFinished = false;
    const appConfig = new Promise((resolve) => { releaseAppConfig = resolve; });
    const workspaces = new Promise((resolve) => { releaseWorkspaces = resolve; });
    globalThis.fetch = vi.fn((url) => {
      if (String(url).endsWith("/app-config")) {
        appConfigStarted = true;
        return appConfig;
      }
      if (String(url).endsWith("/workspaces")) {
        workspacesStarted = true;
        return workspaces;
      }
      return Promise.resolve(ok({}));
    });
    app.renderWorkspaces = vi.fn();
    app.loadWorkspaceMeta = vi.fn();
    app.loadSession = vi.fn();

    const bootstrap = app.bootstrapAPI().finally(() => { bootstrapFinished = true; });
    await Promise.resolve();

    expect(appConfigStarted).toBe(true);
    expect(workspacesStarted).toBe(true);
    expect(bootstrapFinished).toBe(false);
    releaseAppConfig(ok({ config: { version: 1, brand: "Custom" }, path: "/config" }));
    await Promise.resolve();
    expect(bootstrapFinished).toBe(false);
    releaseWorkspaces(ok({ workspaces: [] }));
    await bootstrap;
    expect(bootstrapFinished).toBe(true);
  });

  it("reports each save failure without showing saved", async () => {
    const app = await connectPiApp();
    app.apiConnected = true;
    app.dataset.activeWorkspaceId = "w1";
    app.settingsState = { project: {}, global: {}, effective: {}, paths: {} };
    app.appConfigState = { config: { version: 1, brand: "pi.dev / web" }, path: "/config" };
    app.fillAppConfigForm();
    app.querySelector("[data-app-setting='brand']").value = "Custom";
    app.settingsPatchFromForm = vi.fn(() => ({ defaultThinkingLevel: "high" }));
    globalThis.fetch = vi.fn(async (url, options = {}) => {
      if (options.method === "PATCH") return err("brand save failed");
      if (options.method === "PUT") return err("settings save failed");
      return ok({});
    });

    await app.saveSettingsForm({ preventDefault: vi.fn() });

    const status = app.querySelector("[data-settings-status]");
    expect(status.textContent).toContain("brand save failed");
    expect(status.textContent).toContain("settings save failed");
    expect(status.textContent).not.toContain("saved");
    expect(status.classList.contains("err")).toBe(true);
  });

  it("runs shell commands through success, nonzero, and failure paths", async () => {
    const app = await connectPiApp();
    const form = document.createElement("form");
    form.innerHTML = `<input name="command"><button type="submit"></button>`;
    app.append(form);
    app.apiConnected = true;
    app.dataset.activeWorkspaceId = "w1";
    app.renderMessages([]);
    app.loadRuntimeStatus = vi.fn();
    app.loadWorkspaceMeta = vi.fn();
    form.querySelector("input").value = "echo ok";
    globalThis.fetch = vi.fn(async () => ok({ exitCode: 0, durationMs: 1, output: "ok" }));

    await app.submitShellCommand({ preventDefault: vi.fn(), currentTarget: form });
    expect(app.querySelector(".tool-card").dataset.status).toBe("ok");
    expect(form.querySelector("input").value).toBe("");

    form.querySelector("input").value = "false";
    globalThis.fetch = vi.fn(async () => ok({ exitCode: 2, durationMs: 2, output: "bad" }));
    await app.submitShellCommand({ preventDefault: vi.fn(), currentTarget: form });
    expect(app.querySelectorAll(".tool-card").item(1).dataset.status).toBe("err");

    form.querySelector("input").value = "empty";
    globalThis.fetch = vi.fn(async () => ok({ exitCode: 0, durationMs: 3, output: "" }));
    await app.submitShellCommand({ preventDefault: vi.fn(), currentTarget: form });
    expect([...app.querySelectorAll(".tool-card")].at(-1).textContent).toContain("[no output]");

    form.querySelector("input").value = "explode";
    globalThis.fetch = vi.fn(async () => err("shell failed"));
    await app.submitShellCommand({ preventDefault: vi.fn(), currentTarget: form });
    expect([...app.querySelectorAll(".tool-card")].at(-1).textContent).toContain("shell failed");
    form.querySelector("input").value = "throw-string";
    globalThis.fetch = vi.fn(async () => { throw "shell string failed"; });
    await app.submitShellCommand({ preventDefault: vi.fn(), currentTarget: form });
    expect([...app.querySelectorAll(".tool-card")].at(-1).textContent).toContain("shell string failed");

    app.dataset.activeWorkspaceId = "";
    form.querySelector("input").value = "ignored";
    await app.submitShellCommand({ preventDefault: vi.fn(), currentTarget: form });
  });
});
