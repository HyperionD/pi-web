export type SlashCommand = {
  name?: string;
  command?: string;
  cmd?: string;
  description?: string;
  desc?: string;
  source?: string;
  scope?: string;
  location?: string;
  availableWhileRunning?: boolean;
};

export const BUILTIN_SLASH_COMMANDS: SlashCommand[] = [
  {
    name: "compact", command: "/compact", description: "压缩当前会话上下文", source: "builtin", scope: "global",
  },
  { name: "reload", command: "/reload", description: "重新加载会话资源", source: "builtin", scope: "global" },
  { name: "name", command: "/name", description: "重命名当前会话", source: "builtin", scope: "global" },
  {
    name: "session", command: "/session", description: "查看当前会话统计", source: "builtin", scope: "global",
    availableWhileRunning: true,
  },
  {
    name: "copy", command: "/copy", description: "复制最近一条助手消息", source: "builtin", scope: "global",
    availableWhileRunning: true,
  },
  { name: "clone", command: "/clone", description: "克隆当前会话分支", source: "builtin", scope: "global" },
];

const SOURCE_ORDER = { builtin: 0, extension: 1, prompt: 2, skill: 3 };

export function commandName(command: SlashCommand): string {
  const name = command.name || command.command?.replace(/^\//, "") || command.cmd?.replace(/^\//, "") || "";
  return name.replace(/^\//, "").trim();
}

export function commandValue(command: SlashCommand): string {
  return command.command || command.cmd || `/${commandName(command)}`;
}

export function commandDescription(command: SlashCommand): string {
  return command.description || command.desc || "";
}

export function getSlashQuery(value = ""): string | null {
  if (!value.startsWith("/") || /\s/.test(value.slice(1))) return null;
  return value.slice(1).toLowerCase();
}

function matchRank(command: SlashCommand, query: string): number {
  const name = commandName(command).toLowerCase();
  const description = commandDescription(command).toLowerCase();
  if (name === query) return 0;
  if (name.startsWith(query)) return 1;
  if (name.includes(query)) return 2;
  if (description.includes(query)) return 3;
  return 4;
}

export function filterSlashCommands(commands: SlashCommand[], query: string, running = false): SlashCommand[] {
  return commands
    .filter((command) => {
      if (running && command.source === "builtin" && !command.availableWhileRunning) return false;
      const name = commandName(command).toLowerCase();
      const description = commandDescription(command).toLowerCase();
      return name.includes(query) || description.includes(query);
    })
    .sort((left, right) => (
      matchRank(left, query) - matchRank(right, query)
      || (SOURCE_ORDER[left.source] ?? 99) - (SOURCE_ORDER[right.source] ?? 99)
      || commandName(left).localeCompare(commandName(right))
    ));
}

export function mergeSlashCommands(commands: SlashCommand[] = []): SlashCommand[] {
  const merged = [...BUILTIN_SLASH_COMMANDS, ...commands];
  const seen = new Set<string>();
  return merged.filter((command) => {
    const name = commandName(command);
    if (!name) return false;
    const key = name.toLowerCase();
    if (seen.has(key)) return false;
    seen.add(key);
    return true;
  }).map((command) => ({
    ...command,
    name: commandName(command),
    command: commandValue(command),
  }));
}

export function isExactSlashCommand(value: string, command: SlashCommand): boolean {
  return value.trim().toLowerCase() === commandValue(command).toLowerCase();
}
