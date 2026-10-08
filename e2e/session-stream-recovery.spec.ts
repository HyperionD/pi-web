import { expect, test, type Page } from "@playwright/test";
import { installSessionStreamFixture, type LifecycleBoundary } from "./fixtures/session-stream-backend";

const BOUNDARIES: LifecycleBoundary[] = [
  "run-started",
  "assistant-delta",
  "tool-start",
  "tool-output",
  "tool-finish",
  "run-finish",
];

async function transcriptLayout(page: Page) {
  return page.evaluate(() => {
    const term = document.querySelector(".term")!;
    const navigation = document.querySelector(".transcript-navigation")!;
    const navigationBox = navigation.getBoundingClientRect();
    return {
      navigationTop: navigationBox.top, navigationBottom: navigationBox.bottom,
      termTop: term.getBoundingClientRect().top, scrollTop: term.scrollTop,
      contentTop: document.querySelector(".term-inner")!.getBoundingClientRect().top,
    };
  });
}

test("shows a durable assistant delta before the run completes", async ({ page }) => {
  const fixture = await installSessionStreamFixture(page, "assistant-delta", { seedTranscript: true });
  await fixture.runUntilBoundary();

  await expect(page.locator(".term-inner .msg[data-kind='pi'] .body")).toHaveText("hello");
  await expect(page.getByText("waiting for response…", { exact: true })).toHaveCount(0);
});

test("localizes workspace controls using the saved UI language", async ({ page }) => {
  await page.addInitScript(() => localStorage.setItem("pi-web.uiLocale", "en"));
  await installSessionStreamFixture(page, "run-started");

  await expect(page.locator(".prompt-textarea")).toHaveAttribute("placeholder", "Type your prompt");
  await expect(page.locator(".sb-footer-actions")).toHaveAttribute("aria-label", "Management tools");
  await expect(page.locator(".sb-settings")).toHaveAttribute("title", "Settings");
  await expect(page.locator(".ws-count-label").first()).toHaveText("sessions");
});

test("keeps the transcript titleless without desktop header space or losing mobile navigation", async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  const fixture = await installSessionStreamFixture(page, "run-started");
  await fixture.runUntilBoundary();
  await expect(page.locator(".main-content h1")).toHaveCount(0);
  await page.locator("pi-app").evaluate((element) => {
    const app = element as HTMLElement & {
      renderMessages: (messages: Array<{ kind: string; text: string }>) => void;
    };
    app.renderMessages(Array.from({ length: 40 }, (_, index) => ({
      kind: "pi",
      text: `Conversation entry ${index + 1}\n\nTranscript detail for the scroll test.`,
    })));
  });

  for (const width of [1440, 390]) {
    await page.setViewportSize({ width, height: 844 });
    const term = page.locator(".term");
    const navigation = page.locator(".transcript-navigation");
    await term.hover();
    await page.mouse.wheel(0, -100_000);
    await expect.poll(() => term.evaluate((element) => element.scrollTop)).toBe(0);
    await expect(navigation).toHaveCount(1);
    const initialLayout = await transcriptLayout(page);
    expect(initialLayout.contentTop).toBeCloseTo(initialLayout.termTop + (width === 1440 ? 24 : 16), 1);
    if (width === 1440) await expect(navigation).toBeHidden();
    else await expect(navigation).toBeVisible();
    await page.mouse.wheel(0, 400);
    await expect.poll(() => term.evaluate((element) => element.scrollTop)).toBeGreaterThan(0);
    if (width === 390) {
      const scrolledLayout = await transcriptLayout(page);
      expect(scrolledLayout.navigationTop).toBeCloseTo(initialLayout.navigationTop, 1);
      expect(scrolledLayout.navigationBottom).toBeLessThanOrEqual(scrolledLayout.termTop);
      await navigation.locator("[data-action='open-drawer']").click();
      await expect(page.locator(".app-body")).toHaveClass(/drawer-open/);
    }
  }
});

test("retains sidebar and notification session names without a visible content heading", async ({ page }) => {
  await installSessionStreamFixture(page, "run-started", { seedTranscript: true });
  await expect(page.locator("[data-active-session-title]")).toBeHidden();
  await expect(page.locator("[data-main='session']")).toHaveAccessibleName("e2e");
  await page.locator("pi-app").evaluate((element) => {
    const app = element as HTMLElement & {
      updateSessionTitle: (session: { id: string; title: string }) => void;
      showToast: (type: string, detail: string) => unknown;
    };
    app.updateSessionTitle({ id: "e2e-session", title: "Renamed conversation" });
    app.showToast("success", "Name preserved");
  });
  await expect(page.locator(".sidebar .session-row[data-session='e2e-session'] .title"))
    .toHaveText("Renamed conversation");
  await expect(page.locator(".session-toast.success .toast-session")).toHaveText("Renamed conversation");
  await expect(page.locator("[data-main='session']")).toHaveAccessibleName("Renamed conversation");
  await expect(page.locator(".main-content h1")).toHaveCount(0);
});

for (const width of [1440, 390]) {
  test(`matches the sidebar base to the gray main canvas at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 844 });
    await page.addInitScript(() => localStorage.setItem("pi-web.uiLocale", "zh-CN"));
    await installSessionStreamFixture(page, "run-started", { seedTranscript: true });
    if (width === 390) {
      await page.locator("[data-action='open-drawer']").click();
      await expect(page.locator(".sidebar")).toHaveCSS("left", "0px");
    }
    const canvasColor = await page.locator(".app").evaluate((element) =>
      getComputedStyle(element).backgroundColor,
    );
    expect(["rgb(255, 255, 255)", "oklch(1 0 0)"]).not.toContain(canvasColor);
    await expect(page.locator(".sidebar-wrap")).toHaveCSS("background-color", canvasColor);
    await expect(page.locator(".sidebar")).toHaveCSS("background-color", canvasColor);
    await page.addStyleTag({ content: "astro-dev-toolbar { display: none !important; }" });
    await page.screenshot({ path: test.info().outputPath(`gray-sidebar-${width}.png`), fullPage: true });
  });

  test(`keeps transcript and composer in one OpenDesign neumorphic card at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 844 });
    await installSessionStreamFixture(page, "run-started", { seedTranscript: true });
    const card = page.locator(".main-content");
    await expect(card).toHaveCount(1);
    await expect(card.locator("[data-main='session']")).toBeVisible();
    await expect(card.locator(".prompt-textarea")).toBeVisible();
    await expect(card).toHaveCSS("border-radius", width === 1440 ? "12px" : "10px");
    const layout = await card.evaluate((element) => {
      const style = getComputedStyle(element);
      const card = element.getBoundingClientRect();
      const main = element.querySelector("[data-main='session']")!;
      const transcript = main.getBoundingClientRect();
      const composer = element.querySelector(".prompt-region")!.getBoundingClientRect();
      return {
        display: style.display, overflow: style.overflow, shadow: style.boxShadow,
        border: style.borderTopColor, borderWidth: style.borderTopWidth,
        left: card.left, right: card.right, top: card.top, bottom: card.bottom,
        transcriptBottom: transcript.bottom, composerTop: composer.top,
        transcriptRadius: getComputedStyle(main).borderRadius,
        composerRadius: getComputedStyle(element.querySelector(".prompt-region")!).borderRadius,
        dialogsInside: element.querySelectorAll(
          "[data-file-preview], [data-git-detail-panel], [data-user-management-dialog]",
        ).length,
      };
    });
    expect(layout).toMatchObject({
      display: "grid", overflow: "hidden", border: "rgba(255, 255, 255, 0.72)",
      borderWidth: "1px", transcriptRadius: "0px", composerRadius: "0px", dialogsInside: 0,
    });
    expect(layout.shadow).toContain("9px 9px 22px");
    expect(layout.shadow).toContain("-9px -9px 22px");
    expect(layout.shadow).toContain("inset");
    expect(layout.left).toBe(width === 1440 ? 296 : 8);
    expect(layout.right).toBe(width - (width === 1440 ? 10 : 8));
    expect(layout.transcriptBottom).toBeCloseTo(layout.composerTop, 1);
    expect(layout.top).toBeGreaterThanOrEqual(8);
    expect(layout.bottom).toBeLessThanOrEqual(844 - 8);
    if (width === 390) {
      await expect(card.locator("[data-action='open-drawer']")).toHaveCSS("height", "44px");
      for (const selector of [".attach-btn", ".send-btn", ".stop-btn"]) {
        await expect(card.locator(selector)).toHaveCSS("height", "44px");
      }
      await expect(card.locator(".attach-btn")).toHaveCSS("width", "44px");
    }
    await page.addStyleTag({ content: "astro-dev-toolbar { display: none !important; }" });
    await page.screenshot({ path: test.info().outputPath(`neumorphic-card-${width}.png`), fullPage: true });
  });
}

test("keeps mobile navigation above optional scanline decoration", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await installSessionStreamFixture(page, "run-started", { seedTranscript: true });
  await page.locator(".term").evaluate((element) => element.classList.add("scanlines"));
  const navigation = page.locator(".transcript-navigation");
  const layers = await navigation.evaluate((element) => {
    const controls = getComputedStyle(element);
    const term = element.closest(".main")!.querySelector(".term")!;
    const decoration = getComputedStyle(term, "::before");
    return { position: controls.position, navigation: controls.zIndex, decoration: decoration.zIndex };
  });
  expect(layers.position).toBe("relative");
  expect(Number(layers.navigation)).toBeGreaterThan(Number(layers.decoration));
  await navigation.locator("[data-action='open-drawer']").click();
  await expect(page.locator(".app-body")).toHaveClass(/drawer-open/);
});

test("keeps the main card static on hover and press", async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  await installSessionStreamFixture(page, "run-started", { seedTranscript: true });
  const card = page.locator(".main-content");
  await expect(card).toHaveCount(1);
  await page.mouse.move(0, 0);
  const shadow = await card.evaluate((element) => getComputedStyle(element).boxShadow);
  await expect(card).toHaveCSS("transition-duration", "0s");
  await card.hover();
  await expect(card).toHaveCSS("box-shadow", shadow);
  await page.mouse.down();
  await expect(card).toHaveCSS("transform", "none");
  await expect(card).toHaveCSS("box-shadow", shadow);
  await page.mouse.up();
});

test("uses OpenDesign geometry for notification stacks and cards", async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  const fixture = await installSessionStreamFixture(page, "run-started");
  await fixture.runUntilBoundary();
  await page.locator("pi-app").evaluate((element) => {
    const app = element as HTMLElement & {
      showToast: (...args: unknown[]) => unknown;
      showSystemToast: (...args: unknown[]) => unknown;
    };
    const context = {
      workspaceName: "e2e",
      sessionName: "conversation",
      sessionId: "e2e-session",
      prompt: "Summarize the result",
    };
    app.showToast("success", "Completed", context);
    app.showToast("choice", "Feedback needed", context);
    app.showSystemToast("warning", "Warning", "Check the configuration", "warning:test");
    app.showSystemToast("error", "Failure", "The request failed", "error:test");
    app.showSystemToast("connection", "Connection lost", "Reconnecting", "connection:test");
  });

  await expect(page.locator(".notyf__toast.session-toast")).toHaveCount(5);
  const stackStyle = await page.locator(".notyf").evaluate((element) => {
    const style = getComputedStyle(element);
    return { top: style.top, right: style.right, width: style.width, gap: style.rowGap };
  });
  expect(stackStyle).toEqual({ top: "16px", right: "16px", width: "360px", gap: "8px" });

  const cardStyle = await page.locator(".session-toast.success").evaluate((element) => {
    const style = getComputedStyle(element);
    const copy = getComputedStyle(element.querySelector(".toast-copy")!);
    return {
      display: style.display,
      minHeight: style.minHeight,
      padding: style.padding,
      borderWidth: style.borderTopWidth,
      borderRadius: style.borderRadius,
      background: style.backgroundColor,
      fontSize: copy.fontSize,
    };
  });
  expect(cardStyle).toEqual({
    display: "flex",
    minHeight: "54px",
    padding: "11px 10px 10px 12px",
    borderWidth: "1px",
    borderRadius: "7px",
    background: "rgb(255, 255, 255)",
    fontSize: "11px",
  });
  await expect(page.locator(".session-toast .toast-icon svg")).toHaveCount(5);
  const iconStyles = await page.locator(".session-toast").evaluateAll((elements) => elements.map((element) => {
    const type = ["success", "choice", "warning", "error", "connection"]
      .find((candidate) => element.classList.contains(candidate));
    const icon = element.querySelector(".toast-icon");
    return { type, color: getComputedStyle(icon!).color, hasPath: !!icon?.querySelector("svg path, svg circle") };
  }));
  expect(iconStyles).toEqual([
    { type: "success", color: "rgb(39, 116, 83)", hasPath: true },
    { type: "choice", color: "rgb(37, 99, 235)", hasPath: true },
    { type: "warning", color: "rgb(152, 91, 30)", hasPath: true },
    { type: "error", color: "rgb(171, 55, 55)", hasPath: true },
    { type: "connection", color: "rgb(100, 116, 139)", hasPath: true },
  ]);
  await expect(page.locator(".session-toast .notyf__dismiss-btn").first()).toHaveAttribute(
    "aria-label",
    "Dismiss notification",
  );
  const closeSize = await page.locator(".session-toast.success .notyf__dismiss-btn").evaluate((element) => {
    const box = element.getBoundingClientRect();
    return { width: Math.round(box.width), height: Math.round(box.height) };
  });
  expect(closeSize).toEqual({ width: 44, height: 44 });
  await expect(page.locator(".toast-dismiss-all")).toBeVisible();
  await page.locator(".toast-dismiss-all").click();
  await expect(page.locator(".notyf__toast")).toHaveCount(0);

  await page.setViewportSize({ width: 390, height: 844 });
  const mobileStack = await page.locator(".notyf").evaluate((element) => {
    const box = element.getBoundingClientRect();
    return { top: Math.round(box.top), width: Math.round(box.width), left: Math.round(box.left) };
  });
  expect(mobileStack).toEqual({ top: 104, width: 358, left: 16 });
});

test("matches the OpenDesign desktop workspace geometry", async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.addInitScript(() => localStorage.setItem("pi-web.uiLocale", "zh-CN"));
  const fixture = await installSessionStreamFixture(page, "run-started");
  await fixture.runUntilBoundary();
  await page.route("**/api/workspaces", (route) => route.fulfill({ json: { workspaces: [{
    id: "w1", name: "e2e", path: "/e2e", sessionCount: 2, sessions: [
      {
        id: "e2e-session",
        title: "A very long active session title that should yield space to its status label",
        lastUsed: "now",
        live: true,
        active: true,
      },
      { id: "other-session", title: "Other session", lastUsed: "then" },
    ],
  }] } }));
  await page.locator("[data-action='refresh-workspaces']").click();

  await expect(page.locator(".topbar")).toHaveCount(0);
  await expect(page.locator(".side-head")).toBeVisible();
  await expect(page.locator(".side-head .cursor-block")).toHaveCount(0);
  await expect(page.locator(".workspace-trigger")).toBeVisible();
  await expect(page.locator(".sb-head").first()).toContainText("会话");
  const workspaceSectionStyle = await page.locator(".sb-section").evaluate((element) => {
    const style = getComputedStyle(element);
    return { radius: style.borderRadius, shadow: style.boxShadow };
  });
  expect(workspaceSectionStyle).toEqual({ radius: "6px", shadow: "none" });
  const activeWorkspaceStyle = await page.locator(".workspace-group .ws-row.open").evaluate((element) => {
    const style = getComputedStyle(element);
    return { radius: style.borderRadius, borderWidth: style.borderTopWidth, padding: style.padding };
  });
  expect(activeWorkspaceStyle).toEqual({ radius: "5px", borderWidth: "1px", padding: "3px 7px" });
  const sessionStyle = await page
    .locator(".workspace-group .sessions .session-row:not(.new-session-row)")
    .first()
    .evaluate((element) => {
      const style = getComputedStyle(element);
      const main = getComputedStyle(element.querySelector(".session-main")!);
      const surfaceProbe = document.createElement("span");
      surfaceProbe.style.cssText = "display:none;background-color:var(--picture-blue-soft)";
      element.closest(".app")!.append(surfaceProbe);
      const selectedSurface = getComputedStyle(surfaceProbe).backgroundColor;
      surfaceProbe.remove();
      return {
        display: style.display,
        radius: style.borderRadius,
        minHeight: style.minHeight,
        borderWidth: style.borderTopWidth,
        borderColor: style.borderTopColor,
        background: style.backgroundColor,
        selectedSurface,
        contentIndent: main.paddingLeft,
      };
    });
  expect(sessionStyle).toMatchObject({
    display: "flex",
    radius: "5px",
    minHeight: "34px",
    borderWidth: "1px",
    borderColor: "rgb(203, 216, 236)",
    contentIndent: "19px",
  });
  expect(sessionStyle.background).toBe(sessionStyle.selectedSurface);
  const selectedTitleColor = await page.locator(".workspace-group .session-row.selected .title").evaluate((element) => {
    const app = element.closest(".app")!;
    const probe = document.createElement("span");
    probe.style.cssText = "display:none;color:var(--picture-text)";
    app.append(probe);
    const foreground = getComputedStyle(probe).color;
    probe.remove();
    return { color: getComputedStyle(element).color, foreground };
  });
  expect(selectedTitleColor.color).toBe(selectedTitleColor.foreground);
  const sessionListStyle = await page.locator(".workspace-group .sessions").evaluate((element) => {
    const style = getComputedStyle(element);
    return {
      display: style.display,
      gap: style.rowGap,
      marginLeft: style.marginLeft,
      marginTop: style.marginTop,
      borderLeft: style.borderLeftWidth,
    };
  });
  expect(sessionListStyle).toEqual({
    display: "flex",
    gap: "2px",
    marginLeft: "0px",
    marginTop: "3px",
    borderLeft: "0px",
  });
  const newSessionGap = await page.locator(".workspace-group .new-session-row").evaluate((element) => {
    const sessions = element.parentElement!;
    const rows = [...sessions.querySelectorAll(".session-row:not(.new-session-row)")];
    const lastSession = rows.at(-1)!;
    return Math.round(element.getBoundingClientRect().top - lastSession.getBoundingClientRect().bottom);
  });
  expect(newSessionGap).toBe(3);
  const unselectedTitleColor = await page.locator("[data-session='other-session'] .title").evaluate((element) => {
    const app = element.closest(".app")!;
    const probe = document.createElement("span");
    probe.style.cssText = "display:none;color:var(--picture-muted)";
    app.append(probe);
    const muted = getComputedStyle(probe).color;
    probe.remove();
    return { color: getComputedStyle(element).color, muted };
  });
  expect(unselectedTitleColor.color).toBe(unselectedTitleColor.muted);
  const runningLabelStyle = await page
    .locator(".workspace-group .session-row.active .meta.live")
    .evaluate((element) => {
      const app = element.closest(".app")!;
      const probe = document.createElement("span");
      probe.style.cssText = "display:none;color:var(--picture-green)";
      app.append(probe);
      const success = getComputedStyle(probe).color;
      probe.remove();
      const style = getComputedStyle(element);
      return {
        visible: !element.hasAttribute("hidden"),
        fontSize: style.fontSize,
        color: style.color,
        success,
      };
    });
  expect(runningLabelStyle).toMatchObject({ visible: true, fontSize: "10px" });
  expect(runningLabelStyle.color).toBe(runningLabelStyle.success);
  const waitingLabelLineCount = await page
    .locator(".workspace-group .session-row.active .meta.live")
    .evaluate((element) => {
      const range = document.createRange();
      range.selectNodeContents(element);
      return range.getClientRects().length;
    });
  expect(waitingLabelLineCount).toBe(1);
  const newSessionIndent = await page.locator(".workspace-group .new-session-row").evaluate((element) =>
    getComputedStyle(element).marginLeft,
  );
  expect(newSessionIndent).toBe("16px");
  await expect(page.locator(".workspace-group .new-session-row")).toContainText("新建会话");
  await expect(page.locator(".workspace-group .session-row.selected .title")).toBeVisible();
  await expect(page.locator("[data-active-session-title]")).toBeHidden();
  await expect(page.locator(".sb-footer-status [data-account-nav]")).toBeVisible();

  const sidebarWidth = await page.locator(".sidebar-wrap").evaluate((element) =>
    Math.round(element.getBoundingClientRect().width),
  );
  expect(sidebarWidth).toBe(286);

  const promptBox = await page.locator(".prompt-textarea").boundingBox();
  const attachmentBox = await page.locator(".attach-btn").boundingBox();
  expect(promptBox?.width).toBeGreaterThan(760);
  expect(promptBox!.y).toBeLessThan(attachmentBox!.y);

  const borderStyle = await page.locator(".prompt-bar").evaluate((element) => {
    const style = getComputedStyle(element, "::after");
    return {
      width: style.borderTopWidth,
      radius: style.borderTopLeftRadius,
      pointerEvents: style.pointerEvents,
    };
  });
  expect(borderStyle).toEqual({ width: "1px", radius: "8px", pointerEvents: "none" });
  await page.locator(".prompt-textarea").focus();
  const focusedBorderColor = await page.locator(".prompt-bar").evaluate((element) =>
    getComputedStyle(element, "::after").borderTopColor,
  );
  expect(focusedBorderColor).toBe("rgb(147, 197, 253)");

  const attachmentGuidance = page.locator(".attachment-guidance-disclosure");
  await attachmentGuidance.locator("summary").click();
  await expect(attachmentGuidance.locator(".attachment-guidance")).toBeVisible();

  await page.setViewportSize({ width: 390, height: 844 });
  const mobileSessionStyle = await page
    .locator(".workspace-group .sessions .session-row:not(.new-session-row)")
    .first()
    .evaluate((element) => ({
      rowHeight: getComputedStyle(element).minHeight,
      mainHeight: getComputedStyle(element.querySelector(".session-main")!).minHeight,
    }));
  expect(mobileSessionStyle).toEqual({ rowHeight: "44px", mainHeight: "42px" });
  await expect(page.locator(".workspace-group .new-session-row")).toHaveCSS("min-height", "44px");
});

test("uses matching OpenDesign action menus for sessions and files", async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  const fixture = await installSessionStreamFixture(page, "run-started");
  await fixture.runUntilBoundary();

  const sessionRow = page.locator(".session-row:has(.session-menu-button)").first();
  await sessionRow.hover();
  await sessionRow.locator(".session-menu-button").click();
  const menus = [
    { locator: page.locator(".session-menu:not([hidden])"), title: "会话操作" },
    { locator: page.locator(".tree-action-menu"), title: "工作区文件" },
  ];

  for (const [index, { locator, title }] of menus.entries()) {
    if (index === 1) {
      await page.locator(".tree-head [data-action='tree-root-menu']").click();
    }
    await expect(locator).toBeVisible();
    await expect(locator.locator("h2")).toHaveText(title);
    const style = await locator.evaluate((element) => {
      const menu = getComputedStyle(element);
      const item = getComputedStyle(element.querySelector("[role='menuitem']")!);
      return {
        width: Math.round(element.getBoundingClientRect().width),
        border: menu.borderTopColor,
        radius: menu.borderRadius,
        shadow: menu.boxShadow,
        itemHeight: item.minHeight,
        itemBackground: item.backgroundColor,
        itemFontSize: item.fontSize,
      };
    });
    expect(style.width).toBe(232);
    expect(style.border).toBe("rgb(184, 198, 216)");
    expect(style.radius).toBe("6px");
    expect(style.shadow).toContain("10px 28px");
    expect(style.itemHeight).toBe("34px");
    expect(style.itemBackground).toBe("rgba(0, 0, 0, 0)");
    expect(style.itemFontSize).toBe("11px");
    if (index === 0) {
      await expect(locator.locator(".danger")).toHaveCSS("color", "rgb(171, 55, 55)");
    } else {
      await expect(locator.locator(".danger")).toBeDisabled();
      await expect(locator.locator(".danger")).not.toHaveCSS("color", "rgb(171, 55, 55)");
    }
  }
  await page.keyboard.press("Escape");
  await expect(page.locator(".tree-action-menu")).toHaveCount(0);
});

test("shows the same action surface for a file target", async ({ page }) => {
  const fixture = await installSessionStreamFixture(page, "run-started");
  await fixture.runUntilBoundary();
  await page.evaluate(() => {
    window.dispatchEvent(new CustomEvent("pi-workspace-tree:update", {
      detail: { statusMap: {}, files: [{
        type: "dir", name: "src", path: "src",
        children: [{ type: "file", name: "main.ts", path: "src/main.ts" }],
      }] },
    }));
  });
  await page.locator(".tree-node.dir").click();
  const row = page.locator(".tree-node.file").first();
  await row.hover();
  await row.locator(".tree-row-menu").click();
  const menu = page.locator(".tree-action-menu");
  await expect(menu.locator("h2")).toHaveText("文件操作");
  await expect(menu.locator("[role='menuitem']", { hasText: "下载" })).toBeEnabled();
  await expect(menu.locator(".danger")).toHaveCSS("color", "rgb(171, 55, 55)");
  expect(Math.round((await menu.boundingBox())!.width)).toBe(232);
  await page.keyboard.press("Escape");
  await expect(menu).toHaveCount(0);
});

test("keeps the file action menu inside a narrow viewport", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 480 });
  const fixture = await installSessionStreamFixture(page, "run-started");
  await fixture.runUntilBoundary();
  await page.addStyleTag({ content: "astro-dev-toolbar { display: none !important; }" });
  await page.locator(".notyf__dismiss-btn").evaluateAll((buttons) => {
    buttons.forEach((button) => (button as HTMLButtonElement).click());
  });
  await page.locator("[data-action='open-drawer']").click();
  await page.locator(".tree-head [data-action='tree-root-menu']").click();
  const menuBox = await page.locator(".tree-action-menu").boundingBox();
  expect(menuBox).not.toBeNull();
  expect(menuBox!.x).toBeGreaterThanOrEqual(8);
  expect(menuBox!.x + menuBox!.width).toBeLessThanOrEqual(382);
  expect(menuBox!.y).toBeGreaterThanOrEqual(8);
  expect(menuBox!.y + menuBox!.height).toBeLessThanOrEqual(472);
});

test("keeps workbench scrollbars thin and rounded without changing account pages", async ({ page }) => {
  const fixture = await installSessionStreamFixture(page, "run-started");
  await fixture.runUntilBoundary();

  for (const selector of [".sidebar .sb-section", ".term", ".settings-body"]) {
    const scrollbar = await page.locator(selector).evaluate((element) => ({
      width: getComputedStyle(element).scrollbarWidth,
      track: getComputedStyle(element, "::-webkit-scrollbar-track").backgroundColor,
      thumbRadius: getComputedStyle(element, "::-webkit-scrollbar-thumb").borderRadius,
      thumbBorder: getComputedStyle(element, "::-webkit-scrollbar-thumb").borderTopWidth,
    }));
    expect(scrollbar).toEqual({ width: "thin", track: "rgba(0, 0, 0, 0)", thumbRadius: "8px", thumbBorder: "2px" });
  }
  await page.goto("/users");
  await expect(page.locator("body")).not.toHaveCSS("scrollbar-width", "thin");
});

test("keeps the desktop sidebar visible when an old preference requested collapse", async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.addInitScript(() => {
    localStorage.setItem("pi-web.uiLocale", "zh-CN");
    localStorage.setItem("pi.sb.collapsed", "1");
  });
  const fixture = await installSessionStreamFixture(page, "run-started");
  await fixture.runUntilBoundary();

  await expect(page.locator(".sidebar-wrap")).toBeVisible();
  await expect(page.locator(".sb-collapse")).toHaveCount(0);
  await expect(page.locator(".sb-expand-btn")).toHaveCount(0);
  await expect(page.locator(".app")).toHaveAttribute("data-sidebar", "open");
  expect(await page.evaluate(() => localStorage.getItem("pi.sb.collapsed"))).toBeNull();
});

test("uses the OpenDesign modal shell for all four sidebar tools", async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.addInitScript(() => localStorage.setItem("pi-web.uiLocale", "zh-CN"));
  const fixture = await installSessionStreamFixture(page, "run-started");
  await fixture.runUntilBoundary();

  const dialogs = [
    {
      trigger: ".sb-settings",
      panel: ".settings-dialog",
      head: ".settings-head",
      close: ".settings-head [data-action='close-settings']",
      overlay: ".settings-modal",
      footer: ".settings-foot",
      width: 760,
    },
    {
      trigger: ".sb-skill-manager",
      panel: ".skill-manager-dialog",
      head: ".skill-manager-head",
      close: "[data-skill-close]",
      overlay: ".skill-manager-overlay",
      footer: ".skill-manager-foot",
      width: 650,
    },
    {
      trigger: ".sb-prompt-presets",
      panel: ".prompt-preset-dialog",
      head: ".prompt-preset-head",
      close: "[data-prompt-preset-close]",
      overlay: ".prompt-preset-overlay",
      footer: ".prompt-preset-foot",
      width: 650,
    },
    {
      trigger: ".sb-account-admin",
      panel: ".user-management-dialog",
      head: ".user-management-dialog-head",
      close: "[data-user-management-close]",
      overlay: null,
      footer: ".account-pagination",
      width: 760,
    },
  ];

  for (const dialog of dialogs) {
    await page.locator(dialog.trigger).click();
    await expect(page.locator(dialog.panel)).toBeVisible();
    await expect(page.locator(dialog.head)).toBeVisible();
    const shellStyle = await page.locator(dialog.panel).evaluate((element) => {
      const style = getComputedStyle(element);
      return {
        width: Math.round(element.getBoundingClientRect().width),
        radius: style.borderRadius,
        borderWidth: style.borderTopWidth,
        background: style.backgroundColor,
        shadow: style.boxShadow,
      };
    });
    expect(shellStyle.width).toBe(dialog.width);
    expect(shellStyle.radius).toBe("8px");
    expect(shellStyle.borderWidth).toBe("1px");
    expect(shellStyle.background).toBe("oklch(1 0 0)");
    expect(shellStyle.shadow).not.toBe("none");
    const backdropColor = dialog.overlay
      ? await page.locator(dialog.overlay).evaluate((element) => getComputedStyle(element).backgroundColor)
      : await page.locator(dialog.panel).evaluate((element) => getComputedStyle(element, "::backdrop").backgroundColor);
    expect(backdropColor).toBe("rgba(17, 28, 45, 0.36)");
    const headerPadding = await page.locator(dialog.head).evaluate((element) => getComputedStyle(element).padding);
    expect(headerPadding).toBe("13px 17px");
    const footerStyle = await page.locator(dialog.panel).locator(dialog.footer).evaluate((element) => {
      const style = getComputedStyle(element);
      return { padding: style.padding, borderTopWidth: style.borderTopWidth };
    });
    expect(footerStyle).toEqual({ padding: "10px 17px", borderTopWidth: "1px" });
    if (dialog.panel === ".user-management-dialog") {
      const bodyStyle = await page.locator(".user-management-content").evaluate((element) => {
        const style = getComputedStyle(element);
        return { display: style.display, overflowY: style.overflowY };
      });
      expect(bodyStyle).toEqual({ display: "flex", overflowY: "hidden" });
      const cardStyle = await page.locator(".user-management-dialog .account-card").evaluate((element) => {
        const style = getComputedStyle(element);
        return { display: style.display, overflowY: style.overflowY };
      });
      expect(cardStyle).toEqual({ display: "flex", overflowY: "hidden" });
    }

    await page.locator(dialog.panel).locator(dialog.close).click();
    if (dialog.panel === ".user-management-dialog") {
      expect(await page.locator(dialog.panel).evaluate((element) => (element as HTMLDialogElement).open)).toBe(false);
    }
    await expect(page.locator(dialog.panel)).not.toBeVisible();
  }

  await page.setViewportSize({ width: 390, height: 844 });
  await page.addStyleTag({ content: "astro-dev-toolbar { display: none !important; }" });
  await page.locator(".notyf__dismiss-btn").evaluateAll((buttons) => {
    buttons.forEach((button) => (button as HTMLButtonElement).click());
  });
  await expect(page.locator(".notyf__toast")).toHaveCount(0);
  await page.locator("[data-action='open-drawer']").click();
  for (const dialog of dialogs) {
    await page.locator(dialog.trigger).click();
    await expect(page.locator(dialog.panel)).toBeVisible();
    const width = await page.locator(dialog.panel).evaluate((element) =>
      Math.round(element.getBoundingClientRect().width),
    );
    expect(width).toBe(374);
    await page.locator(dialog.panel).locator(dialog.close).click();
    await expect(page.locator(dialog.panel)).not.toBeVisible();
  }
});

test("keeps the OpenDesign sidebar reachable without a mobile conversation title", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  const fixture = await installSessionStreamFixture(page, "run-started");
  await fixture.runUntilBoundary();

  const promptBar = await page.locator(".prompt-bar").boundingBox();
  expect(promptBar?.x).toBeGreaterThanOrEqual(0);
  expect(promptBar!.x + promptBar!.width).toBeLessThanOrEqual(390);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);

  await expect(page.locator(".main-content h1")).toHaveCount(0);
  const openDrawer = page.locator("[data-action='open-drawer']");
  await expect(openDrawer).toBeVisible();
  await expect(openDrawer).toHaveCSS("width", "44px");
  await expect(openDrawer).toHaveCSS("height", "44px");
  await openDrawer.click();
  await expect(page.locator(".app-body")).toHaveClass(/drawer-open/);
  await expect(page.locator(".sidebar")).toBeInViewport();

  await page.locator(".side-head [data-action='close-drawer']").click();
  await expect(page.locator(".app-body")).not.toHaveClass(/drawer-open/);
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
