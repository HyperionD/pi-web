import { z } from "zod";
import type { PromptPreset } from "../../lib/api";
import { commandDescription, commandName, type SlashCommand } from "./slash-commands";

export type { PromptPreset };

const promptPresetSchema = z.object({
  id: z.string().min(1),
  label: z.string().min(1),
  skill: z.string(),
  prompt: z.string().min(1),
  enabled: z.boolean(),
  order: z.number().int(),
}).passthrough();

export function parsePromptPresetsResponse(value: unknown): PromptPreset[] {
  const result = z.object({ presets: z.array(promptPresetSchema) }).safeParse(value);
  if (!result.success) throw new Error("invalid prompt presets response");
  return result.data.presets;
}

export function parsePromptPresetResponse(value: unknown): PromptPreset {
  const result = z.object({ preset: promptPresetSchema }).safeParse(value);
  if (!result.success) throw new Error("invalid prompt preset response");
  return result.data.preset;
}

export type PromptPresetSkillOption = {
  value: string;
  label: string;
  description: string;
};

export function promptPresetSkillOptions(commands: SlashCommand[] = []): PromptPresetSkillOption[] {
  const options = new Map<string, PromptPresetSkillOption>();
  for (const command of commands) {
    if (command.source !== "skill") continue;
    const value = commandName(command).replace(/^skill:/i, "").trim();
    if (!value || options.has(value)) continue;
    options.set(value, { value, label: value, description: commandDescription(command) });
  }
  return [...options.values()].sort((left, right) => left.label.localeCompare(right.label));
}

export function formatPromptPreset(preset: PromptPreset): string {
  const prompt = preset.prompt.trim();
  const skill = preset.skill.trim();
  return skill ? `/skill ${skill}\n\n${prompt}` : prompt;
}

export function visiblePromptPresets(presets: PromptPreset[] = []): PromptPreset[] {
  return presets
    .filter((preset) => preset.enabled)
    .sort((left, right) => left.order - right.order || left.label.localeCompare(right.label));
}
