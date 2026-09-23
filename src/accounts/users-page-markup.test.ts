// @vitest-environment node
import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";

const usersPage = readFileSync(new URL("../pages/users.astro", import.meta.url), "utf8");
const userManagement = readFileSync(new URL("../components/UserManagement.astro", import.meta.url), "utf8");
const appTemplate = readFileSync(new URL("../App.astro", import.meta.url), "utf8");
const accountLayout = readFileSync(new URL("../components/AccountLayout.astro", import.meta.url), "utf8");
const accountStyles = readFileSync(new URL("./pages.css", import.meta.url), "utf8");

describe("users page layout", () => {
  it("reuses the user management component on the standalone page", () => {
    expect(usersPage).toContain('import UserManagement from "../components/UserManagement.astro";');
    expect(usersPage).toContain("<UserManagement />");
    expect(accountLayout).toContain("data-account-error");
  });

  it("embeds user management in a modal on the workspace page", () => {
    expect(appTemplate).toContain('data-user-management-dialog');
    expect(appTemplate).toContain('data-account-admin');
    expect(appTemplate).toContain('<UserManagement embedded />');
  });

  it("places import status before the table controls", () => {
    expect(userManagement.indexOf('data-users-status')).toBeLessThan(userManagement.indexOf('class="account-toolbar"'));
  });

  it("defaults to ten users per page", () => {
    expect(userManagement).toContain('<option value="10" selected>10 条</option>');
  });

  it("provides a batch workspace authorization dialog", () => {
    expect(userManagement).toContain('data-batch-open');
    expect(userManagement).toContain('data-batch-dialog');
    expect(userManagement).toContain('data-batch-open-directory');
    expect(userManagement).toContain('data-batch-status');
  });

  it("uses one background for the full account document", () => {
    expect(accountLayout).toContain('<html lang="zh-CN" class="account-document">');
    expect(accountStyles).toContain("html.account-document, body.account-page { background: var(--bg-1); }");
  });

  it("keeps the user count heading separate from its supporting description", () => {
    expect(accountStyles).toContain(".account-section-hint { margin: 4px 0 0; font-size: 12px; }");
  });
});
