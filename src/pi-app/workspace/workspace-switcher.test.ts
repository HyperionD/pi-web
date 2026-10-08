// @ts-nocheck
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { applyUiLocale } from "../../i18n/client";
import { cleanupPiAppFixture, connectPiApp, installPiAppFixture } from "../test-helper";

const workspaces = [
  { id: "w1", name: "One", path: "/one", sessionCount: 1, sessions: [{ id: "s1", title: "First" }] },
  { id: "w2", name: "Two", path: "/two", sessionCount: 1, sessions: [{ id: "s2", title: "Second" }] },
  { id: "empty", name: "Empty", path: "/empty", sessionCount: 0, sessions: [] },
];

async function setup() {
  const app = await connectPiApp();
  app.querySelector(".sidebar").insertAdjacentHTML("afterbegin", `
    <details data-workspace-switcher>
      <summary class="workspace-trigger" data-i18n-aria-label="switchWorkspace"><strong data-active-workspace></strong>
        <small data-active-workspace-path></small></summary>
      <div data-workspace-options></div>
      <button data-action="route-picker">添加工作区</button>
    </details>`);
  app.dataset.activeWorkspaceId = "w1";
  app.renderWorkspaces(workspaces);
  return app;
}

describe("workspace switcher", () => {
  beforeEach(installPiAppFixture);
  afterEach(cleanupPiAppFixture);

  it("provides a separate removal action for administrator workspace options", async () => {
    document.body.dataset.accountRole = "admin";
    const app = await setup();
    const remove = app.querySelector("[data-workspace-options] [data-action='close-workspace'][data-workspace='w1']");
    expect(remove).not.toBeNull();
    expect(remove.closest(".workspace-option-row")).not.toBeNull();
    expect(remove.closest(".workspace-option")).toBeNull();
    expect(remove.getAttribute("aria-label")).toBe("从列表移除 One");
  });

  it("does not expose workspace removal to regular users", async () => {
    document.body.dataset.accountRole = "user";
    const app = await setup();
    expect(app.querySelector("[data-workspace-options] [data-action='close-workspace']")).toBeNull();
  });

  it("shows only the current workspace sessions and lists every workspace in the switcher", async () => {
    const app = await setup();
    expect(app.querySelectorAll("[data-workspace-options] [data-action='switch-workspace']")).toHaveLength(3);
    expect(app.querySelector("[data-workspace-group='w1']").hidden).toBe(false);
    expect(app.querySelector("[data-workspace-group='w2']").hidden).toBe(true);
    expect(app.querySelector("[data-workspace-group='w1'] .workspace-shell").hidden).toBe(true);
    expect(app.querySelector("[data-active-workspace-path]").textContent).toBe("/one");
  });

  it("switches the session, name, path and selection together without toggling closed", async () => {
    const app = await setup();
    applyUiLocale("en", app);
    expect(app.querySelector("[data-workspace-switcher] summary").getAttribute("aria-label"))
      .toBe("Switch workspace, current workspace: One");
    const switcher = app.querySelector("[data-workspace-switcher]");
    switcher.open = true;
    app.querySelector("[data-action='switch-workspace'][data-workspace='w2']").click();
    expect(app.dataset.activeWorkspaceId).toBe("w2");
    expect(app.dataset.activeSessionId).toBe("s2");
    expect(app.querySelector("[data-active-workspace]").textContent).toBe("Two");
    expect(app.querySelector("[data-active-workspace-path]").textContent).toBe("/two");
    expect(app.querySelector("[data-workspace-switcher] summary").getAttribute("aria-label"))
      .toBe("Switch workspace, current workspace: Two");
    expect(app.querySelector("[data-workspace-group='w1']").hidden).toBe(true);
    expect(app.querySelector("[data-workspace-group='w2'] .sessions").hidden).toBe(false);
    expect(switcher.open).toBe(false);
    expect(app.querySelector("[data-action='switch-workspace'][aria-current='true']").dataset.workspace).toBe("w2");
    app.querySelector("[data-action='switch-workspace'][data-workspace='w2']").click();
    expect(app.querySelector("[data-workspace-group='w2'] .sessions").hidden).toBe(false);
  });

  it("clears the old conversation when switching to an empty workspace and retains new session entry", async () => {
    const app = await setup();
    app.dataset.activeSessionId = "s1";
    app.querySelector("[data-action='switch-workspace'][data-workspace='empty']").click();
    expect(app.dataset.activeWorkspaceId).toBe("empty");
    expect(app.dataset.activeSessionId).toBe("");
    expect(app.querySelector("[data-main='empty']").hidden).toBe(false);
    const group = app.querySelector("[data-workspace-group='empty']");
    expect(group.hidden).toBe(false);
    expect(group.querySelector("[data-action='new-session']").dataset.workspace).toBe("empty");
    expect(group.querySelector("[data-sessions-empty]").hidden).toBe(false);
  });

  it("prefers an active session when the target workspace has multiple sessions", async () => {
    const app = await setup();
    app.renderWorkspaces([
      workspaces[0],
      { id: "w2", name: "Two", path: "/two", sessionCount: 2, sessions: [
        { id: "s2", title: "Older" },
        { id: "s3", title: "Running", live: true },
      ] },
    ]);
    app.querySelector("[data-action='switch-workspace'][data-workspace='w2']").click();
    await vi.waitFor(() => expect(app.dataset.activeSessionId).toBe("s3"));
    expect(app.querySelector("[data-session='s3']").classList.contains("selected")).toBe(true);
  });

  it("clears a previous conversation when opening a newly added empty workspace", async () => {
    const app = await setup();
    app.dataset.activeSessionId = "s1";
    await app.openWorkspace("empty");
    expect(app.dataset.activeWorkspaceId).toBe("empty");
    expect(app.dataset.activeSessionId).toBe("");
    expect(app.querySelector("[data-main='empty']").hidden).toBe(false);
  });

  it("updates the empty state after the last session is removed", async () => {
    const app = await setup();
    app.clearWorkspaceSessionRows("w1");
    expect(app.querySelector("[data-workspace-group='w1'] [data-sessions-empty]").hidden).toBe(false);
  });

  it("closes on Escape or outside click and restores keyboard focus on Escape", async () => {
    const app = await setup();
    const switcher = app.querySelector("[data-workspace-switcher]");
    switcher.open = true;
    switcher.querySelector("button").focus();
    window.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape", bubbles: true }));
    expect(switcher.open).toBe(false);
    expect(document.activeElement).toBe(switcher.querySelector("summary"));
    switcher.open = true;
    document.body.click();
    expect(switcher.open).toBe(false);
  });

  it("rejects unknown workspace IDs and handles an empty workspace list", async () => {
    const app = await setup();
    await app.switchWorkspace("missing");
    expect(app.dataset.activeWorkspaceId).toBe("w1");
    app.dataset.activeWorkspaceId = "";
    app.renderWorkspaces([]);
    expect(app.querySelector("[data-workspace-options]").textContent).toContain("暂无工作区");
    expect(app.querySelector("[data-active-workspace-path]").textContent).toBe("");
    expect(app.querySelector("[data-action='route-picker']")).not.toBeNull();
  });
});
