import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { AccountError, currentAccount, logout } from "./api";
import { requireAccount, showAccountError, signOut, syncAccountCache } from "./browser";

vi.mock("./api", async (load) => ({ ...await load<typeof import("./api")>(),
  currentAccount: vi.fn(), logout: vi.fn() }));
const user = { id: "alice-id", account: "alice", username: "Alice", role: "user" as const, disabled: false, workspaces: [] };
const replace = vi.fn();
beforeEach(() => { localStorage.clear(); vi.stubGlobal("location", { replace }); });
afterEach(() => { vi.restoreAllMocks(); vi.unstubAllGlobals(); vi.clearAllMocks(); });

describe("browser identity isolation", () => {
  it("clears old drafts, sessions and unread caches without removing unrelated storage", () => {
    localStorage.setItem("pi.promptDraft", "private");
    localStorage.setItem("piweb:last-session-prompts", "private");
    localStorage.setItem("unrelated", "keep");
    syncAccountCache(user.id);
    expect(localStorage.getItem("pi.promptDraft")).toBeNull();
    expect(localStorage.getItem("piweb:last-session-prompts")).toBeNull();
    expect(localStorage.getItem("unrelated")).toBe("keep");
    localStorage.setItem("pi.promptDraft", "current");
    syncAccountCache(user.id);
    expect(localStorage.getItem("pi.promptDraft")).toBe("current");
  });
  it("loads the account and redirects stale tabs on identity changes only", async () => {
    vi.mocked(currentAccount).mockResolvedValue({ user });
    const listen = vi.spyOn(window, "addEventListener");
    expect(await requireAccount()).toEqual(user);
    const callback = listen.mock.calls.find(([type]) => type === "storage")![1] as EventListener;
    callback(new StorageEvent("storage", { key: "other" }));
    expect(replace).not.toHaveBeenCalled();
    callback(new StorageEvent("storage", { key: "pi.account.identity" }));
    expect(replace).toHaveBeenCalledWith("/login");
  });
  it("requires administrator role on management pages", async () => {
    vi.mocked(currentAccount).mockResolvedValue({ user });
    expect(await requireAccount(true)).toBeNull();
    expect(replace).toHaveBeenCalledWith("/");
    vi.mocked(currentAccount).mockResolvedValue({ user: { ...user, role: "admin" } });
    expect(await requireAccount(true)).toMatchObject({ role: "admin" });
  });
  it("redirects unauthorized accounts but surfaces operational failures", async () => {
    vi.mocked(currentAccount).mockRejectedValue(new AccountError(401, "expired"));
    expect(await requireAccount()).toBeNull();
    expect(replace).toHaveBeenCalledWith("/login");
    vi.mocked(currentAccount).mockRejectedValue(new Error("offline"));
    await expect(requireAccount()).rejects.toThrow("offline");
  });
  it("revokes the cookie before clearing state on logout", async () => {
    vi.mocked(logout).mockResolvedValue({});
    syncAccountCache(user.id);
    localStorage.setItem("pi.activeSession", "private");
    await signOut();
    expect(logout).toHaveBeenCalled();
    expect(localStorage.getItem("pi.activeSession")).toBeNull();
    expect(replace).toHaveBeenCalledWith("/login");
    vi.mocked(logout).mockRejectedValue(new Error("offline"));
    await expect(signOut()).rejects.toThrow("offline");
  });
  it("renders error text without interpreting HTML", () => {
    const element = document.createElement("p");
    showAccountError(new Error("<img>"), element);
    expect(element.innerHTML).toBe("&lt;img&gt;");
    showAccountError(null, element);
    expect(element.textContent).toBe("请求失败，请重试");
  });
});
