// @vitest-environment node
import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";
import astroConfig from "../astro.config";

describe("browser compatibility", () => {
  it("builds production assets explicitly for Chrome 107", () => {
    expect(astroConfig.vite?.build?.target).toBe("chrome107");
  });

  it("uses Simplified Chinese as the default document language", () => {
    const page = readFileSync(new URL("./pages/index.astro", import.meta.url), "utf8");
    expect(page).toContain('<html lang="zh-CN">');
  });

  it("shows an idea-focused placeholder in the prompt composer", () => {
    const app = readFileSync(new URL("./App.astro", import.meta.url), "utf8");
    expect(app).toContain('<textarea class="prompt-textarea" placeholder="输入你的想法" rows="1"></textarea>');
  });

  it("does not use authored CSS features newer than the Chrome 107 baseline", () => {
    const styles = [
      "./styles.css",
      "./design-system/tokens.css",
      "./styles/parts/sidebar-workspaces.css",
      "./styles/parts/prompt-composer.css",
      "./styles/parts/settings-modal.css",
      "./styles/parts/session-toasts.css",
      "./styles/parts/session-agent-children.css",
    ].map((path) => readFileSync(new URL(path, import.meta.url), "utf8")).join("\n");
    expect(styles).not.toContain("color-mix(");
  });
});
