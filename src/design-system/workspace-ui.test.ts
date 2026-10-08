import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { afterEach, describe, expect, it } from "vitest";

const foundationStylesPath = resolve(process.cwd(), "src/styles/parts/foundation-layout.css");
const treePreviewStylesPath = resolve(process.cwd(), "src/styles/parts/tree-preview-status.css");
const workspaceStylesPath = resolve(process.cwd(), "src/styles/parts/picture-ui.css");
const appTemplatePath = resolve(process.cwd(), "src/App.astro");
const foundationStyles = readFileSync(foundationStylesPath, "utf8");
const treePreviewStyles = readFileSync(treePreviewStylesPath, "utf8");
const workspaceStyles = readFileSync(workspaceStylesPath, "utf8");
const appTemplate = readFileSync(appTemplatePath, "utf8");

function installWorkspaceStyles() {
  const style = document.createElement("style");
  style.textContent = `${foundationStyles}\n${treePreviewStyles}\n${workspaceStyles}`;
  document.head.append(style);
}

function declarationsFor(selector: string) {
  const escapedSelector = selector.replaceAll(".", "[.]");
  const block = workspaceStyles.match(new RegExp(`${escapedSelector}\\s*\\{([^}]*)\\}`))?.[1] || "";
  return Object.fromEntries(block.split(";").map((declaration) => {
    const [property, ...value] = declaration.split(":");
    return [property.trim(), value.join(":").trim()];
  }).filter(([property]) => property));
}

afterEach(() => {
  document.head.replaceChildren();
  document.body.replaceChildren();
});

describe("workspace UI styles", () => {
  it("uses one canvas background across the sidebar while keeping the workspace menu distinct", () => {
    const selectors = [
      ".sidebar-wrap", ".sidebar", ".workspace-trigger", ".sidebar .sb-section", ".tree", ".sb-footer",
    ];
    for (const selector of selectors) {
      expect(declarationsFor(selector).background, selector).toBe("var(--picture-canvas)");
    }
    expect(declarationsFor(".workspace-menu").background).toBe("var(--picture-surface)");
  });

  it("removes the decorative cursor block from the sidebar brand", () => {
    expect(appTemplate).not.toContain('<span class="cursor-block"></span>');
  });

  it("keeps the composer outline continuous without clipping its disclosure", () => {
    expect(workspaceStyles).toMatch(/\.prompt-bar\s*\{[^}]*overflow:\s*visible;/s);
    expect(workspaceStyles).toMatch(/\.prompt-bar::after\s*\{[^}]*border:\s*1px solid var\(--picture-border-strong\)/s);
    expect(workspaceStyles).toMatch(/\.prompt-bar::after\s*\{[^}]*border-radius:\s*inherit/s);
    expect(workspaceStyles).toMatch(/\.prompt-bar::after\s*\{[^}]*pointer-events:\s*none/s);
  });

  it("uses UI typography for prose and monospace for code and workspace paths", () => {
    installWorkspaceStyles();
    const app = document.createElement("div");
    app.className = "app";
    app.style.setProperty("--font-ui", "Arial, sans-serif");
    app.style.setProperty("--font-mono", "monospace");

    const row = document.createElement("div");
    row.className = "msg";
    const prose = document.createElement("span");
    prose.className = "body";
    const code = document.createElement("code");
    prose.append(code);
    row.append(prose);

    const workspaceRow = document.createElement("div");
    workspaceRow.className = "ws-row";
    const path = document.createElement("span");
    path.className = "ws-path";
    workspaceRow.append(path);
    app.append(row, workspaceRow);
    document.body.append(app);

    expect(getComputedStyle(prose).fontFamily).toBe("var(--font-ui)");
    expect(getComputedStyle(code).fontFamily).toBe("var(--font-mono)");
    expect(getComputedStyle(path).fontFamily).toBe("var(--font-mono)");
  });

  it("gives footer actions full-size targets and narrows transcript measure", () => {
    installWorkspaceStyles();
    const app = document.createElement("div");
    app.className = "app";
    const footer = document.createElement("nav");
    footer.className = "sb-footer-actions";
    const hiddenAdminAction = document.createElement("button");
    hiddenAdminAction.hidden = true;
    const action = document.createElement("button");
    const secondAction = document.createElement("button");
    const thirdAction = document.createElement("button");
    footer.append(hiddenAdminAction, action, secondAction, thirdAction);
    const transcript = document.createElement("div");
    transcript.className = "term-inner";
    app.append(footer, transcript);
    document.body.append(app);

    expect(getComputedStyle(footer).display).toBe("flex");
    expect(getComputedStyle(hiddenAdminAction).display).toBe("none");
    expect(getComputedStyle(action).flexGrow).toBe("1");
    expect(getComputedStyle(secondAction).flexGrow).toBe("1");
    expect(getComputedStyle(thirdAction).flexGrow).toBe("1");
    expect(getComputedStyle(action).minHeight).toBe("44px");
    expect(getComputedStyle(transcript).maxWidth).toBe("830px");
    expect(getComputedStyle(document.documentElement).getPropertyValue("--picture-canvas").trim())
      .toBe("rgb(239,241,244)");
  });

  it("matches the compact file tabs and quiet header icon actions", () => {
    const tab = declarationsFor(".tree-head .tree-tab");
    const selectedTab = declarationsFor(".tree-head .tree-tab.on");
    const actions = declarationsFor(".tree-head-actions button");

    expect(tab["min-height"]).toBe("30px");
    expect(tab.padding).toBe("0 9px");
    expect(tab.font).toBe("500 11px/1 var(--font-ui)");
    expect(selectedTab.background).toBe("var(--picture-blue-soft)");
    expect(selectedTab["border-color"]).toBe("var(--picture-border)");
    expect(actions.width).toBe("29px");
    expect(actions.height).toBe("30px");
    expect(actions.background).toBe("transparent");
    expect(actions.border).toBe("1px solid transparent");
    expect(appTemplate).toContain("<Folder size={14}");
    expect(appTemplate).toContain("<GitBranch size={14}");
    expect(appTemplate).toContain("<Plus size={16}");
    expect(appTemplate).toContain("<RefreshCw size={16}");
  });

  it("keeps attachment limits collapsed until requested", () => {
    installWorkspaceStyles();
    const disclosure = document.createElement("details");
    disclosure.className = "attachment-guidance-disclosure";
    const summary = document.createElement("summary");
    disclosure.append(summary);
    document.body.append(disclosure);

    expect(disclosure.open).toBe(false);
    expect(getComputedStyle(summary).minHeight).toBe("28px");
    expect(getComputedStyle(summary).cursor).toBe("pointer");
  });
});
