import { expect, test } from "@playwright/test";
import { installSessionStreamFixture, type LifecycleBoundary } from "./fixtures/session-stream-backend";

const BOUNDARIES: LifecycleBoundary[] = [
  "run-started",
  "assistant-delta",
  "tool-start",
  "tool-output",
  "tool-finish",
  "run-finish",
];

test("shows a durable assistant delta before the run completes", async ({ page }) => {
  const fixture = await installSessionStreamFixture(page, "assistant-delta", { seedTranscript: true });
  await fixture.runUntilBoundary();

  await expect(page.locator(".term-inner .msg[data-kind='pi'] .body")).toHaveText("hello");
});

for (const boundary of BOUNDARIES) {
  test(`refresh at ${boundary} preserves the exact transcript`, async ({ page }) => {
    const fixture = await installSessionStreamFixture(page, boundary);
    await fixture.runUntilBoundary();
    await page.reload();
    await fixture.finishRun();

    await expect(page.getByText("final bash output", { exact: true })).toBeVisible();
    await expect(page.locator(".tool-card")).toHaveCount(1);
    await expect(page.locator(".tool-card")).toHaveAttribute("data-status", "ok");
  });
}

test("reconnects from the applied sequence after a dropped stream", async ({ page }) => {
  const fixture = await installSessionStreamFixture(page, "tool-output");
  await fixture.runUntilBoundary();
  await page.evaluate(() => {
    const sources = (window as unknown as { __mockSources?: Array<{ emitError: () => void }> }).__mockSources;
    sources?.at(-1)?.emitError();
  });
  await fixture.finishRun();
  await expect(page.locator(".tool-card")).toHaveCount(1);
});

test("keeps tool output after the card is virtualized offscreen", async ({ page }) => {
  const fixture = await installSessionStreamFixture(page, "tool-start");
  await fixture.runUntilBoundary();
  for (let index = 0; index < 60; index += 1) {
    await page.evaluate((count) => {
      window.dispatchEvent(new CustomEvent("pi-e2e:append-message", { detail: { index: count } }));
    }, index);
  }
  await fixture.finishRun();
  await expect(page.locator(".tool-card")).toHaveCount(1);
  await expect(page.locator(".tool-card .tc-body")).toContainText("final bash output");
});
