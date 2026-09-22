import { describe, expect, it } from "vitest";
import {
  formatPromptPreset,
  parsePromptPresetsResponse,
  promptPresetSkillOptions,
  visiblePromptPresets,
} from "./prompt-presets";

const presets = [
  { id: "later", label: "后置", skill: "review", prompt: "检查代码", enabled: true, order: 20 },
  { id: "disabled", label: "隐藏", skill: "", prompt: "不显示", enabled: false, order: 1 },
  { id: "first", label: "前置", skill: "", prompt: "先说明", enabled: true, order: 10 },
];

describe("prompt preset helpers", () => {
  it("formats a preset with its skill before the prompt", () => {
    expect(formatPromptPreset(presets[0])).toBe("/skill review\n\n检查代码");
  });

  it("formats a preset without a skill as plain prompt text", () => {
    expect(formatPromptPreset(presets[2])).toBe("先说明");
  });

  it("returns only enabled presets in stable order", () => {
    expect(visiblePromptPresets(presets).map((preset) => preset.id)).toEqual(["first", "later"]);
  });

  it("rejects malformed server responses", () => {
    expect(() => parsePromptPresetsResponse({ presets: [{ id: "missing-fields" }] })).toThrow("invalid prompt presets response");
  });

  it("converts skill slash commands into selectable skill names", () => {
    expect(promptPresetSkillOptions([
      { name: "skill:debug", command: "/skill:debug", source: "skill", description: "调试问题" },
      { name: "review", command: "/review", source: "prompt", description: "普通命令" },
    ])).toEqual([
      { value: "debug", label: "debug", description: "调试问题" },
    ]);
  });
});
