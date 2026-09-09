// @vitest-environment node
import { readdirSync, readFileSync } from "node:fs";
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
    expect(app).toContain('<textarea class="prompt-textarea" placeholder="输入你的想法" rows="1" disabled={!activeWorkspaceID}></textarea>');
  });

  it("starts in the empty state when no workspace is rendered", () => {
    const app = readFileSync(new URL("./App.astro", import.meta.url), "utf8");
    expect(app).toContain('const emptyVisible = initialSession === "empty" || !activeWorkspaceID;');
  });

  it("provides a Chrome 107 viewport fallback for session toasts", () => {
    const styles = readFileSync(new URL("./styles/parts/session-toasts.css", import.meta.url), "utf8");
    expect(styles).toContain("max-height: calc(100vh - var(--space-8));");
    expect(styles).not.toContain("100dvh");
  });

  it("does not use authored CSS features newer than the Chrome 107 baseline", () => {
    const styles = collectAuthoredCss(new URL(".", import.meta.url));
    expect(styles).toContain("--app-viewport-height: 100vh");
    expect(styles).not.toContain("color-mix(");
    const authoredDeclarations = styles.replace(
      "@supports (height: 100dvh) {\n  :root { --app-viewport-height: 100dvh; }\n}\n",
      "",
    );
    expect(authoredDeclarations).not.toContain("100dvh");
  });
});

function collectAuthoredCss(directory: URL): string {
  return readdirSync(directory, { withFileTypes: true })
    .flatMap((entry) => {
      const url = new URL(`${entry.name}${entry.isDirectory() ? "/" : ""}`, directory);
      if (entry.isDirectory()) return collectAuthoredCss(url);
      return entry.name.endsWith(".css") ? [readFileSync(url, "utf8")] : [];
    })
    .join("\n");
}
