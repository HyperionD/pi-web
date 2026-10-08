import { expect, test, type Locator, type Page } from "@playwright/test";
import { installSessionStreamFixture } from "./fixtures/session-stream-backend";

const CANVAS_COLOR = "rgb(239, 241, 244)";

async function expectCanvas(region: Locator) {
  const color = await region.evaluate((element) => {
    let node: Element | null = element;
    while (node) {
      const color = getComputedStyle(node).backgroundColor;
      if (color !== "rgba(0, 0, 0, 0)" && color !== "transparent") return color;
      node = node.parentElement;
    }
    return "transparent";
  });
  expect(color).toBe(CANVAS_COLOR);
}

async function expectWhite(region: Locator) {
  const color = await region.evaluate((element) => getComputedStyle(element).backgroundColor);
  expect(["rgb(255, 255, 255)", "oklch(1 0 0)"]).toContain(color);
}

async function installAccountFixture(page: Page) {
  const responses: Record<string, unknown> = {
    "/api/account/status": { configured: true },
    "/api/account/me": { user: {
      id: "background-admin", account: "admin", username: "Background test", role: "admin",
      department: "", disabled: false, workspaces: [],
    } },
    "/api/admin/workspaces": { workspaces: [] },
    "/api/admin/users": { users: [], total: 0, page: 1, pageSize: 10 },
  };
  await page.route("**/api/**", (route) => {
    const response = responses[new URL(route.request().url()).pathname];
    return response ? route.fulfill({ json: response })
      : route.fulfill({ status: 404, json: { error: "Unexpected account request" } });
  });
}

test.beforeEach(async ({ browser }, testInfo) => {
  if (testInfo.project.name === "chromium-107") expect(browser.version()).toMatch(/^107\./);
});

for (const width of [1440, 390]) {
  test(`unifies workspace canvas without recoloring session and output cards at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 844 });
    await installSessionStreamFixture(page, "run-started", { seedTranscript: true });
    if (width === 390) await page.locator("[data-action='open-drawer']").click();
    for (const selector of ["html", "body", ".app", ".app-body", ".sidebar-wrap", ".sidebar",
      ".sidebar .tree", ".sidebar .sb-section"]) {
      await expectCanvas(page.locator(selector));
    }
    for (const selector of [".main-content", ".main[data-main='session']",
      ".term", ".prompt-region", ".prompt-bar"]) {
      await expectWhite(page.locator(selector));
    }
    await page.locator("pi-app").evaluate((element) => {
      (element as HTMLElement & { showEmptyMain: () => void }).showEmptyMain();
    });
    await expect(page.locator(".empty-shell")).toBeVisible();
    await expectWhite(page.locator(".empty-shell"));
    await page.locator("pi-app").evaluate((element) => {
      (element as HTMLElement & { showSessionMain: () => void }).showSessionMain();
    });
    if (width === 390) await page.locator("button[data-action='close-drawer']").click();
    await expect(page.locator(".app-body")).not.toHaveClass(/drawer-open/);
    await page.addStyleTag({ content: "astro-dev-toolbar { display: none !important; }" });
    await page.screenshot({
      path: test.info().outputPath(`system-background-${width}.png`), fullPage: true, animations: "disabled",
    });
  });

  for (const path of ["/login", "/users"]) {
    test(`uses the same system canvas on ${path} at ${width}px`, async ({ page }) => {
      await page.setViewportSize({ width, height: 844 });
      await installAccountFixture(page);
      await page.goto(path);
      await expect(page.locator(".account-card").first()).toBeVisible();
      await expectCanvas(page.locator("html"));
      await expectCanvas(page.locator("body"));
      await expectWhite(page.locator(".account-card").first());
      await expect(page.locator("[data-account-error]").last()).toBeEmpty();
    });
  }
}
