import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const sessionStyles = readFileSync(resolve(process.cwd(), "src/styles/parts/sidebar-workspaces.css"), "utf8");
const fileTreeStyles = readFileSync(resolve(process.cwd(), "src/styles/parts/tree-preview-status.css"), "utf8");

function declarationsFor(styles: string, selector: string) {
  const escapedSelector = selector.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  const block = styles.match(new RegExp(`${escapedSelector}\\s*\\{([^}]*)\\}`))?.[1] || "";
  return Object.fromEntries(block.split(";").map((declaration) => {
    const [property, ...value] = declaration.split(":");
    return [property.trim(), value.join(":").trim()];
  }).filter(([property]) => property));
}

describe("workspace file tree popup styles", () => {
  it("matches the session menu spacing between popup items", () => {
    const sessionSpacing = declarationsFor(sessionStyles, ".session-menu button + button");
    const fileSpacing = declarationsFor(fileTreeStyles, ".tree-action-menu button + button");
    expect(fileSpacing["margin-top"]).toBe(sessionSpacing["margin-top"]);
  });

  it("matches the session popup surface and menu item appearance", () => {
    const sessionMenu = declarationsFor(sessionStyles, ".session-menu");
    const fileMenu = declarationsFor(fileTreeStyles, ".tree-action-menu");
    const sessionItem = declarationsFor(sessionStyles, ".session-menu button");
    const fileItem = declarationsFor(fileTreeStyles, ".tree-action-menu button");

    for (const property of ["position", "z-index", "border", "border-radius", "background", "box-shadow", "padding"]) {
      expect(fileMenu[property], `.tree-action-menu ${property}`).toBe(sessionMenu[property]);
    }
    const itemProperties = [
      "display", "align-items", "gap", "width", "padding", "color", "background",
      "border", "border-radius", "font", "font-size", "text-align", "cursor",
    ];
    for (const property of itemProperties) {
      expect(fileItem[property], `.tree-action-menu button ${property}`).toBe(sessionItem[property]);
    }
  });
});
