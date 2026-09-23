import { describe, expect, it } from "vitest";
import { calculateOutputRate, estimateIncrementalOutputTokens, estimateOutputTokens } from "./output-rate";

describe("output rate estimation", () => {
  it("estimates CJK characters more densely than latin text", () => {
    expect(estimateOutputTokens("你好世界")).toBe(4);
    expect(estimateOutputTokens("abcd")).toBe(1);
  });

  it("updates an estimate from only the streamed suffix", () => {
    const first = { text: "abcd", tokens: estimateOutputTokens("abcd") };
    const next = estimateIncrementalOutputTokens(first, "abcdef你好");
    expect(next).toBe(3.5);
  });

  it("waits for a stable sample before calculating rate", () => {
    expect(calculateOutputRate(20, 1000, 1400)).toBeNull();
    expect(calculateOutputRate(20, 1000, 2000)).toBe(20);
  });
});
