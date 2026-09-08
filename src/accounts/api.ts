export type Account = { id: string; username: string; role: "admin" | "user"; workspaces: string[] };
export type ManagedWorkspace = { id: string; name: string; path: string };
export type AccountInput = { username?: string; password: string; workspaces: string[] };

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
export const login = (username: string, password: string) =>
  accountRequest<{ user: Account }>("/api/account/login", "POST", { username, password });
export const logout = () => accountRequest("/api/account/logout", "POST", {});
export const setup = (password: string, confirmation: string) =>
  accountRequest("/api/admin/password", "POST", { password, confirmation });
export const listUsers = () => accountRequest<{ users: Account[] }>("/api/admin/users");
export const listWorkspaces = () => accountRequest<{ workspaces: ManagedWorkspace[] }>("/api/workspaces");
export const createUser = (value: AccountInput) => accountRequest("/api/admin/users", "POST", value);
export const updateUser = (name: string, value: AccountInput) =>
  accountRequest(`/api/admin/users/${encodeURIComponent(name)}`, "PATCH", value);
export const deleteUser = (name: string) => accountRequest(`/api/admin/users/${encodeURIComponent(name)}`, "DELETE");
