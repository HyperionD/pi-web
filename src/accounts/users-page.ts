import { createUser, deleteUser, listUsers, listWorkspaces, updateUser, type Account, type ManagedWorkspace } from "./api";
import { requireAccount, showAccountError } from "./browser";

const form = document.querySelector<HTMLFormElement>("[data-user-form]")!;
const fields = document.querySelector<HTMLFieldSetElement>("[data-user-fields]")!;
const errorMessage = document.querySelector<HTMLElement>("[data-account-error]")!;
const status = document.querySelector<HTMLElement>("[data-user-status]")!;
const removeButton = document.querySelector<HTMLButtonElement>("[data-user-delete]")!;
const dialog = document.querySelector<HTMLDialogElement>("[data-delete-dialog]")!;
let editing: Account | null = null;
let workspaces: ManagedWorkspace[] = [];

function selectUser(user: Account | null) {
  editing = user;
  form.reset();
  errorMessage.textContent = "";
  status.textContent = "";
  const username = form.elements.namedItem("username") as HTMLInputElement;
  username.value = user?.username || "";
  username.readOnly = !!user;
  (form.elements.namedItem("password") as HTMLInputElement).required = !user;
  document.querySelector("[data-user-form-title]")!.textContent = user ? `编辑 ${user.username}` : "创建用户";
  document.querySelector("[data-password-label]")!.textContent = user ? "重置密码（留空不修改）" : "初始密码";
  removeButton.hidden = !user;
  const container = document.querySelector("[data-user-workspaces]")!;
  container.replaceChildren();
  for (const workspace of workspaces) {
    const label = document.createElement("label");
    const checkbox = document.createElement("input");
    checkbox.type = "checkbox";
    checkbox.name = "workspaces";
    checkbox.value = workspace.id;
    checkbox.checked = user?.workspaces.includes(workspace.id) || false;
    const text = document.createElement("span");
    text.textContent = `${workspace.name} · ${workspace.path}`;
    label.append(checkbox, text);
    container.append(label);
  }
  if (!workspaces.length) container.textContent = "暂无可分配的工作区。";
}

async function refreshUsers() {
  const { users } = await listUsers();
  const container = document.querySelector("[data-users-list]")!;
  container.replaceChildren();
  document.querySelector<HTMLElement>("[data-users-empty]")!.hidden = users.length > 0;
  for (const user of users) {
    const item = document.createElement("li");
    const button = document.createElement("button");
    button.type = "button";
    button.textContent = `${user.username} · ${user.workspaces.length} 个工作区`;
    button.addEventListener("click", () => selectUser(user));
    item.append(button);
    container.append(item);
  }
}

form.addEventListener("submit", async (event) => {
  event.preventDefault();
  const values = new FormData(form);
  const value = { username: String(values.get("username")), password: String(values.get("password")),
    workspaces: values.getAll("workspaces").map(String) };
  fields.disabled = true;
  errorMessage.textContent = "";
  try {
    if (editing) await updateUser(editing.username, value);
    else await createUser(value);
    await refreshUsers();
    selectUser(null);
    status.textContent = "用户已保存。";
  } catch (error) { showAccountError(error, errorMessage); }
  finally { fields.disabled = false; }
});

document.querySelector("[data-user-new]")!.addEventListener("click", () => selectUser(null));
removeButton.addEventListener("click", () => {
  document.querySelector("[data-delete-name]")!.textContent = editing?.username || "";
  dialog.returnValue = "cancel";
  dialog.showModal();
});
dialog.addEventListener("close", async () => {
  if (dialog.returnValue !== "confirm" || !editing) return;
  fields.disabled = true;
  try {
    await deleteUser(editing.username);
    await refreshUsers();
    selectUser(null);
    status.textContent = "用户已删除。";
  } catch (error) { showAccountError(error, errorMessage); }
  finally { fields.disabled = false; }
});

async function initialize() {
  if (!await requireAccount(true)) return;
  ({ workspaces } = await listWorkspaces());
  await refreshUsers();
  selectUser(null);
  document.querySelector<HTMLElement>("[data-users-content]")!.hidden = false;
}
void initialize().catch((error) => showAccountError(error, errorMessage));
