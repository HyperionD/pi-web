import { afterEach, describe, expect, it } from "vitest";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";

const terminalStyles = readFileSync(
  resolve(process.cwd(), "src/styles/parts/terminal-messages.css"),
  "utf8",
);

let styleElement: HTMLStyleElement | undefined;

afterEach(() => {
  styleElement?.remove();
  styleElement = undefined;
  document.body.replaceChildren();
});

describe("transcript scrolling styles", () => {
  it("keeps virtual transcript positioning immediate and disables browser anchoring", () => {
    styleElement = document.createElement("style");
    styleElement.textContent = terminalStyles;
    document.head.append(styleElement);
    const term = document.createElement("div");
    term.className = "term";
    document.body.append(term);

    const styles = getComputedStyle(term);

    expect(styles.scrollBehavior).toBe("auto");
    expect(styles.overflowAnchor).toBe("none");
  });

  it("keeps streaming tail in the same non-anchored content layer", () => {
    expect(terminalStyles).toContain(".streaming-tail");

    const streamingTailRule =
      terminalStyles.match(/(?:^|\n)\s*\.streaming-tail\s*\{([^}]*)\}/)?.[1] ?? "";
    const declaration = (property: string, value: string) =>
      new RegExp(`(?:^|;)\\s*${property}\\s*:\\s*${value}\\s*;?`);

    expect(streamingTailRule).toMatch(declaration("position", "relative"));
    expect(streamingTailRule).toMatch(declaration("z-index", "2"));
    expect(streamingTailRule).toMatch(declaration("max-width", "960px"));
    expect(streamingTailRule).toMatch(declaration("min-height", "0"));
    expect(streamingTailRule).toMatch(declaration("overflow-anchor", "none"));
    expect(streamingTailRule).not.toMatch(
      /(?:^|;)\s*(?:overflow|overflow-x|overflow-y|scroll-behavior)\s*:/,
    );
  });
});
