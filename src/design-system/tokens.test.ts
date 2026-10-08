import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { afterEach, describe, expect, it } from "vitest";

const tokensPath = resolve(process.cwd(), "src/design-system/tokens.css");

afterEach(() => {
  document.head.replaceChildren();
});

describe("light theme tokens", () => {
  it("applies the approved light surface, text, border, and accent colors", () => {
    const style = document.createElement("style");
    style.textContent = readFileSync(tokensPath, "utf8");
    document.head.append(style);

    const root = getComputedStyle(document.documentElement);
    expect(root.getPropertyValue("--bg-0").trim()).toBe("#ffffff");
    expect(root.getPropertyValue("--bg-1").trim()).toBe("#f8fafc");
    expect(root.getPropertyValue("--border").trim()).toBe("#cbd5e1");
    expect(root.getPropertyValue("--fg-0").trim()).toBe("#0f172a");
    expect(root.getPropertyValue("--fg-2").trim()).toBe("#475569");
    expect(root.getPropertyValue("--accent").trim()).toBe("#2563eb");
  });

  it("separates Chinese UI typography from technical monospace typography", () => {
    const style = document.createElement("style");
    style.textContent = readFileSync(tokensPath, "utf8");
    document.head.append(style);

    const root = getComputedStyle(document.documentElement);
    const sans = root.getPropertyValue("--font-sans").trim();
    const mono = root.getPropertyValue("--font-mono").trim();

    expect(sans).toContain('"Noto Sans SC"');
    expect(sans).not.toBe(mono);
    expect(mono).toContain('"Fira Code"');
  });
});
