export type Account = { id: string; account: string; username: string; department: string; role: "admin" | "user"; disabled: boolean; workspaces: string[] };
export type ManagedWorkspace = { id: string; name: string; path: string };
export type AccountInput = { account?: string; username: string; department: string; password: string; workspaces: string[]; workspacePaths?: string[]; disabled?: boolean };

export class AccountError extends Error {
  constructor(public status: number, message: string) { super(message); }
}

export async function accountRequest<T>(path: string, method = "GET", body?: unknown): Promise<T> {
  const response = await fetch(path, {
    method, credentials: "same-origin", cache: "no-store",
    headers: { "Content-Type": "application/json" },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  const value = await response.json();
  if (!response.ok) throw new AccountError(response.status, value.error || "请求失败，请重试");
  return value;
}

export const currentAccount = () => accountRequest<{ user: Account }>("/api/account/me");
export const accountStatus = () => accountRequest<{ configured: boolean }>("/api/account/status");
export const login = (account: string, password: string) =>
  accountRequest<{ user: Account }>("/api/account/login", "POST", { account, password });
export const logout = () => accountRequest("/api/account/logout", "POST", {});
export const setup = (password: string, confirmation: string) =>
  accountRequest("/api/admin/password", "POST", { password, confirmation });
export const listUsers = (query = "", page = 1, pageSize = 50) => accountRequest<{ users: Account[]; total: number; page: number; pageSize: number }>(`/api/admin/users?q=${encodeURIComponent(query)}&page=${page}&pageSize=${pageSize}`);
export const listManagedWorkspaces = () => accountRequest<{ workspaces: ManagedWorkspace[] }>("/api/admin/workspaces");
export const listFolders = (path: string) => accountRequest<{ path: string; parent?: string; folders?: Array<{ name: string; path: string; displayPath?: string }> }>(`/api/system/folders?purpose=allowed-root&path=${encodeURIComponent(path)}`);
export const createUser = (value: AccountInput) => accountRequest("/api/admin/users", "POST", value);
export async function importUsers(file: File): Promise<{ imported: number }> {
  const body = new FormData();
  body.append("file", file);
  const response = await fetch("/api/admin/users/import", { method: "POST", credentials: "same-origin", cache: "no-store", body });
  const value = await response.json();
  if (!response.ok) throw new AccountError(response.status, value.error || "导入失败，请检查文件");
  return value;
}
export const updateUser = (name: string, value: AccountInput) =>
  accountRequest(`/api/admin/users/${encodeURIComponent(name)}`, "PATCH", value);
export const deleteUser = (name: string) => accountRequest(`/api/admin/users/${encodeURIComponent(name)}`, "DELETE");
export const batchAssignWorkspaces = (usernames: string[], workspaces: string[]) => accountRequest("/api/admin/users/batch-workspaces", "POST", { usernames, workspaces });
