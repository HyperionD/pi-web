import { expect, test, type Locator, type Page } from "@playwright/test";
import { installSessionStreamFixture } from "./fixtures/session-stream-backend";

test.beforeEach(async ({ browser }, testInfo) => {
  if (testInfo.project.name === "chromium-107") expect(browser.version()).toMatch(/^107\./);
});

async function seedCards(page: Page) {
  await page.addInitScript(() => localStorage.setItem("pi-web.uiLocale", "zh-CN"));
  await installSessionStreamFixture(page, "run-started", { seedTranscript: true });
  await page.locator("pi-app").evaluate((element) => {
    const app = element as HTMLElement & {
      updatePromptMeta: (status: Record<string, unknown>) => void;
      renderMessages: (messages: Array<Record<string, unknown>>) => void;
    };
    app.updatePromptMeta({ model: "GPT-5.5" });
    app.renderMessages([
      { kind: "user", text: "检查输出区域的命令与思考卡片。" },
      { kind: "pi", text: "我会先确认当前工作区状态，再运行针对性检查。" },
      {
        kind: "think", key: "think:1", text: "命令输出会单独显示，避免打断回复阅读。", status: "committed",
      },
      {
        kind: "tool", key: "tool:1", tool: "bash", status: "running",
        args: JSON.stringify({ command: "bun run test -- src/pi-app/messages/messages.test.ts" }),
        body: Array.from({ length: 18 }, (_, index) => `检查 ${index + 1}：等待后续输出…`).join("\n"),
      },
      { kind: "pi", text: "输出保留在独立滚动区域，回复与输入区仍然可以正常阅读。" },
    ]);
  });
  await page.addStyleTag({ content: "astro-dev-toolbar { display: none !important; }" });
}

async function expectLatestOutput(output: Locator) {
  await expect.poll(() => output.evaluate((element) =>
    element.scrollHeight - element.scrollTop - element.clientHeight,
  )).toBeLessThan(2);
}

async function verifyMessageIcons(page: Page) {
  const user = page.locator(".prefix.user");
  const thinking = page.locator(".prefix.think");
  const assistant = page.locator(".prefix.pi").first();
  await expect(user).toHaveText("");
  await expect(user.locator("[data-message-icon='user-round']")).toHaveCount(1);
  await expect(user.locator("[role='img']")).toHaveAccessibleName("您");
  await expect(thinking).toHaveText("");
  await expect(thinking.locator("[data-message-icon='lightbulb']")).toHaveCount(1);
  await expect(thinking.locator("[role='img']")).toHaveAccessibleName("AI");
  await expect(assistant).toHaveText("π");
  await expect(assistant.locator("[role='img']")).toHaveAccessibleName("Pi");
  for (const prefix of [user, thinking, assistant]) {
    await expect(prefix.locator(".message-avatar")).toHaveCSS("width", "24px");
    await expect(prefix.locator(".message-avatar")).toHaveCSS("height", "24px");
    await expect(prefix.locator(".message-avatar")).toHaveCSS("border-radius", "5px");
  }
}

async function thinkingIconGeometry(page: Page) {
  return page.locator(".msg[data-kind='think']").evaluate((element) => {
    const row = element.getBoundingClientRect();
    const icon = element.querySelector(".message-avatar")!.getBoundingClientRect();
    const summary = element.querySelector(".thinking-card > summary")!.getBoundingClientRect();
    return {
      centerOffset: Math.abs(icon.top + icon.height / 2 - summary.top - summary.height / 2),
      topWithinRow: icon.top - row.top,
      overflow: document.documentElement.scrollWidth > window.innerWidth,
    };
  });
}

async function verifyGeometry(page: Page, width: number) {
  const thinking = page.locator(".thinking-card");
  const command = page.locator(".command-card");
  const output = command.locator(".tc-body");
  for (const card of [thinking, command]) {
    await expect(card).toHaveCSS("border-radius", "6px");
    await expect(card).toHaveCSS("border-top-style", "solid");
    await expect(card).toHaveCSS("border-top-color", "rgb(219, 227, 237)");
    await expect(card).toHaveCSS("background-color", "rgb(248, 249, 251)");
    await expect(card).toHaveCSS("box-shadow", "none");
    await expect(card.locator("summary")).toHaveCSS("min-height", width === 1440 ? "36px" : "44px");
  }
  await expect(command.locator("summary")).toHaveCSS("background-color", "rgb(244, 246, 249)");
  await expect(thinking.locator("summary")).toHaveCSS("background-color", "rgba(0, 0, 0, 0)");
  await expect(command.locator(".tc-command")).toHaveCSS("border-radius", "4px");
  await expect(output).toHaveCSS("max-height", "150px");
  await expect(output).toHaveCSS("overflow-y", "auto");
  const outputColor = await output.evaluate((element) => getComputedStyle(element).backgroundColor);
  expect(["rgb(255, 255, 255)", "oklch(1 0 0)"]).toContain(outputColor);
  const geometry = await command.evaluate((element) => {
    const box = element.getBoundingClientRect();
    const thinkingBox = document.querySelector(".thinking-card")!.getBoundingClientRect();
    const output = element.querySelector(".tc-body")!;
    return {
      left: box.left, right: box.right, thinkingLeft: thinkingBox.left,
      outputHeight: output.getBoundingClientRect().height,
      scrollable: output.scrollHeight > output.clientHeight,
      overflow: document.documentElement.scrollWidth > window.innerWidth,
    };
  });
  expect(geometry.left).toBeCloseTo(geometry.thinkingLeft, 1);
  expect(geometry.right).toBeLessThanOrEqual(width);
  expect(geometry.outputHeight).toBeLessThanOrEqual(150);
  expect(geometry.scrollable).toBe(true);
  expect(geometry.overflow).toBe(false);
}

for (const width of [1440, 390]) {
  test(`matches OpenDesign thinking and command cards at ${width}px`, async ({ page }) => {
    const errors: string[] = [];
    page.on("pageerror", (error) => errors.push(error.message));
    await page.setViewportSize({ width, height: 844 });
    await seedCards(page);
    const thinking = page.locator(".thinking-card");
    const command = page.locator(".command-card");
    await verifyMessageIcons(page);
    await expect(page.locator("[data-prompt-meta]")).toContainText("GPT-5.5");
    await expectLatestOutput(command.locator(".tc-body"));
    await expect(thinking).not.toHaveAttribute("open");
    await expect(thinking.locator(".thinking-copy")).toBeHidden();
    await expect(thinking.locator("summary")).toContainText("已完成 · 可展开查看");
    await expect(command).toHaveAttribute("open", "");
    await expect(command.locator("summary")).toContainText("命令执行");
    await expect(command.locator("summary")).not.toContainText("bun run");
    await verifyGeometry(page, width);
    await page.screenshot({ path: test.info().outputPath(`stream-cards-${width}.png`), fullPage: true });
    await thinking.locator("summary").focus();
    await page.keyboard.press("Enter");
    await expect(thinking.locator(".thinking-copy")).toBeVisible();
    await page.keyboard.press("Space");
    await expect(thinking.locator(".thinking-copy")).toBeHidden();
    await command.locator("summary").click();
    await expect(command.locator(".tc-body")).toBeHidden();
    await command.locator("summary").focus();
    await page.keyboard.press("Enter");
    await expect(command.locator(".tc-body")).toBeVisible();
    expect(errors).toEqual([]);
  });
}

for (const width of [1440, 881, 880, 601, 600, 390, 320]) {
  test(`aligns the thinking icon with its summary before and after disclosure at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 844 });
    await seedCards(page);
    const card = page.locator(".thinking-card");
    const closed = await thinkingIconGeometry(page);
    expect(closed.centerOffset).toBeLessThan(1);
    expect(closed.overflow).toBe(false);
    await card.locator("summary").click();
    await expect(card.locator(".thinking-copy")).toBeVisible();
    const open = await thinkingIconGeometry(page);
    expect(open.centerOffset).toBeLessThan(1);
    expect(open.topWithinRow).toBeCloseTo(closed.topWithinRow, 1);
    expect(open.overflow).toBe(false);
    await card.locator("summary").click();
    await expect(card.locator(".thinking-copy")).toBeHidden();
    const collapsed = await thinkingIconGeometry(page);
    expect(collapsed.centerOffset).toBeLessThan(1);
    expect(collapsed.topWithinRow).toBeCloseTo(closed.topWithinRow, 1);
  });
}

test("keeps a command collapsed through durable output and completion events", async ({ page }) => {
  await page.addInitScript(() => localStorage.setItem("pi-web.uiLocale", "zh-CN"));
  const fixture = await installSessionStreamFixture(page, "tool-output", { seedTranscript: true });
  await fixture.runUntilBoundary();
  const card = page.locator(".tool-card");
  await expect(card.locator(".stream-state.running")).toHaveText("运行中");
  await card.locator("summary").click();
  await expect(card).not.toHaveAttribute("open");
  await fixture.finishRun();
  await expect(card).toHaveAttribute("data-status", "ok");
  await expect(card).not.toHaveAttribute("open");
  await expect(card.locator(".tc-body")).toBeHidden();
  await expect(card.locator(".stream-state.ok")).toHaveText("已完成");
  await card.locator("summary").click();
  await expect(card.locator(".tc-body")).toBeVisible();
  await expect(card.locator(".tc-body")).toHaveText("final bash output");
});

test("streams thinking into its content region and retains a reader's collapse at completion", async ({ page }) => {
  await page.addInitScript(() => localStorage.setItem("pi-web.uiLocale", "zh-CN"));
  await installSessionStreamFixture(page, "run-started", { seedTranscript: true });
  await page.locator("pi-app").evaluate((element) => {
    const app = element as HTMLElement & {
      applyDurableEvent: (event: Record<string, unknown>) => void;
      flushDurableTranscriptRender: () => void;
    };
    app.applyDurableEvent({
      seq: "2", sessionId: "e2e-session", entityId: "thinking:run-1:1",
      type: "assistant.segment.started", payload: { kind: "think" },
    });
    app.applyDurableEvent({
      seq: "3", sessionId: "e2e-session", entityId: "thinking:run-1:1",
      type: "assistant.segment.delta", payload: { kind: "think", delta: "第一段思考" },
    });
    app.flushDurableTranscriptRender();
  });
  const card = page.locator(".thinking-card");
  await expect(card).toHaveAttribute("open", "");
  await expect(card.locator(".thinking-state")).toHaveText("思考中");
  await expect(card.locator(".thinking-copy")).toHaveText("第一段思考");
  await expect(card.locator("summary")).not.toContainText("第一段思考");
  await card.locator("summary").click();
  await expect(card).not.toHaveAttribute("open");
  await page.locator("pi-app").evaluate((element) => {
    const app = element as HTMLElement & { applyDurableEvent: (event: Record<string, unknown>) => void };
    app.applyDurableEvent({
      seq: "4", sessionId: "e2e-session", entityId: "thinking:run-1:1",
      type: "assistant.segment.committed", payload: { kind: "think", text: "思考已经完成" },
    });
  });
  await expect(card).not.toHaveAttribute("open");
  await expect(card.locator(".thinking-state")).toHaveText("已完成 · 可展开查看");
  await card.locator("summary").click();
  await expect(card.locator(".thinking-copy")).toHaveText("思考已经完成");
  await expect(card.locator(".thinking-copy")).toBeVisible();
});

test("appends live output at the bottom without disturbing a reader of earlier output", async ({ page }) => {
  await seedCards(page);
  const output = page.locator(".tc-body");
  await page.locator("pi-app").evaluate((element) => {
    const app = element as HTMLElement & { appendToolOutput: (payload: Record<string, unknown>) => void };
    app.appendToolOutput({ tool: "bash", chunk: "\nlatest output" });
  });
  await expect.poll(() => output.evaluate((element) =>
    element.scrollHeight - element.scrollTop - element.clientHeight,
  )).toBeLessThan(2);
  await output.evaluate((element) => { element.scrollTop = 0; });
  await page.locator("pi-app").evaluate((element) => {
    const app = element as HTMLElement & { appendToolOutput: (payload: Record<string, unknown>) => void };
    app.appendToolOutput({ tool: "bash", chunk: "\nanother output" });
  });
  await expect.poll(() => output.evaluate((element) => element.scrollTop)).toBe(0);
});

test("defaults running and finished command output to the latest position", async ({ page }) => {
  await seedCards(page);
  await page.locator("pi-app").evaluate((element) => {
    const app = element as HTMLElement & { renderMessages: (messages: Array<Record<string, unknown>>) => void };
    app.renderMessages(["running", "ok", "err"].map((status) => ({
      kind: "tool", id: status, tool: "bash", status,
      body: Array.from({ length: 20 }, (_, index) => `${status} output ${index}`).join("\n"),
    })));
  });
  for (let index = 0; index < 3; index += 1) await expectLatestOutput(page.locator(".tc-body").nth(index));
});

test("opens a collapsed command at the latest position after hidden output updates", async ({ page }) => {
  await seedCards(page);
  const card = page.locator(".command-card");
  await expectLatestOutput(card.locator(".tc-body"));
  await card.locator("summary").click();
  await expect(card).not.toHaveAttribute("open");
  await page.locator("pi-app").evaluate((element) => {
    const app = element as HTMLElement & { appendToolOutput: (payload: Record<string, unknown>) => void };
    app.appendToolOutput({ tool: "bash", chunk: "\nnew output after collapse\nlatest marker" });
  });
  await card.locator("summary").click();
  await expectLatestOutput(card.locator(".tc-body"));
  await expect(card.locator(".tc-body")).toContainText("latest marker");
});

for (const followLatest of [true, false]) {
  test(`retains output followLatest=${followLatest} when a virtualized card remounts`, async ({ page }) => {
    await seedCards(page);
    const output = page.locator(".tc-body");
    await expectLatestOutput(output);
    if (!followLatest) {
      await output.evaluate((element) => {
        element.scrollTop = 20;
        element.dispatchEvent(new Event("scroll"));
      });
    }
    await page.locator("pi-app").evaluate((element) => {
      const app = element as HTMLElement & {
        transcriptItems: Array<{ message: { kind: string } }>;
        renderVirtualTranscriptItem: (item: unknown) => HTMLElement;
      };
      const item = app.transcriptItems.find((candidate) => candidate.message.kind === "tool");
      const replacement = app.renderVirtualTranscriptItem(item);
      app.querySelector(".tool-card")!.closest(".transcript-item")!.replaceWith(replacement);
    });
    if (followLatest) await expectLatestOutput(output);
    else await expect.poll(() => output.evaluate((element) => element.scrollTop)).toBe(20);
  });
}

test("disables pulse and cursor animation for reduced-motion readers", async ({ page }) => {
  await page.emulateMedia({ reducedMotion: "reduce" });
  await seedCards(page);
  const animation = await page.locator(".tool-card").evaluate((element) => ({
    status: getComputedStyle(element.querySelector(".stream-state")!, "::before").animationName,
    cursor: getComputedStyle(element.querySelector(".tc-body")!, "::after").animationName,
  }));
  expect(animation).toEqual({ status: "none", cursor: "none" });
});
