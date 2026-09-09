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
    expect(styles).toContain(".account-nav");
    expect(styles).not.toContain("color-mix(");
    assertViewportFallback(styles);
  });

  it("rejects a fallback placed inside the complete dynamic viewport feature query", () => {
    expect(() =>
      assertViewportFallback(`
        @supports (height: 100dvh) {
          :root { --app-viewport-height: 100vh; }
        }
      `),
    ).toThrow();
  });

  it("rejects a fallback found only inside a CSS comment", () => {
    expect(() =>
      assertViewportFallback(`
        /* :root { --app-viewport-height: 100vh; } */
        @supports (height: 100dvh) {
          :root { --app-viewport-height: 100dvh; }
        }
      `),
    ).toThrow();
  });

  it("rejects uppercase dynamic viewport units outside the feature query", () => {
    expect(() => assertViewportFallback(".app { height: 100DVH; }")).toThrow();
  });
});

function collectAuthoredCss(directory: URL): string {
  return readdirSync(directory, { withFileTypes: true })
    .flatMap((entry) => {
      const url = new URL(`${entry.name}${entry.isDirectory() ? "/" : ""}`, directory);
      if (entry.isDirectory()) return [collectAuthoredCss(url)];
      if (entry.name.endsWith(".css")) return [readFileSync(url, "utf8")];
      if (entry.name.endsWith(".astro")) return extractStyleBlocks(readFileSync(url, "utf8"));
      return [];
    })
    .join("\n");
}

function extractStyleBlocks(source: string): string[] {
  return [...source.matchAll(/<style(?:\s[^>]*)?>([\s\S]*?)<\/style\s*>/gi)].map(
    (match) => match[1],
  );
}

function stripCssComments(css: string): string {
  return css.replace(/\/\*[\s\S]*?\*\//g, "");
}

function assertViewportFallback(css: string): void {
  const outsideViewportFeatureQueries = stripViewportFeatureQueries(stripCssComments(css));
  expect(outsideViewportFeatureQueries).toMatch(
    /--app-viewport-height\s*:\s*100vh\b/i,
  );
  expect(outsideViewportFeatureQueries).not.toMatch(/100dvh/i);
}

function stripViewportFeatureQueries(css: string): string {
  return stripFeatureQueries(css, /@supports\s*\(\s*height\s*:\s*100dvh\s*\)\s*\{/i);
}

function stripFeatureQueries(css: string, featureQuery: RegExp): string {
  let cursor = 0;
  let stripped = "";
  while (true) {
    const match = featureQuery.exec(css.slice(cursor));
    if (!match || match.index === undefined) break;
    const start = cursor + match.index;
    const openingBrace = start + match[0].length - 1;
    const end = findMatchingBrace(css, openingBrace);
    stripped += css.slice(cursor, start);
    cursor = end + 1;
  }
  return stripped + css.slice(cursor);
}

function findMatchingBrace(css: string, openingBrace: number): number {
  let depth = 0;
  let quote = "";
  for (let index = openingBrace; index < css.length; index += 1) {
    const character = css[index];
    if (quote) {
      if (character === quote && css[index - 1] !== "\\") quote = "";
      continue;
    }
    if (character === "/" && css[index + 1] === "*") {
      const commentEnd = css.indexOf("*/", index + 2);
      if (commentEnd < 0) throw new Error("Unclosed CSS comment");
      index = commentEnd + 1;
      continue;
    }
    if (character === '"' || character === "'") quote = character;
    if (character === "{") depth += 1;
    if (character === "}" && --depth === 0) return index;
  }
  throw new Error("Unclosed CSS feature query");
}
