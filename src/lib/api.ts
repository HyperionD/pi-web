import { downloadWorkspaceFile, uploadWorkspaceFile } from "./file-transfer";
import { normalizeEventCursor } from "../pi-app/sessions/session-recovery";
import { apiBase } from "./api-base";

export { apiBase } from "./api-base";

export type AppConfigPatch = {
  brand?: string | null;
  workspace?: { allowedRoots?: string[]; maxUploadBytes?: number } | null;
};

export type PromptPreset = {
  id: string;
  label: string;
  skill: string;
  prompt: string;
  enabled: boolean;
  order: number;
  createdAt?: string;
  updatedAt?: string;
};

export type PromptPresetInput = {
  label: string;
  skill?: string;
  prompt: string;
  enabled?: boolean;
  order?: number;
};

export type PromptPresetPatch = Partial<PromptPresetInput>;

export type WorkspaceSkill = {
  name: string;
  description: string;
  path: string;
  scope: "global" | "project" | "path" | "temporary";
  enabled: boolean;
  sourceKind: "repository" | "package" | "local" | "temporary";
  sourceId: string;
  sourceLabel: string;
};

export type WorkspaceSkillsResponse = {
  skills: WorkspaceSkill[];
  available: boolean;
  error?: string;
  notice?: string;
};

export type AdminPasswordStatus = {
  configured: boolean;
};

export type FolderListing = {
  path: string;
  displayPath?: string;
  parent?: string;
  parentDisplayPath?: string;
  rootListing?: boolean;
  folders?: Array<{ name: string; path: string; displayPath?: string }>;
};

async function request(path: string, options: RequestInit = {}) {
  const response = await fetch(`${apiBase()}${path}`, {
    ...options,
    credentials: "include",
    headers: {
      "Content-Type": "application/json",
      ...options.headers,
    },
  });
  if (!response.ok) {
    let message = `${response.status} ${response.statusText}`;
    try {
      const body = await response.json();
      if (body.error) message = body.error;
    } catch {}
    const error = new Error(message);
    (error as any).status = response.status;
    throw error;
  }
  if (response.status === 204 || response.status === 205) return undefined;
  return response.json();
}

export function health() {
  return request("/api/health");
}

export function getWorkspaces() {
  return request("/api/workspaces");
}

export function getAdminPasswordStatus(): Promise<AdminPasswordStatus> {
  return request("/api/admin/password/status");
}

export function setupAdminPassword(password: string, confirmation: string) {
  return request("/api/admin/password", {
    method: "POST",
    body: JSON.stringify({ password, confirmation }),
  });
}

export function getAppConfig() {
  return request("/api/app-config");
}

export function saveAppConfig(config: AppConfigPatch, adminPassword = "") {
  return request("/api/app-config", {
    method: "PATCH",
    headers: adminPassword ? { "X-Admin-Password": adminPassword } : undefined,
    body: JSON.stringify(config),
  });
}

export function getPromptPresets(): Promise<{ presets: PromptPreset[] }> {
  return request("/api/prompt-presets");
}

export function createPromptPreset(preset: PromptPresetInput, adminPassword = "") {
  return request("/api/prompt-presets", {
    method: "POST",
    headers: adminPassword ? { "X-Admin-Password": adminPassword } : undefined,
    body: JSON.stringify(preset),
  });
}

export function updatePromptPreset(id: string, patch: PromptPresetPatch, adminPassword = "") {
  return request(`/api/prompt-presets/${encodeURIComponent(id)}`, {
    method: "PATCH",
    headers: adminPassword ? { "X-Admin-Password": adminPassword } : undefined,
    body: JSON.stringify(patch),
  });
}

export function deletePromptPreset(id: string, adminPassword = "") {
  return request(`/api/prompt-presets/${encodeURIComponent(id)}`, {
    method: "DELETE",
    headers: adminPassword ? { "X-Admin-Password": adminPassword } : undefined,
  });
}

export function getAuthProviders() {
  return request("/api/auth/providers");
}

export function saveAPIKey(provider, apiKey) {
  return request("/api/auth/api-key", {
    method: "POST",
    body: JSON.stringify({ provider, apiKey }),
  });
}

export function getOAuthProviders() {
  return request("/api/auth/oauth/providers");
}

export function startOAuthLogin(provider) {
  return request("/api/auth/oauth/start", {
    method: "POST",
    body: JSON.stringify({ provider }),
  });
}

export function getOAuthLoginSession(sessionId) {
  return request(`/api/auth/oauth/sessions/${encodeURIComponent(sessionId)}`);
}

export function sendOAuthLoginInput(sessionId, value) {
  return request(`/api/auth/oauth/sessions/${encodeURIComponent(sessionId)}/input`, {
    method: "POST",
    body: JSON.stringify({ value }),
  });
}

export function logoutProvider(provider) {
  return request(`/api/auth/${encodeURIComponent(provider)}`, { method: "DELETE" });
}

export function openWorkspace(path) {
  return request("/api/workspaces/open", {
    method: "POST",
    body: JSON.stringify({ path }),
  });
}

export function cloneWorkspace(parentPath, gitUrl, name = "") {
  return request("/api/workspaces/clone", {
    method: "POST",
    body: JSON.stringify({ parentPath, gitUrl, name }),
  });
}

export function listFolders(path = "~", purpose?: "workspace" | "allowed-root"): Promise<FolderListing> {
  const query = `path=${encodeURIComponent(path)}${purpose ? `&purpose=${encodeURIComponent(purpose)}` : ""}`;
  return request(`/api/system/folders?${query}`);
}

export function getWorkspaceSessions(workspaceId) {
  return request(`/api/workspaces/${encodeURIComponent(workspaceId)}/sessions`);
}

export function deleteWorkspaceSessions(workspaceId) {
  return request(`/api/workspaces/${encodeURIComponent(workspaceId)}/sessions`, { method: "DELETE" });
}

export function createSession(workspaceId) {
  return request(`/api/workspaces/${encodeURIComponent(workspaceId)}/sessions`, { method: "POST" });
}

export function closeWorkspace(workspaceId) {
  return request(`/api/workspaces/${encodeURIComponent(workspaceId)}`, { method: "DELETE" });
}

export function getWorkspaceFiles(workspaceId) {
  return request(`/api/workspaces/${encodeURIComponent(workspaceId)}/files`);
}

export function getWorkspaceSkills(workspaceId: string): Promise<WorkspaceSkillsResponse> {
  return request(`/api/workspaces/${encodeURIComponent(workspaceId)}/skills`);
}

export function setWorkspaceSkillEnabled(workspaceId: string, path: string, enabled: boolean) {
  return request(`/api/workspaces/${encodeURIComponent(workspaceId)}/skills`, {
    method: "PATCH",
    body: JSON.stringify({ path, enabled }),
  });
}

export function searchWorkspaceFiles(workspaceId, query) {
  return request(`/api/workspaces/${encodeURIComponent(workspaceId)}/files/search?q=${encodeURIComponent(query)}`);
}

export function getWorkspaceCommands(workspaceId) {
  return request(`/api/workspaces/${encodeURIComponent(workspaceId)}/commands`);
}

export function getSessionCommands(sessionId: string) {
  return request(`/api/sessions/${encodeURIComponent(sessionId)}/commands`);
}

export function sendSessionCommand(sessionId: string, command: string, args = "", clientRequestId = "") {
  return request(`/api/sessions/${encodeURIComponent(sessionId)}/command`, {
    method: "POST",
    headers: clientRequestId ? { "X-Client-Request-ID": clientRequestId } : undefined,
    body: JSON.stringify({ command, ...(args ? { args } : {}) }),
  });
}

export function getWorkspaceModels(workspaceId) {
  return request(`/api/workspaces/${encodeURIComponent(workspaceId)}/models`);
}

export function getWorkspaceRuntimeStatus(workspaceId, sessionId = "") {
  const query = sessionId ? `?sessionID=${encodeURIComponent(sessionId)}` : "";
  return request(`/api/workspaces/${encodeURIComponent(workspaceId)}/runtime-status${query}`);
}

export function getWorkspaceRuntimeModel(workspaceId, sessionId = "") {
  const query = sessionId ? `?sessionID=${encodeURIComponent(sessionId)}` : "";
  return request(`/api/workspaces/${encodeURIComponent(workspaceId)}/runtime-model${query}`);
}

export function getWorkspaceRuntimeQuota(workspaceId, model = "") {
  const query = model ? `?model=${encodeURIComponent(model)}` : "";
  return request(`/api/workspaces/${encodeURIComponent(workspaceId)}/runtime-quota${query}`);
}

export function getWorkspaceSettings(workspaceId) {
  return request(`/api/workspaces/${encodeURIComponent(workspaceId)}/settings`);
}

export function saveWorkspaceSettings(workspaceId, scope, settings, location = undefined) {
  return request(`/api/workspaces/${encodeURIComponent(workspaceId)}/settings`, {
    method: "PUT",
    body: JSON.stringify({ scope, location, settings }),
  });
}

export function getWorkspaceFile(workspaceId, path) {
  return request(`/api/workspaces/${encodeURIComponent(workspaceId)}/files/read?path=${encodeURIComponent(path)}`);
}

export function getWorkspaceFileAssetURL(workspaceId, path) {
  return `${apiBase()}/api/workspaces/${encodeURIComponent(workspaceId)}/files/raw?path=${encodeURIComponent(path)}`;
}

export function saveWorkspaceFile(workspaceId, path, content) {
  return request(`/api/workspaces/${encodeURIComponent(workspaceId)}/files/write?path=${encodeURIComponent(path)}`, {
    method: "PUT",
    body: JSON.stringify({ content }),
  });
}

export function createWorkspaceFile(workspaceId, path, kind = "file", content = "") {
  return request(`/api/workspaces/${encodeURIComponent(workspaceId)}/files/create`, {
    method: "POST",
    body: JSON.stringify({ path, kind, content }),
  });
}

export function renameWorkspaceFile(workspaceId, oldPath, newPath) {
  return request(`/api/workspaces/${encodeURIComponent(workspaceId)}/files/rename`, {
    method: "PATCH",
    body: JSON.stringify({ oldPath, newPath }),
  });
}

export function deleteWorkspaceFile(workspaceId, path) {
  return request(`/api/workspaces/${encodeURIComponent(workspaceId)}/files/delete`, {
    method: "DELETE",
    body: JSON.stringify({ path }),
  });
}

export { downloadWorkspaceFile, uploadWorkspaceFile };

export function getGitStatus(workspaceId) {
  return request(`/api/workspaces/${encodeURIComponent(workspaceId)}/git/status`);
}

export function getGitHistory(workspaceId, limit = 80) {
  return request(`/api/workspaces/${encodeURIComponent(workspaceId)}/git/history?limit=${encodeURIComponent(limit)}`);
}

export function getGitCommit(workspaceId, hash) {
  return request(`/api/workspaces/${encodeURIComponent(workspaceId)}/git/commit?hash=${encodeURIComponent(hash)}`);
}

export function runShellCommand(workspaceId, command) {
  return request(`/api/workspaces/${encodeURIComponent(workspaceId)}/shell`, {
    method: "POST",
    body: JSON.stringify({ command }),
  });
}

export function getSessionContextUsage(sessionId: string) {
  return request(`/api/sessions/${encodeURIComponent(sessionId)}/context-usage`);
}

export function setSessionModel(sessionId: string, provider: string, model: string, thinkingLevel = "") {
  return request(`/api/sessions/${encodeURIComponent(sessionId)}/model`, {
    method: "POST",
    body: JSON.stringify({ provider, model, ...(thinkingLevel ? { thinkingLevel } : {}) }),
  });
}

export function getSession(sessionId, options: { limit?: number; before?: string } = {}) {
  const params = new URLSearchParams();
  if (options.limit) params.set("limit", String(options.limit));
  if (options.before) params.set("before", options.before);
  const query = params.toString() ? `?${params}` : "";
  return request(`/api/sessions/${encodeURIComponent(sessionId)}${query}`);
}

export function renameSession(sessionId, title, clientRequestId = "") {
  return request(`/api/sessions/${encodeURIComponent(sessionId)}`, {
    method: "PATCH",
    body: JSON.stringify({ title, ...(clientRequestId ? { clientRequestId } : {}) }),
  });
}

export function deleteSession(sessionId) {
  return request(`/api/sessions/${encodeURIComponent(sessionId)}`, { method: "DELETE" });
}

export function cancelSession(sessionId) {
  return request(`/api/sessions/${encodeURIComponent(sessionId)}/cancel`, { method: "POST" });
}

export function compactSession(sessionId: string, clientRequestId = "") {
  return request(`/api/sessions/${encodeURIComponent(sessionId)}/compact`, {
    method: "POST",
    headers: clientRequestId ? { "X-Client-Request-ID": clientRequestId } : undefined,
  });
}

export function postPrompt(sessionId, text, attachments = [], clientRequestId = "") {
  return request(`/api/sessions/${encodeURIComponent(sessionId)}/prompt`, {
    method: "POST",
    body: JSON.stringify({
      text,
      attachments,
      ...(clientRequestId ? { clientRequestId } : {}),
    }),
  });
}

export function steerSession(sessionId, text, attachments = []) {
  return request(`/api/sessions/${encodeURIComponent(sessionId)}/steer`, {
    method: "POST",
    body: JSON.stringify({ text, attachments }),
  });
}

export function getSessionState(sessionId: string, options: { limit?: number; before?: string } = {}) {
  const params = new URLSearchParams();
  if (options.limit) params.set("limit", String(options.limit));
  if (options.before) params.set("before", options.before);
  const query = params.toString() ? `?${params}` : "";
  return request(`/api/sessions/${encodeURIComponent(sessionId)}/state${query}`);
}

export function getSessionItemContent(sessionId: string, itemId: string, version = 0) {
  const params = new URLSearchParams();
  if (version) params.set("version", String(version));
  const query = params.toString() ? `?${params}` : "";
  return request(`/api/sessions/${encodeURIComponent(sessionId)}/items/${encodeURIComponent(itemId)}/content${query}`);
}

const SESSION_STREAM_EVENT_TYPES = ["session.event", "stream.reset", "heartbeat"];

export function sessionEventsV2(
  sessionId: string,
  { after, onEvent, onOpen, onError }: any = {},
) {
  const params = new URLSearchParams();
  const cursor = normalizeEventCursor(after);
  if (cursor) params.set("after", cursor);
  const query = params.toString() ? `?${params}` : "";
  const source = new EventSource(
    `${apiBase()}/api/sessions/${encodeURIComponent(sessionId)}/events-v2${query}`,
  );
  source.onopen = () => onOpen?.();
  source.onerror = (event) => onError?.(event);
  for (const type of SESSION_STREAM_EVENT_TYPES) {
    source.addEventListener(type, (message: MessageEvent) => {
      try {
        const event = JSON.parse(String(message.data || "{}"));
        if (type !== "heartbeat") onEvent?.(event);
      } catch (error) {
        onError?.(error);
      }
    });
  }
  return source;
}

export function sessionEvents(sessionId: string, { onEvent, onOpen, onError, replay = true, after, snapshotAfter }: any = {}) {
  const params = new URLSearchParams();
  if (replay === "active") params.set("replay", "active");
  else if (!replay) params.set("replay", "false");
  const cursor = normalizeEventCursor(after);
  if (cursor && cursor !== "0") params.set("after", cursor);
  const snapshotCursor = normalizeEventCursor(snapshotAfter);
  if (replay === "active" && snapshotCursor && snapshotCursor !== "0") params.set("snapshot", snapshotCursor);
  const query = params.toString() ? `?${params}` : "";
  const source = new EventSource(
    `${apiBase()}/api/sessions/${encodeURIComponent(sessionId)}/events${query}`,
  );
  source.onopen = () => onOpen?.();
  source.onerror = (event) => onError?.(event);
  const types = [
    "session.message",
    "session.delta",
    "session.status",
    "session.renamed",
    "tool.started",
    "tool.output",
    "tool.finished",
    "workspace.files.changed",
    "error",
    "broker_gap",
    "heartbeat",
  ];
  for (const type of types) {
    source.addEventListener(type, (message) => {
      try {
        const event = JSON.parse(message.data);
        if (type === "broker_gap" || event.type === "broker_gap") {
          // EventSource inherits lastEventId for id-less SSE blocks; gaps must stay synthetic.
          event.id = 0;
        } else {
          const sseCursor = normalizeEventCursor(message.lastEventId);
          if (sseCursor) event.id = sseCursor;
        }
        onEvent?.(event);
      } catch (error) {
        onError?.(error);
      }
    });
  }
  return source;
}
