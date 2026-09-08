import { AccountError, currentAccount } from "./api";
import { requireAccount, showAccountError, signOut } from "./browser";
import "./app-permissions.css";

async function start() {
  if (document.body.dataset.userManagement !== "true") { await import("../pi-app/index"); return; }
  const user = await requireAccount();
  if (!user) return;
  document.body.dataset.accountRole = user.role;
  if (user.role === "user") {
    const empty = document.querySelector("[data-empty-no-workspace]");
    if (empty) empty.textContent = "尚未分配可用工作区，请联系管理员。";
  }
  const nav = document.querySelector<HTMLElement>("[data-account-nav]");
  if (nav) {
    nav.hidden = false;
    nav.querySelector("[data-account-name]")!.textContent = user.username;
    const managementLink = document.querySelector<HTMLElement>("[data-account-admin]");
    if (managementLink) managementLink.hidden = user.role !== "admin";
    nav.querySelector("[data-account-logout]")!.addEventListener("click", () => {
      void signOut().catch((error) => showAccountError(error, nav));
    });
  }
  document.body.dataset.accountReady = "true";
  await import("../pi-app/index");
  const check = async () => {
    try {
      const { user: current } = await currentAccount();
      if (current.id !== user.id) location.replace("/login");
    } catch (error) {
      if (error instanceof AccountError && error.status === 401) {
        document.querySelector("pi-app")?.remove();
        location.replace("/login");
      } else if (nav) showAccountError(error, nav);
    }
  };
  window.addEventListener("focus", () => void check());
  window.setInterval(() => void check(), 15000);
}

void start().catch((error) => {
  const message = document.createElement("p");
  message.setAttribute("role", "alert");
  showAccountError(error, message);
  document.body.replaceChildren(message);
});
