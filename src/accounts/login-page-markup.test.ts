// @vitest-environment node
import { readFileSync } from "node:fs";

const loginPage = readFileSync(new URL("../pages/login.astro", import.meta.url), "utf8");

describe("login page markup", () => {
  it("hides the account header and omits administrative guidance", () => {
    expect(loginPage).toContain('<AccountLayout title="登录" hideHeader>');
    expect(loginPage).not.toContain("使用管理员分配的账号登录。会话仅对本人可见。");
    expect(loginPage).not.toContain("工作区由管理员分配，普通用户不能自行添加。仅提供应用权限隔离，不提供终端沙箱。");
  });
});
