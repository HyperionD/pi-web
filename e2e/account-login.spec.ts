import { expect, test } from "@playwright/test";

test("routes login API requests through the Astro backend proxy", async ({ page }) => {
  const accountStatusRequest = page.waitForRequest((request) => request.url().endsWith("/api/account/status"));
  await page.goto("/login");

  const request = await accountStatusRequest;
  expect(new URL(request.url()).origin).toBe(new URL(page.url()).origin);
  await expect(page.locator("[data-login-title]")).toHaveText("设置管理员账号");
  await expect(page.locator("[data-confirmation]")).toBeVisible();
});
