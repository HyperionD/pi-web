// @ts-nocheck
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanupPiAppFixture, connectPiApp, installPiAppFixture } from "../test-helper";

function ok(body = {}) {
  return { ok: true, status: 200, statusText: "OK", json: async () => body };
}

function err(message = "boom") {
  return { ok: false, status: 500, statusText: "ERR", json: async () => ({ error: message }) };
}

describe("pi-app input actions coverage", () => {
  beforeEach(() => {
    installPiAppFixture();
    globalThis.PI_WEB_API_BASE = "http://backend.test";
  });
  afterEach(cleanupPiAppFixture);

  it("dispatches click actions and list navigation", async () => {
    const app = await connectPiApp();
    const calls = [];
    [
      "route", "refreshWorkspaces", "browseFolder", "loadFolder", "openWorkspacePath", "openFolderPopover", "closeFolderPopover", "toggleTree", "showFileTreePanel", "showGitHistory", "loadMoreGitHistory", "selectGitCommit", "closeGitDetail", "refreshTree",
      "refreshGitHistory", "openFile", "closeFilePreview", "toggleFilePreviewMode", "saveFilePreview", "collapseSidebar",
      "toggleDrawer", "toggleTool", "showFullToolOutput", "toggleWorkspace", "deleteWorkspace", "deleteWorkspaceSessions",
      "newSession", "toggleSessionMenu", "renameSession", "deleteSession", "submitFallbackChoice", "openSettingsModal",
      "closeSettingsModal", "saveSettingsForm", "saveAuthForm", "logoutAuthProvider", "startOAuthLogin", "sendOAuthInput", "pickSession", "openWorkspace", "fillPrompt", "pickSlash",
    ].forEach((name) => { app[name] = (...args) => calls.push([name, ...args]); });
    app.currentFolder = "/cwd";
    app.currentFolderParent = "/";
    const actions = [
      "route-picker", "route-workspace", "refresh-workspaces", "browse-folder", "folder-open", "folder-close", "folder-enter", "folder-up",
      "folder-open-current", "toggle-tree", "show-file-tree", "show-git-history", "load-more-git-history", "select-git-commit", "close-git-detail", "refresh-tree", "tree-root-menu", "open-file", "close-file-preview",
      "toggle-file-preview-mode", "save-file-preview", "collapse-sidebar", "expand-sidebar", "open-drawer", "close-drawer",
      "toggle-tool", "show-full-tool-output", "toggle-workspace", "delete-workspace", "delete-workspace-sessions", "new-session",
      "session-menu-toggle", "rename-session", "delete-session", "open-settings", "close-settings", "save-settings",
      "save-auth-provider", "logout-auth-provider", "start-oauth-login", "send-oauth-input",
    ];
    for (const action of actions) {
      const row = document.createElement("div");
      row.className = "session-row";
      row.dataset.session = "s1";
      const button = document.createElement("button");
      button.dataset.action = action;
      button.dataset.workspace = "w1";
      button.dataset.path = "/p";
      row.append(button);
      app.append(row);
      button.click();
    }
    const recent = document.createElement("button");
    recent.className = "recent-row";
    recent.dataset.workspace = "w1";
    app.append(recent);
    recent.click();
    const seed = document.createElement("button");
    seed.dataset.seed = "seed";
    app.append(seed);
    seed.click();
    const skill = document.createElement("button");
    skill.dataset.skill = "worker";
    app.append(skill);
    skill.click();
    const slash = document.createElement("button");
    slash.dataset.slash = "/model";
    app.append(slash);
    slash.click();

    const insideSpan = document.createElement("span");
    app.append(insideSpan);
    app.handleAppClick({ target: insideSpan });
    app.handleAppClick({ target: document.createElement("span") });
    const outsideButton = document.createElement("button");
    app.handleAppClick({ target: outsideButton });
    const spanAction = document.createElement("span");
    spanAction.dataset.action = "route-picker";
    app.append(spanAction);
    app.handleAppClick({ target: spanAction });
    const spanTreeMenu = document.createElement("span");
    spanTreeMenu.dataset.action = "tree-root-menu";
    app.append(spanTreeMenu);
    app.handleAppClick({ target: spanTreeMenu });
    const positionedTreeMenu = document.createElement("button");
    positionedTreeMenu.dataset.action = "tree-root-menu";
    positionedTreeMenu.getBoundingClientRect = () => ({ left: 12, bottom: 34 });
    app.append(positionedTreeMenu);
    positionedTreeMenu.click();
    const fileTreeNode = document.createElement("div");
    fileTreeNode.dataset.action = "open-file";
    fileTreeNode.dataset.filePath = "src/tree.ts";
    app.append(fileTreeNode);
    app.handleAppClick({ target: fileTreeNode });
    const fallbackButton = document.createElement("button");
    fallbackButton.dataset.action = "fallback-choice";
    fallbackButton.dataset.choiceId = "c";
    fallbackButton.dataset.choiceValue = "v";
    const fallbackPanel = document.createElement("div");
    fallbackPanel.className = "fallback-choice-list";
    fallbackPanel.append(fallbackButton);
    app.append(fallbackPanel);
    fallbackButton.click();
    const tweaks = document.createElement("div");
    tweaks.dataset.tweaks = "";
    app.append(tweaks);
    const closeTweaks = document.createElement("button");
    closeTweaks.dataset.action = "close-tweaks";
    app.append(closeTweaks);
    closeTweaks.click();
    const sessionButton = document.createElement("button");
    sessionButton.dataset.session = "solo";
    app.append(sessionButton);
    sessionButton.click();

    const chip = document.createElement("span");
    chip.className = "attach-chip";
    chip.dataset.attachmentIndex = "0";
    chip.innerHTML = `<button data-remove-attachment></button>`;
    app.attachmentContents = ["x"];
    app.append(chip);
    chip.querySelector("button").click();
    expect(app.attachmentContents[0]).toBe("");
    const badChip = document.createElement("span");
    badChip.className = "attach-chip";
    badChip.dataset.attachmentIndex = "bad";
    badChip.innerHTML = `<button data-remove-attachment></button>`;
    app.append(badChip);
    badChip.querySelector("button").click();
    expect(calls.some(([name]) => name === "newSession")).toBe(true);
    expect(calls).toContainEqual(["openFolderPopover", "workspace"]);
    expect(calls).toContainEqual(["closeFolderPopover"]);

    app.dataset.activeWorkspaceId = "w1";
    app.loadWorkspaceMeta = (...args) => calls.push(["loadWorkspaceMeta", ...args]);
    app.openFilePath = (...args) => calls.push(["openFilePath", ...args]);
    window.dispatchEvent(new CustomEvent("pi-workspace-tree:refresh", { detail: { selectedPath: "src/main.ts" } }));
    window.dispatchEvent(new CustomEvent("pi-workspace-file:open", { detail: { path: "src/main.ts" } }));
    expect(calls).toContainEqual(["loadWorkspaceMeta", "w1", { selectedPath: "src/main.ts" }]);
    expect(calls).toContainEqual(["openFilePath", "src/main.ts"]);
    app.dataset.activeWorkspaceId = "";
    delete app.openFilePath;
    window.dispatchEvent(new Event("pi-workspace-tree:refresh"));
    window.dispatchEvent(new CustomEvent("pi-workspace-file:open", { detail: { path: "" } }));
    window.dispatchEvent(new CustomEvent("pi-workspace-file:open", { detail: { path: "src/skip.ts" } }));

    Element.prototype.scrollIntoView = vi.fn();
    const event = { preventDefault: vi.fn(), key: "ArrowDown" };
    app.navigateList(event, ".slash-item", (item) => calls.push(["nav", item.dataset.slash]));
    app.navigateList({ ...event, key: "ArrowUp" }, ".slash-item", () => {});
    app.navigateList({ ...event, key: "Enter" }, ".slash-item", (item) => calls.push(["enter", item.dataset.slash]));
    app.navigateList({ preventDefault: vi.fn(), key: "Enter" }, ".missing-item", vi.fn());
    expect(event.preventDefault).toHaveBeenCalled();
  });

  it("opens the workspace popover from the sidebar without changing the route", async () => {
    const app = await connectPiApp();
    const trigger = app.querySelector("[data-action='route-picker']");
    const event = { target: trigger, clientX: 90, clientY: 112 };
    app.dataset.route = "workspace";
    app.openFolderPopover = vi.fn();

    app.handleAppClick(event);

    expect(app.dataset.route).toBe("workspace");
    expect(app.querySelector('[data-view="picker"]')).toBeNull();
    expect(app.openFolderPopover).toHaveBeenCalledWith(
      "workspace",
      expect.objectContaining({ clientX: 90, clientY: 112 }),
    );
  });

  it("opens the root workspace popover through a real sidebar click", async () => {
    const app = await connectPiApp();
    const trigger = app.querySelector("[data-action='route-picker']");
    const popover = app.querySelector("[data-folder-popover]");
    app.apiConnected = true;
    globalThis.fetch = vi.fn(async () => ok({ path: "/allowed", parent: "/", folders: [] }));

    trigger.dispatchEvent(new MouseEvent("click", { bubbles: true, clientX: 90, clientY: 112 }));

    await vi.waitFor(() => expect(popover.hidden).toBe(false));
    expect(app.dataset.route).toBe("workspace");
    expect(popover.parentElement).toBe(app);
    expect(popover.closest('[data-view="picker"]')).toBeNull();
    expect(globalThis.fetch).toHaveBeenCalledWith(
      expect.stringContaining("purpose=workspace"),
      expect.any(Object),
    );
  });

  it("prevents duplicate folder confirmations while opening a workspace", async () => {
    const app = await connectPiApp();
    const popover = app.querySelector("[data-folder-popover]");
    const confirm = app.querySelector("[data-action='folder-open-current']");
    app.apiConnected = true;
    app.folderBrowsePurpose = "workspace";
    app.folderListingValid = true;
    app.currentFolder = "/selected";
    popover.hidden = false;
    let resolveOpening;
    app.openWorkspacePath = vi.fn(() => new Promise((resolve) => { resolveOpening = resolve; }));

    confirm.click();
    confirm.click();

    expect(app.openWorkspacePath).toHaveBeenCalledTimes(1);
    expect(confirm.disabled).toBe(true);
    expect(confirm.getAttribute("aria-busy")).toBe("true");
    expect(confirm.textContent).toBe("正在打开…");
    resolveOpening();
    await vi.waitFor(() => expect(popover.hidden).toBe(true));
    expect(app.folderOpeningPending).toBe(false);
  });

  it("clears opening state when opening the current folder throws", async () => {
    const app = await connectPiApp();
    const popover = app.querySelector("[data-folder-popover]");
    const confirm = app.querySelector("[data-action='folder-open-current']");
    app.apiConnected = true;
    app.folderBrowsePurpose = "workspace";
    app.folderListingValid = true;
    app.currentFolder = "/broken";
    popover.hidden = false;
    app.setConnection = vi.fn();
    app.openWorkspacePath = vi.fn(() => { throw new Error("open failed"); });

    confirm.click();

    expect(app.setConnection).toHaveBeenCalledWith("err");
    expect(app.querySelector(".folder-error").textContent).toBe("open failed");
    expect(app.folderOpeningPending).toBe(false);
    expect(confirm.disabled).toBe(true);
  });

  it("ignores stale folder opening results after the picker is reopened", async () => {
    const app = await connectPiApp();
    const trigger = app.querySelector("[data-action='route-picker']");
    const popover = app.querySelector("[data-folder-popover]");
    const confirm = app.querySelector("[data-action='folder-open-current']");
    app.apiConnected = true;
    app.folderBrowsePurpose = "workspace";
    app.folderListingValid = true;
    app.currentFolder = "/first";
    popover.hidden = false;
    trigger.setAttribute("aria-expanded", "true");
    const openings = [];
    app.openWorkspacePath = vi.fn(() => new Promise((resolve, reject) => openings.push({ resolve, reject })));

    confirm.click();
    app.closeFolderPopover();
    popover.hidden = false;
    trigger.setAttribute("aria-expanded", "true");
    app.folderBrowsePurpose = "workspace";
    app.folderListingValid = true;
    app.currentFolder = "/second";
    app.setFolderLoading(false);
    confirm.click();
    expect(openings).toHaveLength(2);

    openings[0].resolve();
    await Promise.resolve();
    await Promise.resolve();
    expect(popover.hidden).toBe(false);
    expect(app.querySelector(".folder-error")).toBeNull();
    expect(confirm.disabled).toBe(true);

    openings[1].reject(new Error("current open failed"));
    await vi.waitFor(() => expect(app.querySelector(".folder-error")?.textContent).toBe("current open failed"));
    expect(confirm.disabled).toBe(true);
    expect(popover.hidden).toBe(false);
  });

  it("clears the popover when opening a forbidden workspace recovers", async () => {
    const app = await connectPiApp();
    const trigger = app.querySelector("[data-action='route-picker']");
    const popover = app.querySelector("[data-folder-popover]");
    const confirm = app.querySelector("[data-action='folder-open-current']");
    app.apiConnected = true;
    app.folderBrowsePurpose = "workspace";
    app.folderListingValid = true;
    app.currentFolder = "/forbidden";
    popover.hidden = false;
    trigger.setAttribute("aria-expanded", "true");
    app.openWorkspacePath = vi.fn(async () => {
      throw Object.assign(new Error("outside allowed roots"), { status: 403 });
    });
    const emptyMain = app.querySelector("[data-main='empty']");

    confirm.click();
    await vi.waitFor(() => expect(app.dataset.route).toBe("workspace"));

    expect(emptyMain.hidden).toBe(false);
    expect(app.querySelector('[data-view="picker"]')).toBeNull();
    expect(popover.hidden).toBe(true);
    expect(trigger.getAttribute("aria-expanded")).toBe("false");
    expect(confirm.disabled).toBe(true);
  });

  it("opens the allowed-root popover and adds the selected folder", async () => {
    const app = await connectPiApp();
    app.apiConnected = true;
    app.dataset.route = "workspace";
    app.appConfigState = {
      config: { version: 1, brand: "x", workspace: { allowedRoots: ["/existing"] } },
      path: "/config",
    };
    const settings = app.querySelector("[data-settings-modal]");
    settings.removeAttribute("hidden");
    app.fillAppConfigForm();
    globalThis.fetch = vi.fn(async () => ok({ path: "/selected", folders: [] }));

    app.querySelector("[data-allowed-root-add]").click();
    await vi.waitFor(() => expect(globalThis.fetch).toHaveBeenCalledWith(
      expect.stringContaining("purpose=allowed-root"),
      expect.any(Object),
    ));
    await vi.waitFor(() => expect(app.currentFolder).toBe("/selected"));

    expect(app.dataset.route).toBe("workspace");
    expect(settings.hidden).toBe(true);
    const popover = app.querySelector("[data-folder-popover]");
    popover.querySelector("[data-action='folder-open-current']").click();

    expect(app.querySelectorAll("[data-allowed-root-row]")).toHaveLength(2);
    expect(app.querySelector("[data-app-setting='allowed-roots']").value).toBe("/existing\n/selected");
    expect(popover.hidden).toBe(true);
    expect(settings.hidden).toBe(false);
    expect(document.activeElement).toBe(app.querySelector("[data-allowed-root-add]"));
  });

  it("restores allowed-root settings focus when Escape closes the popover", async () => {
    const app = await connectPiApp();
    app.apiConnected = true;
    app.dataset.route = "workspace";
    app.appConfigState = { config: { version: 1, brand: "x", workspace: { allowedRoots: [] } } };
    const settings = app.querySelector("[data-settings-modal]");
    const addButton = app.querySelector("[data-allowed-root-add]");
    settings.removeAttribute("hidden");
    app.fillAppConfigForm();
    globalThis.fetch = vi.fn(async () => ok({ path: "/selected", parent: "/", folders: [] }));

    addButton.click();
    await vi.waitFor(() => expect(app.currentFolder).toBe("/selected"));
    expect(app.dataset.route).toBe("workspace");
    expect(settings.hidden).toBe(true);
    app.querySelector("[data-action='folder-close']").focus();
    window.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape" }));

    expect(settings.hidden).toBe(false);
    expect(app.dataset.route).toBe("workspace");
    expect(document.activeElement).toBe(addButton);
  });

  it("submits fallback choices and renders slash command variants", async () => {
    const app = await connectPiApp();
    app.apiConnected = true;
    app.dataset.activeSessionId = "s1";
    const panel = document.createElement("div");
    panel.className = "fallback-choice-list";
    panel.innerHTML = `<button></button><input><div class="choice-custom"><input data-choice-custom-input value="custom"></div>`;
    globalThis.fetch = vi.fn(async () => ok({ accepted: true }));
    await app.submitFallbackChoice("id", "yes", panel);
    expect(panel.classList.contains("answered")).toBe(true);
    expect(panel.querySelector("button").disabled).toBe(true);

    globalThis.fetch = vi.fn(async () => err("prompt failed"));
    app.setConnection = vi.fn();
    await app.submitFallbackChoice("id", "no", panel);
    expect(app.setConnection).toHaveBeenCalledWith("err");

    const detached = document.createElement("pi-app");
    detached.renderSlashCommands([]);
    detached.fillPrompt("ignored");
    await app.submitFallbackChoice("", "", null);
    app.renderSlashCommands([
      { command: "/one", description: "one", scope: "project", source: "prompt" },
      { cmd: "/two", desc: "two", location: "global" },
      { name: "three" },
      { name: undefined },
    ]);
    expect([...app.querySelectorAll(".slash-item")].map((item) => item.dataset.slash)).toEqual(["/one", "/two", "/three"]);
    app.prompt.value = "/tw";
    app.updatePrompt();
    expect(app.querySelector(".slash-pop").hidden).toBe(false);
    app.pickSlash("/one");
    expect(app.prompt.value).toBe("/one ");

    app.querySelector(".slash-list").replaceChildren();
    app.renderSlashCommands([]);
    expect(app.querySelector(".slash-empty").textContent).toBe("no slash commands found");
  });

  it("cancels active sessions", async () => {
    const app = await connectPiApp();
    app.apiConnected = true;
    app.dataset.activeSessionId = "s1";
    globalThis.fetch = vi.fn(async () => ok({ cancelled: true }));
    await app.cancelActiveSession();
    expect(String(globalThis.fetch.mock.calls[0][0])).toContain("/cancel");
    expect(app.isSessionCancellationPending("s1")).toBe(false);
    globalThis.fetch = vi.fn(async () => ok({ cancelled: false }));
    await app.cancelActiveSession();
    expect(app.isSessionCancellationPending("s1")).toBe(false);
    globalThis.fetch = vi.fn(async () => err("cancel failed"));
    app.setConnection = vi.fn();
    await app.cancelActiveSession();
    expect(app.setConnection).toHaveBeenCalledWith("err");
    app.apiConnected = false;
    await app.cancelActiveSession();
  });
});
