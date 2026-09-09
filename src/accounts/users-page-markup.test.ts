// @vitest-environment node
import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";

const usersPage = readFileSync(new URL("../pages/users.astro", import.meta.url), "utf8");
const accountLayout = readFileSync(new URL("../components/AccountLayout.astro", import.meta.url), "utf8");
const accountStyles = readFileSync(new URL("./pages.css", import.meta.url), "utf8");

describe("users page layout", () => {
  it("places import status before the table controls", () => {
    expect(usersPage.indexOf('data-users-status')).toBeLessThan(usersPage.indexOf('class="account-toolbar"'));
  });

  it("defaults to ten users per page", () => {
    expect(usersPage).toContain('<option value="10" selected>10 条</option>');
  });

  it("provides a batch workspace authorization dialog", () => {
    expect(usersPage).toContain('data-batch-open');
    expect(usersPage).toContain('data-batch-dialog');
    expect(usersPage).toContain('data-batch-open-directory');
    expect(usersPage).toContain('data-batch-status');
  });

  it("uses one background for the full account document", () => {
    expect(accountLayout).toContain('<html lang="zh-CN" class="account-document">');
    expect(accountStyles).toContain("html.account-document, body.account-page { background: var(--bg-1); }");
  });
});
