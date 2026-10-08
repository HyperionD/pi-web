import { expect, test, type Page } from "@playwright/test";
import { installSessionStreamFixture } from "./fixtures/session-stream-backend";

const files = [
  { type: "dir", name: ".worktrees", path: ".worktrees" },
  { type: "dir", name: "src", path: "src", children: [
    { type: "dir", name: "deep", path: "src/deep" },
    { type: "file", name: "main.ts", path: "src/main.ts" },
  ] },
];

async function installFileTreeFixture(page: Page, width = 1440) {
  const directoryRequests: string[] = [];
  await page.setViewportSize({ width, height: 900 });
  await page.addInitScript(() => localStorage.setItem("pi-web.uiLocale", "zh-CN"));
  await installSessionStreamFixture(page, "run-started");
  await page.route("**/api/workspaces/w1/files", (route) => route.fulfill({ json: { files } }));
  await page.route("**/api/workspaces/w1/git/status", (route) => route.fulfill({ json: {
    branch: "main", dirty: 1, files: { "src/deep/nested.ts": "modified" },
  } }));
  await page.route("**/api/workspaces/w1/files/list?*", async (route) => {
    const path = new URL(route.request().url()).searchParams.get("path") || "";
    directoryRequests.push(path);
    await route.fulfill({ json: { entries: path === "src/deep" ? [
      { type: "file", name: "nested.ts", path: "src/deep/nested.ts" },
      { type: "dir", name: "unloaded", path: "src/deep/unloaded" },
    ] : [] } });
  });
  await page.route("**/api/workspaces/w1/files/read?*", (route) => route.fulfill({ json: {
    path: new URL(route.request().url()).searchParams.get("path"),
    previewKind: "text", content: "const nested = true;", mime: "text/plain",
  } }));
  await page.route("**/api/workspaces/w1/files/search?*", (route) => route.fulfill({ json: {
    matches: ["src/deep/unloaded/needle.txt"],
  } }));
  await page.reload();
  if (width < 640) await page.locator("[data-action='open-drawer']").click();
  await expect(page.locator(".tree-node[data-file-path='.worktrees']")).toBeVisible();
  return { directoryRequests };
}

function row(page: Page, path: string) {
  return page.locator(`.tree-node[data-file-path='${path}']`);
}

for (const width of [1440, 390]) {
  test(`expands empty directories with feedback and keyboard support at ${width}px`, async ({ page }) => {
    const { directoryRequests } = await installFileTreeFixture(page, width);
    const folder = row(page, ".worktrees");
    await folder.click();
    await expect(folder).toHaveAttribute("aria-expanded", "true");
    await expect(folder).toContainText("空文件夹");
    await folder.focus();
    await page.keyboard.press("Enter");
    await expect(folder).toHaveAttribute("aria-expanded", "false");
    await expect(folder).not.toContainText("空文件夹");
    await page.keyboard.press("Space");
    await expect(folder).toHaveAttribute("aria-expanded", "true");
    expect(directoryRequests.filter((path) => path === ".worktrees")).toHaveLength(1);
  });

  test(`keeps lazy directory children after opening a file at ${width}px`, async ({ page }) => {
    const { directoryRequests } = await installFileTreeFixture(page, width);
    await row(page, "src").click();
    await row(page, "src/deep").click();
    const child = row(page, "src/deep/nested.ts");
    await expect(child).toBeVisible();
    await child.click();
    await expect(page.locator(".fp-body")).toContainText("const nested = true;");
    await expect(child).toBeVisible();
    await expect(child).toHaveAttribute("aria-current", "true");
    await expect(child.locator("[aria-label='modified']")).toBeVisible();
    await page.locator("[data-action='close-file-preview']").click();
    await row(page, "src/deep").click();
    await expect(child).toBeHidden();
    await row(page, "src/deep").click();
    await expect(child).toBeVisible();
    expect(directoryRequests.filter((path) => path === "src/deep")).toHaveLength(1);
  });

  test(`shows deep search hits without caching partial directories at ${width}px`, async ({ page }) => {
    await installFileTreeFixture(page, width);
    const search = page.locator(".tree-search input");
    await search.fill("needle");
    const hit = row(page, "src/deep/unloaded/needle.txt");
    await expect(hit).toBeVisible();
    await expect(hit).toHaveAttribute("data-action", "open-file");
    await expect(row(page, "src/deep/unloaded")).toBeVisible();
    await search.fill("");
    await row(page, "src").click();
    await row(page, "src/deep").click();
    await expect(row(page, "src/deep/nested.ts")).toBeVisible();
    await expect(hit).toHaveCount(0);
  });
}

test("refreshing during a directory load drops the stale response and allows reopening", async ({ page }) => {
  await installFileTreeFixture(page);
  let releaseOldRequest!: () => void;
  const pending = new Promise<void>((resolve) => { releaseOldRequest = resolve; });
  let requestCount = 0;
  await page.route("**/api/workspaces/w1/files/list?path=src%2Fdeep", async (route) => {
    const requestIndex = ++requestCount;
    if (requestIndex === 1) await pending;
    await route.fulfill({ json: { entries: [{ type: "file", name: requestIndex === 1 ? "stale.txt" : "current.txt",
      path: requestIndex === 1 ? "src/deep/stale.txt" : "src/deep/current.txt" }] } });
  });
  try {
    await row(page, "src").click();
    await row(page, "src/deep").click();
    await expect(row(page, "src/deep")).toHaveAttribute("aria-busy", "true");
    await page.locator("[data-action='refresh-tree']").click();
    await expect(row(page, "src")).toHaveAttribute("aria-expanded", "false");
    const oldResponse = page.waitForResponse("**/api/workspaces/w1/files/list?path=src%2Fdeep");
    releaseOldRequest();
    await oldResponse;
    await row(page, "src").click();
    await expect(row(page, "src/deep")).toHaveAttribute("aria-expanded", "false");
    await row(page, "src/deep").click();
    await expect(row(page, "src/deep/current.txt")).toBeVisible();
    await expect(row(page, "src/deep/stale.txt")).toHaveCount(0);
  } finally {
    releaseOldRequest();
  }
});
