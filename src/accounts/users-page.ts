import { batchAssignWorkspaces, createUser, deleteUser, listFolders, listUsers, listWorkspaces, updateUser, type Account, type ManagedWorkspace } from "./api";
import { requireAccount, showAccountError } from "./browser";

const form = document.querySelector<HTMLFormElement>("[data-user-form]")!;
const fields = document.querySelector<HTMLFieldSetElement>("[data-user-fields]")!;
const errorMessage = document.querySelector<HTMLElement>("[data-account-error]")!;
const status = document.querySelector<HTMLElement>("[data-user-status]")!;
const removeButton = document.querySelector<HTMLButtonElement>("[data-user-delete]")!;
const dialog = document.querySelector<HTMLDialogElement>("[data-delete-dialog]")!;
let editing: Account | null = null;
let workspaces: ManagedWorkspace[] = [];
let userPage = 1;
let userTotal = 0;
let searchTimer: number | undefined;
const selectedUsers = new Set<string>();

function selectUser(user: Account | null) {
  editing = user;
  form.reset();
  document.querySelector<HTMLElement>("[data-selected-directory]")!.textContent = "未选择新目录。点击“添加目录”后从服务器目录树中选择。";
  errorMessage.textContent = "";
  status.textContent = "";
  const username = form.elements.namedItem("username") as HTMLInputElement;
  username.value = user?.username || "";
  username.readOnly = !!user;
  (form.elements.namedItem("password") as HTMLInputElement).required = !user;
  (form.elements.namedItem("disabled") as HTMLInputElement).checked = user?.disabled || false;
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

function syncBatchControls() {
  document.querySelector<HTMLElement>("[data-account-batch]")!.hidden = selectedUsers.size === 0;
  document.querySelector("[data-batch-count]")!.textContent = `${selectedUsers.size} 个用户`;
}

async function refreshUsers() {
  const query = document.querySelector<HTMLInputElement>("[data-user-search]")?.value.trim() || "";
  const { users, total, page } = await listUsers(query, userPage, 50);
  userTotal = total; userPage = page;
  const container = document.querySelector("[data-users-list]")!;
  container.replaceChildren();
  document.querySelector<HTMLElement>("[data-users-empty]")!.hidden = users.length > 0;
  document.querySelector("[data-users-count]")!.textContent = String(total);
  document.querySelector("[data-users-page]")!.textContent = `第 ${page} 页 / 共 ${Math.max(1, Math.ceil(total / 50))} 页`;
  (document.querySelector("[data-users-prev]") as HTMLButtonElement).disabled = page <= 1;
  (document.querySelector("[data-users-next]") as HTMLButtonElement).disabled = page * 50 >= total;
  for (const user of users) {
    const item = document.createElement("li");
    const checkbox = document.createElement("input"); checkbox.type = "checkbox"; checkbox.checked = selectedUsers.has(user.username); checkbox.setAttribute("aria-label", `选择 ${user.username}`);
    checkbox.addEventListener("change", () => { if (checkbox.checked) selectedUsers.add(user.username); else selectedUsers.delete(user.username); syncBatchControls(); });
    const button = document.createElement("button"); button.type = "button"; button.textContent = `${user.username} · ${user.workspaces.length} 个工作区${user.disabled ? " · 已禁用" : ""}`; button.addEventListener("click", () => selectUser(user));
    item.append(checkbox, button);
    container.append(item);
  }
}

form.addEventListener("submit", async (event) => {
  event.preventDefault();
  const values = new FormData(form);
  const value = { username: String(values.get("username")), password: String(values.get("password")),
    workspaces: values.getAll("workspaces").map(String), workspacePaths: [String(values.get("workspacePath") || "").trim()].filter(Boolean),
    disabled: values.get("disabled") === "on" };
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
document.querySelector("[data-users-prev]")!.addEventListener("click", () => { if (userPage > 1) { userPage--; void refreshUsers(); } });
document.querySelector("[data-users-next]")!.addEventListener("click", () => { if (userPage * 50 < userTotal) { userPage++; void refreshUsers(); } });
document.querySelector<HTMLInputElement>("[data-user-search]")!.addEventListener("input", () => { window.clearTimeout(searchTimer); searchTimer = window.setTimeout(() => { userPage = 1; void refreshUsers(); }, 250); });
document.querySelector("[data-batch-assign]")!.addEventListener("click", async () => {
  const select = document.querySelector<HTMLSelectElement>("[data-batch-workspaces]")!;
  const workspaces = [...select.selectedOptions].map((option) => option.value);
  if (!workspaces.length) { status.textContent = "请选择至少一个工作区。"; return; }
  try { await batchAssignWorkspaces([...selectedUsers], workspaces); selectedUsers.clear(); await refreshUsers(); syncBatchControls(); status.textContent = "批量授权已保存。"; }
  catch (error) { showAccountError(error, errorMessage); }
});
const directoryDialog = document.querySelector<HTMLDialogElement>("[data-directory-dialog]")!;
const directoryCurrent = document.querySelector<HTMLElement>("[data-directory-current]")!;
const directoryList = document.querySelector<HTMLElement>("[data-directory-list]")!;
let selectedDirectory = "/";
async function loadDirectory(path: string) {
  directoryCurrent.textContent = "加载目录中…";
  directoryList.replaceChildren();
  try {
    const result = await listFolders(path);
    selectedDirectory = result.path;
    directoryCurrent.textContent = result.path;
    const folders = result.folders || [];
    if (!folders.length) directoryList.textContent = "此目录没有可浏览的子目录。";
    for (const folder of folders) {
      const button = document.createElement("button");
      button.type = "button";
      button.setAttribute("role", "treeitem");
      button.textContent = folder.name;
      button.title = folder.displayPath || folder.path;
      button.addEventListener("click", () => void loadDirectory(folder.path));
      directoryList.append(button);
    }
  } catch (error) { directoryCurrent.textContent = "无法加载目录"; showAccountError(error, errorMessage); }
}
document.querySelector("[data-open-directory]")!.addEventListener("click", () => { directoryDialog.showModal(); void loadDirectory("/"); });
document.querySelector("[data-directory-up]")!.addEventListener("click", () => void loadDirectory(selectedDirectory.replace(/\/[^/]*$/, "") || "/"));
directoryDialog.addEventListener("close", () => {
  if (directoryDialog.returnValue !== "confirm") return;
  (form.elements.namedItem("workspacePath") as HTMLInputElement).value = selectedDirectory;
  document.querySelector<HTMLElement>("[data-selected-directory]")!.textContent = `已选择：${selectedDirectory}`;
});
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
  const batchSelect = document.querySelector<HTMLSelectElement>("[data-batch-workspaces]")!;
  batchSelect.replaceChildren(...workspaces.map((workspace) => new Option(`${workspace.name} · ${workspace.path}`, workspace.id)));
  await refreshUsers();
  selectUser(null); syncBatchControls();
  document.querySelector<HTMLElement>("[data-users-content]")!.hidden = false;
}
void initialize().catch((error) => showAccountError(error, errorMessage));
