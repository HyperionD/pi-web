import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
  requireAccount: vi.fn(),
  showAccountError: vi.fn(),
  signOut: vi.fn(),
  currentAccount: vi.fn(),
  initUsersPage: vi.fn(),
  pageAction: vi.fn(),
}));

vi.mock("./api", () => ({
  currentAccount: mocks.currentAccount,
  AccountError: class AccountError extends Error {
    constructor(public status: number, message: string) { super(message); }
  },
}));
vi.mock("./browser", () => ({
  requireAccount: mocks.requireAccount,
  showAccountError: mocks.showAccountError,
  signOut: mocks.signOut,
}));
vi.mock("./users-page", () => ({ initUsersPage: mocks.initUsersPage }));
vi.mock("../pi-app/index", () => ({}));

beforeEach(() => {
  vi.resetModules();
  vi.clearAllMocks();
  document.body.dataset.userManagement = "true";
  document.body.innerHTML = `
    <nav data-account-nav><span data-account-name></span><button data-account-logout></button></nav>
    <button data-account-admin></button>
    <dialog data-user-management-dialog>
      <input data-user-search />
      <div data-users-content hidden><button data-user-new></button></div>
      <p data-account-error></p>
      <button data-user-management-close></button>
    </dialog>
    <pi-app></pi-app>
  `;
  const dialog = document.querySelector<HTMLDialogElement>("[data-user-management-dialog]");
  dialog.showModal = vi.fn(() => dialog.setAttribute("open", ""));
  dialog.close = vi.fn(() => dialog.removeAttribute("open"));
  vi.spyOn(window, "setInterval").mockReturnValue({} as ReturnType<typeof window.setInterval>);
  mocks.requireAccount.mockResolvedValue({ id: "admin-id", username: "Admin", role: "admin" });
});

afterEach(() => {
  vi.restoreAllMocks();
  document.body.innerHTML = "";
});

describe("embedded user-management initialization", () => {
  it("retries initialization after a transient failure", async () => {
    mocks.initUsersPage
      .mockImplementationOnce(async (scope) => {
        scope.querySelector("[data-user-new]").addEventListener("click", () => mocks.pageAction());
        throw new Error("temporary failure");
      })
      .mockImplementationOnce(async (scope) => {
        scope.querySelector("[data-user-new]").addEventListener("click", () => mocks.pageAction());
      });
    await import("./app-entry");
    await vi.waitFor(() => expect(document.body.dataset.accountReady).toBe("true"));

    const managementButton = document.querySelector<HTMLButtonElement>("[data-account-admin]");
    managementButton.click();
    await vi.waitFor(() => expect(mocks.showAccountError).toHaveBeenCalledOnce());
    managementButton.click();
    await vi.waitFor(() => expect(mocks.initUsersPage).toHaveBeenCalledTimes(2));
    document.querySelector("[data-user-new]").click();
    expect(mocks.pageAction).toHaveBeenCalledOnce();
    const dialog = document.querySelector<HTMLDialogElement>("[data-user-management-dialog]");
    dialog.querySelector("[data-user-management-close]").click();
    expect(dialog.close).toHaveBeenCalledWith("cancel");
  });
});
