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
    const managementDialog = document.querySelector<HTMLDialogElement>("[data-user-management-dialog]");
    if (managementLink) managementLink.hidden = user.role !== "admin";
    if (user.role === "admin" && managementLink && managementDialog) {
      let managementReturnFocus: HTMLElement | null = null;
      let managementInit: Promise<void> | undefined;
      const initialManagementMarkup = managementDialog.innerHTML;
      const ensureManagement = () => {
        if (!managementInit) {
          managementInit = import("./users-page")
            .then(({ initUsersPage }) => initUsersPage(managementDialog, { skipAuth: true }))
            .catch((error) => {
              managementInit = undefined;
              throw error;
            });
        }
        return managementInit;
      };
      managementLink.addEventListener("click", () => {
        managementReturnFocus = document.activeElement instanceof HTMLElement ? document.activeElement : managementLink;
        if (!managementDialog.open) managementDialog.showModal();
        void ensureManagement().then(() => managementDialog.querySelector<HTMLInputElement>("[data-user-search]")?.focus()).catch((error) => {
          managementDialog.innerHTML = initialManagementMarkup;
          managementDialog.querySelector<HTMLElement>("[data-users-content]")?.removeAttribute("hidden");
          showAccountError(error, managementDialog.querySelector<HTMLElement>("[data-account-error]") || managementDialog);
        });
      });
      managementDialog.addEventListener("click", (event) => {
        if (event.target instanceof Element && event.target.closest("[data-user-management-close]")) {
          managementDialog.close("cancel");
        }
      });
      managementDialog.addEventListener("close", () => managementReturnFocus?.focus());
    }
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
