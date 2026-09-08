import { accountStatus, login, setup } from "./api";
import { showAccountError, syncAccountCache } from "./browser";

const form = document.querySelector<HTMLFormElement>("[data-login-form]")!;
const fields = document.querySelector<HTMLFieldSetElement>("[data-login-fields]")!;
const errorMessage = document.querySelector<HTMLElement>("[data-account-error]")!;
let configured = true;

async function initialize() {
  ({ configured } = await accountStatus());
  if (!configured) {
    document.querySelector("[data-login-title]")!.textContent = "设置管理员账号";
    document.querySelector("[data-login-hint]")!.textContent = "首次部署：设置 admin 密码后即可创建用户并分配工作区。";
    document.querySelector("[data-login-submit]")!.textContent = "设置并登录";
    document.querySelector<HTMLElement>("[data-confirmation]")!.hidden = false;
    const username = form.elements.namedItem("username") as HTMLInputElement;
    username.value = "admin";
    username.readOnly = true;
    (form.elements.namedItem("password") as HTMLInputElement).autocomplete = "new-password";
    (form.elements.namedItem("confirmation") as HTMLInputElement).required = true;
  }
  fields.disabled = false;
}

form.addEventListener("submit", async (event) => {
  event.preventDefault();
  const values = new FormData(form);
  const username = String(values.get("username"));
  const password = String(values.get("password"));
  fields.disabled = true;
  errorMessage.textContent = "";
  try {
    if (!configured) { await setup(password, String(values.get("confirmation"))); configured = true; }
    const { user } = await login(username, password);
    syncAccountCache(user.id);
    form.reset();
    location.replace("/");
  } catch (error) { showAccountError(error, errorMessage); }
  finally { fields.disabled = false; }
});

void initialize().catch((error) => showAccountError(error, errorMessage));
