import { describe, expect, it } from "vitest";
import {
  BUILTIN_SLASH_COMMANDS,
  builtinSlashCommandError,
  filterSlashCommands,
  getSlashQuery,
  isExactSlashCommand,
  mergeSlashCommands,
  parseBuiltinSlashCommand,
} from "./slash-commands";

describe("slash command helpers", () => {
  it("only returns a query before the first command argument", () => {
    expect(getSlashQuery("/")).toBe("");
    expect(getSlashQuery("/mod")).toBe("mod");
    expect(getSlashQuery("/name my session")).toBeNull();
    expect(getSlashQuery("hello /mod")).toBeNull();
  });

  it("ranks exact and name matches before descriptions or metadata", () => {
    const commands = [
      { name: "team", description: "model helper", source: "extension" },
      { name: "model", description: "change model", source: "prompt" },
      { name: "model-fast", description: "fast model", source: "skill" },
    ];

    expect(filterSlashCommands(commands, "model").map((command) => command.name)).toEqual([
      "model",
      "model-fast",
      "team",
    ]);
  });

  it("merges built-ins without duplicating commands from the runtime", () => {
    const commands = mergeSlashCommands([
      { name: "compact", description: "runtime compact", source: "prompt" },
      { name: "review", description: "review changes", source: "prompt" },
    ]);

    expect(commands.filter((command) => command.name === "compact")).toHaveLength(1);
    expect(commands.some((command) => command.name === "review")).toBe(true);
    expect(commands.filter((command) => command.source === "builtin")).toHaveLength(
      BUILTIN_SLASH_COMMANDS.length,
    );
  });

  it("parses known builtin commands and preserves arguments", () => {
    expect(parseBuiltinSlashCommand("/name project alpha")).toMatchObject({
      command: { name: "name" },
      args: "project alpha",
    });
    expect(parseBuiltinSlashCommand("/review")).toBeNull();
    expect(parseBuiltinSlashCommand("hello /copy")).toBeNull();
  });

  it("recognizes only an exact command without arguments", () => {
    const command = { name: "model", command: "/model", source: "prompt" };
    expect(isExactSlashCommand("/model", command)).toBe(true);
    expect(isExactSlashCommand("/MODEL ", command)).toBe(true);
    expect(isExactSlashCommand("/model gpt", command)).toBe(false);
  });

  it("rejects unsupported builtin arguments instead of dropping them", () => {
    expect(builtinSlashCommandError("compact", "instructions")).toBe("Usage: /compact");
    expect(builtinSlashCommandError("copy", "extra")).toBe("Usage: /copy");
    expect(builtinSlashCommandError("name", "release")).toBeNull();
    expect(builtinSlashCommandError("name", "")).toBe("Usage: /name <name>");
  });
});
