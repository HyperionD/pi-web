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
});
