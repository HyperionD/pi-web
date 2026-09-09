import { afterEach, describe, expect, it, vi } from "vitest";
import {
  AccountError, accountRequest, accountStatus, createUser, currentAccount, deleteUser, listUsers,
  listManagedWorkspaces, login, logout, setup, updateUser,
} from "./api";

const fetchMock = vi.fn();
afterEach(() => { vi.unstubAllGlobals(); fetchMock.mockReset(); });

describe("account API", () => {
  it("uses same-origin cookies without persisting credentials", async () => {
    vi.stubGlobal("fetch", fetchMock.mockResolvedValue({ ok: true, json: async () => ({ ok: true }) }));
    const input = { account: "alice", username: "Alice", department: "研发部", password: "password-123", workspaces: ["one"] };
    await currentAccount(); await accountStatus(); await login("alice", "password-123"); await logout();
    await setup("password-123", "password-123"); await listUsers(); await listManagedWorkspaces();
    await createUser(input); await updateUser("a b", input); await deleteUser("a b");
    expect(fetchMock.mock.calls.map(([path]) => path)).toEqual([
      "/api/account/me", "/api/account/status", "/api/account/login", "/api/account/logout", "/api/admin/password",
      "/api/admin/users?q=&page=1&pageSize=50", "/api/admin/workspaces", "/api/admin/users", "/api/admin/users/a%20b", "/api/admin/users/a%20b",
    ]);
    expect(fetchMock.mock.calls[2][1]).toMatchObject({ credentials: "same-origin", cache: "no-store", method: "POST",
      body: JSON.stringify({ account: "alice", password: "password-123" }) });
    expect(fetchMock.mock.calls[0][1].body).toBeUndefined();
    expect(JSON.parse(fetchMock.mock.calls[7][1].body)).toMatchObject({ department: "研发部" });
    expect(JSON.parse(fetchMock.mock.calls[8][1].body)).toMatchObject({ department: "研发部" });
    expect(fetchMock.mock.calls[9][1].method).toBe("DELETE");
  });
  it("preserves status and server errors", async () => {
    vi.stubGlobal("fetch", fetchMock.mockResolvedValue({ ok: false, status: 401, json: async () => ({ error: "expired" }) }));
    await expect(accountRequest("/api/account/me")).rejects.toMatchObject({ status: 401, message: "expired" });
    fetchMock.mockResolvedValue({ ok: false, status: 503, json: async () => ({}) });
    await expect(currentAccount()).rejects.toEqual(new AccountError(503, "请求失败，请重试"));
  });
  it("does not hide network or malformed-response errors", async () => {
    vi.stubGlobal("fetch", fetchMock.mockRejectedValue(new Error("offline")));
    await expect(currentAccount()).rejects.toThrow("offline");
    fetchMock.mockResolvedValue({ json: async () => { throw new Error("invalid JSON"); } });
    await expect(currentAccount()).rejects.toThrow("invalid JSON");
  });
});
