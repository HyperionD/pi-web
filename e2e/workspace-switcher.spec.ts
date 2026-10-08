import { expect, test } from "@playwright/test";
import { installSessionStreamFixture } from "./fixtures/session-stream-backend";

for (const width of [1440, 390]) {
  test(`switches workspaces and adds a folder at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 });
    await page.addInitScript(() => localStorage.setItem("pi-web.uiLocale", "zh-CN"));
    await installSessionStreamFixture(page, "run-started");
    const workspaces = [
      { id: "w1", name: "e2e", path: "/e2e", sessionCount: 1,
        sessions: [{ id: "e2e-session", title: "e2e", lastUsed: "now" }] },
      { id: "w2", name: "Second workspace", path: "/second", sessionCount: 0, sessions: [] },
    ];
    await page.route("**/api/workspaces", (route) => route.fulfill({ json: { workspaces } }));
    await page.route("**/api/system/folders?*", (route) => route.fulfill({ json: {
      path: "/new-project", parent: "/", folders: [],
    } }));
    await page.route("**/api/workspaces/open", async (route) => {
      expect(route.request().postDataJSON()).toEqual({ path: "/new-project" });
      const workspace = { id: "w3", name: "New project", path: "/new-project", sessionCount: 0, sessions: [] };
      workspaces.push(workspace);
      await route.fulfill({ json: { workspace } });
    });
    if (width < 640) await page.locator("[data-action='open-drawer']").click();
    await page.locator("[data-action='refresh-workspaces']").click();
    const trigger = page.locator("summary.workspace-trigger");
    await trigger.click();
    const second = page.locator("[data-action='switch-workspace'][data-workspace='w2']");
    await expect(second).toBeVisible();
    const menu = await page.locator(".workspace-menu").boundingBox();
    expect(menu!.x).toBeGreaterThanOrEqual(0);
    expect(menu!.x + menu!.width).toBeLessThanOrEqual(width);
    await second.click();
    await expect(page.locator("[data-active-workspace]")).toHaveText("Second workspace");
    await expect(page.locator("[data-active-workspace-path]")).toHaveText("/second");
    await expect(page.locator("[data-workspace-group='w1']")).toBeHidden();
    await expect(page.locator("[data-workspace-group='w2'] .workspace-shell")).toBeHidden();
    await expect(page.locator("[data-workspace-group='w2'] [data-sessions-empty]")).toBeVisible();
    await expect(page.locator("pi-app")).toHaveAttribute("data-active-session-id", "");
    await expect(page.locator(".sb-head").first()).toContainText("会话");

    await trigger.focus();
    await page.keyboard.press("Enter");
    await expect(second).toBeVisible();
    await page.keyboard.press("Escape");
    await expect(second).toBeHidden();
    await expect(trigger).toBeFocused();

    await trigger.click();
    await page.locator(".workspace-add").click();
    const picker = page.locator("[data-folder-popover]");
    await expect(picker).toBeVisible();
    await expect(picker.locator("strong")).toHaveText("添加工作区");
    await page.locator("[data-action='folder-open-current']").click();
    await expect(picker).toBeHidden();
    await expect(page.locator("[data-active-workspace]")).toHaveText("New project");
    await expect(page.locator("summary.workspace-trigger"))
      .toHaveAttribute("aria-label", "切换工作区，当前工作区：New project");
    await expect(page.locator("[data-active-workspace-path]")).toHaveText("/new-project");
    await trigger.click();
    await expect(page.locator("[data-action='switch-workspace'][data-workspace='w3']"))
      .toHaveAttribute("aria-current", "true");
    await page.screenshot({ path: `test-results/workspace-switcher-${width}.png` });
  });
}

test("removing the active workspace confirms and activates a remaining workspace", async ({ page }) => {
  await page.addInitScript(() => localStorage.setItem("pi-web.uiLocale", "zh-CN"));
  await installSessionStreamFixture(page, "run-started");
  let workspaces = [
    { id: "w1", name: "e2e", path: "/e2e", sessionCount: 1,
      sessions: [{ id: "e2e-session", title: "e2e", lastUsed: "now" }] },
    { id: "w2", name: "Remaining", path: "/remaining", sessionCount: 0, sessions: [] },
  ];
  let deleteCount = 0;
  await page.route("**/api/workspaces", (route) => route.fulfill({ json: { workspaces } }));
  await page.route("**/api/workspaces/w1", async (route) => {
    expect(route.request().method()).toBe("DELETE");
    deleteCount += 1;
    workspaces = workspaces.filter((workspace) => workspace.id !== "w1");
    await route.fulfill({ json: { closed: true } });
  });
  await page.locator("[data-action='refresh-workspaces']").click();
  await page.locator("summary.workspace-trigger").click();
  const remove = page.locator("[data-workspace-options] [data-action='close-workspace'][data-workspace='w1']");
  await expect(remove).toBeVisible();

  let confirmationMessage = "";
  page.once("dialog", async (dialog) => {
    confirmationMessage = dialog.message();
    await dialog.accept();
  });
  await remove.click();
  expect(confirmationMessage).toContain("e2e");
  expect(confirmationMessage).toContain("不会删除磁盘上的文件");

  await expect(page.locator("pi-app")).toHaveAttribute("data-active-workspace-id", "w2");
  await expect(page.locator("[data-active-workspace]")).toHaveText("Remaining");
  expect(deleteCount).toBe(1);
});

test("canceling workspace removal leaves the workspace available", async ({ page }) => {
  await installSessionStreamFixture(page, "run-started");
  const workspaces = [
    { id: "w1", name: "e2e", path: "/e2e", sessionCount: 1,
      sessions: [{ id: "e2e-session", title: "e2e", lastUsed: "now" }] },
  ];
  let deleteCount = 0;
  await page.route("**/api/workspaces", (route) => route.fulfill({ json: { workspaces } }));
  await page.route("**/api/workspaces/w1", async (route) => {
    deleteCount += 1;
    await route.fulfill({ json: { closed: true } });
  });
  await page.locator("[data-action='refresh-workspaces']").click();
  await page.locator("summary.workspace-trigger").click();
  const remove = page.locator("[data-workspace-options] [data-action='close-workspace'][data-workspace='w1']");
  page.once("dialog", (dialog) => { void dialog.dismiss(); });
  await remove.click();
  await expect(remove).toBeVisible();
  expect(deleteCount).toBe(0);
  await expect(page.locator("pi-app")).toHaveAttribute("data-active-workspace-id", "w1");
});
