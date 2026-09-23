import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const pictureStyles = readFileSync(resolve(process.cwd(), "src/styles/parts/picture-ui.css"), "utf8");
const responsiveStyles = readFileSync(resolve(process.cwd(), "src/styles/parts/responsive-themes.css"), "utf8");

describe("workspace grid styling", () => {
  it("keeps viewport-fixed menus out of the transformed mobile drawer", () => {
    const mobileSidebarRule = responsiveStyles.match(/\.sidebar\s*\{[^}]*position:\s*absolute;[^}]*\}/)?.[0];
    expect(mobileSidebarRule).toBeTruthy();
    const style = document.createElement("style");
    style.textContent = mobileSidebarRule || "";
    document.head.append(style);
    const sidebar = document.createElement("aside");
    sidebar.className = "sidebar";
    document.body.append(sidebar);
    try {
      expect(getComputedStyle(sidebar).transform).toBe("none");
    } finally {
      style.remove();
      sidebar.remove();
    }
  });

  it("allows the live sidebar width to override the picture theme", () => {
    const appBodyRule = pictureStyles.match(/\.app-body\s*\{[^}]*\}/)?.[0];
    expect(appBodyRule).toBeTruthy();
    const style = document.createElement("style");
    style.textContent = appBodyRule || "";
    document.head.append(style);
    const body = document.createElement("section");
    body.className = "app-body";
    body.style.gridTemplateColumns = "360px 1fr";
    document.body.append(body);
    try {
      expect(getComputedStyle(body).gridTemplateColumns).toBe("360px 1fr");
    } finally {
      style.remove();
      body.remove();
    }
  });
});
