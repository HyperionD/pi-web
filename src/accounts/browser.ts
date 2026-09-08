import { AccountError, currentAccount, logout, type Account } from "./api";

const accountKey = "pi.account.identity";

export function syncAccountCache(userID: string) {
  if (localStorage.getItem(accountKey) === userID) return;
  for (const key of Object.keys(localStorage)) {
    if ((key.startsWith("pi.") || key.startsWith("piweb:")) && key !== accountKey) localStorage.removeItem(key);
  }
  localStorage.setItem(accountKey, userID);
}

export async function requireAccount(admin = false): Promise<Account | null> {
  try {
    const { user } = await currentAccount();
    syncAccountCache(user.id);
    if (admin && user.role !== "admin") { location.replace("/"); return null; }
    window.addEventListener("storage", (event) => {
      if (event.key === accountKey) { document.body.replaceChildren(); location.replace("/login"); }
    });
    return user;
  } catch (error) {
    if (error instanceof AccountError && error.status === 401) { location.replace("/login"); return null; }
    throw error;
  }
}

export async function signOut() {
  await logout();
  syncAccountCache("");
  location.replace("/login");
}

export function showAccountError(error: unknown, element: HTMLElement) {
  element.textContent = error instanceof Error ? error.message : "请求失败，请重试";
}
