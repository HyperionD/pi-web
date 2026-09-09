import { batchAssignWorkspaces, createUser, deleteUser, importUsers, listFolders, listManagedWorkspaces, listUsers, updateUser, type Account, type ManagedWorkspace } from "./api";
import { requireAccount, showAccountError, showImportError, showOperationSuccess } from "./browser";

const form = document.querySelector<HTMLFormElement>("[data-user-form]")!;
const fields = document.querySelector<HTMLFieldSetElement>("[data-user-fields]")!;
const errorMessage = document.querySelector<HTMLElement>("[data-account-error]")!;
const status = document.querySelector<HTMLElement>("[data-user-status]")!;
const usersStatus = document.querySelector<HTMLElement>("[data-users-status]")!;
const batchDialog = document.querySelector<HTMLDialogElement>("[data-batch-dialog]")!;
const batchForm = document.querySelector<HTMLFormElement>("[data-batch-form]")!;
const batchStatus = document.querySelector<HTMLElement>("[data-batch-status]")!;
const batchSubmit = document.querySelector<HTMLButtonElement>("[data-batch-submit]")!;
const batchTargetCount = document.querySelector<HTMLElement>("[data-batch-target-count]")!;
const batchSelectedPath = document.querySelector<HTMLElement>("[data-batch-selected-path]")!;
const removeButton = document.querySelector<HTMLButtonElement>("[data-user-delete]")!;
const dialog = document.querySelector<HTMLDialogElement>("[data-delete-dialog]")!;
let editing: Account | null = null;
let workspaces: ManagedWorkspace[] = [];
let userPage = 1;
let userTotal = 0;
let userPageSize = 10;
let searchTimer: number | undefined;
let batchWorkspacePath = "";
const selectedUsers = new Set<string>();

function selectUser(user: Account | null) {
  editing = user;
  form.reset();
  document.querySelector<HTMLElement>("[data-selected-directory]")!.textContent = "未选择新目录。点击“添加目录”后从服务器目录树中选择。";
  errorMessage.textContent = "";
  status.textContent = "";
  const account = form.elements.namedItem("account") as HTMLInputElement;
  account.value = user?.account || "";
  account.readOnly = !!user;
  const username = form.elements.namedItem("username") as HTMLInputElement;
  username.value = user?.username || "";
  const department = form.elements.namedItem("department") as HTMLInputElement;
  department.value = user?.department || "";
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

async function refreshUsersSafely() {
  try { await refreshUsers(); } catch (error) { showAccountError(error, errorMessage); }
}

async function refreshWorkspaceOptions() {
  ({ workspaces } = await listManagedWorkspaces());
}

async function refreshUsers() {
  const query = document.querySelector<HTMLInputElement>("[data-user-search]")?.value.trim() || "";
  const { users, total, page, pageSize } = await listUsers(query, userPage, userPageSize);
  userTotal = total; userPage = page; userPageSize = pageSize;
  const container = document.querySelector<HTMLTableSectionElement>("[data-users-list]")!;
  container.replaceChildren();
  document.querySelector<HTMLElement>("[data-users-empty]")!.hidden = users.length > 0;
  document.querySelector("[data-users-count]")!.textContent = String(total);
  document.querySelector("[data-users-page]")!.textContent = `第 ${page} 页 / 共 ${Math.max(1, Math.ceil(total / userPageSize))} 页`;
  (document.querySelector("[data-users-prev]") as HTMLButtonElement).disabled = page <= 1;
  (document.querySelector("[data-users-next]") as HTMLButtonElement).disabled = page * userPageSize >= total;
  for (const user of users) {
    const row = document.createElement("tr");
    const selectCell = document.createElement("td");
    const checkbox = document.createElement("input"); checkbox.type = "checkbox"; checkbox.checked = selectedUsers.has(user.account); checkbox.setAttribute("aria-label", `选择 ${user.username}`);
    checkbox.addEventListener("change", () => { if (checkbox.checked) selectedUsers.add(user.account); else selectedUsers.delete(user.account); syncBatchControls(); });
    selectCell.append(checkbox);
    const accountCell = document.createElement("td"); const accountCode = document.createElement("code"); accountCode.textContent = user.account; accountCell.append(accountCode);
    const nameCell = document.createElement("td"); nameCell.textContent = user.username; nameCell.className = "account-user-name";
    const departmentCell = document.createElement("td"); departmentCell.textContent = user.department || "—";
    const statusCell = document.createElement("td"); const badge = document.createElement("span"); badge.textContent = user.disabled ? "已禁用" : "正常"; badge.className = `account-user-status${user.disabled ? " is-disabled" : ""}`; statusCell.append(badge);
    const workspaceCell = document.createElement("td"); workspaceCell.textContent = `${user.workspaces.length} 个工作区`;
    const actionCell = document.createElement("td"); const editButton = document.createElement("button"); editButton.type = "button"; editButton.className = "account-row-action"; editButton.textContent = "编辑"; editButton.addEventListener("click", () => openUserDialog(user)); actionCell.append(editButton);
    row.append(selectCell, accountCell, nameCell, departmentCell, statusCell, workspaceCell, actionCell); container.append(row);
  }
}

form.addEventListener("submit", async (event) => {
  event.preventDefault();
  const values = new FormData(form);
  const value = { account: String(values.get("account")), username: String(values.get("username")), department: String(values.get("department") || "").trim(), password: String(values.get("password")),
    workspaces: values.getAll("workspaces").map(String), workspacePaths: [String(values.get("workspacePath") || "").trim()].filter(Boolean),
    disabled: values.get("disabled") === "on" };
  fields.disabled = true;
  errorMessage.textContent = "";
  try {
    if (editing) await updateUser(editing.account, value);
    else await createUser(value);
    await refreshWorkspaceOptions();
    await refreshUsers();
    userDialog.close("saved");
    selectUser(null);
    status.textContent = "用户已保存。";
  } catch (error) { showAccountError(error, errorMessage); }
  finally { fields.disabled = false; }
});

const userDialog = document.querySelector<HTMLDialogElement>("[data-user-dialog]")!;
function openUserDialog(user: Account | null) { selectUser(user); userDialog.showModal(); }
document.querySelector("[data-user-new]")!.addEventListener("click", () => openUserDialog(null));
document.querySelectorAll("[data-user-cancel]").forEach((button) => button.addEventListener("click", () => userDialog.close("cancel")));
userDialog.addEventListener("close", () => selectUser(null));
document.querySelector("[data-users-prev]")!.addEventListener("click", () => { if (userPage > 1) { userPage--; void refreshUsersSafely(); } });
document.querySelector("[data-users-next]")!.addEventListener("click", () => { if (userPage * userPageSize < userTotal) { userPage++; void refreshUsersSafely(); } });
document.querySelector<HTMLInputElement>("[data-user-search]")!.addEventListener("input", () => { window.clearTimeout(searchTimer); searchTimer = window.setTimeout(() => { userPage = 1; void refreshUsersSafely(); }, 250); });
document.querySelector<HTMLSelectElement>("[data-user-page-size]")!.addEventListener("change", (event) => { userPageSize = Number((event.target as HTMLSelectElement).value); userPage = 1; void refreshUsersSafely(); });
const importInput = document.querySelector<HTMLInputElement>("[data-user-import-input]")!;
document.querySelector("[data-user-import]")!.addEventListener("click", () => importInput.click());
importInput.addEventListener("change", async () => {
  const file = importInput.files?.[0];
  importInput.value = "";
  if (!file) return;
  usersStatus.classList.remove("is-error");
  usersStatus.textContent = "导入中…";
  let result: { imported: number };
  try {
    result = await importUsers(file);
  } catch (error) {
    showImportError(error, usersStatus);
    return;
  }
  try {
    await Promise.all([refreshWorkspaceOptions(), refreshUsers()]);
    usersStatus.classList.remove("is-error");
    usersStatus.textContent = `成功导入 ${result.imported} 个用户。`;
  } catch {
    usersStatus.classList.remove("is-error");
    usersStatus.textContent = `成功导入 ${result.imported} 个用户，但列表刷新失败，请手动刷新。`;
  }
});
document.querySelector("[data-batch-open]")!.addEventListener("click", () => {
  batchWorkspacePath = "";
  batchTargetCount.textContent = `${selectedUsers.size} 个用户`;
  batchSelectedPath.textContent = "尚未选择工作区。";
  batchStatus.classList.remove("is-error");
  batchStatus.textContent = "";
  batchDialog.showModal();
});
document.querySelectorAll("[data-batch-cancel]").forEach((button) => button.addEventListener("click", () => batchDialog.close("cancel")));
batchForm.addEventListener("submit", async (event) => {
  event.preventDefault();
  if (!batchWorkspacePath) {
    batchStatus.classList.add("is-error");
    batchStatus.textContent = "请先选择要授权的服务器文件夹。";
    return;
  }
  if (!selectedUsers.size) {
    batchStatus.classList.add("is-error");
    batchStatus.textContent = "请先在用户列表中选择用户。";
    return;
  }
  const count = selectedUsers.size;
  batchSubmit.disabled = true;
  batchStatus.classList.remove("is-error");
  batchStatus.textContent = "授权保存中…";
  try {
    await batchAssignWorkspaces([...selectedUsers], [], [batchWorkspacePath]);
    selectedUsers.clear();
    syncBatchControls();
    let message = `已为 ${count} 个用户授权工作区。`;
    try {
      await Promise.all([refreshWorkspaceOptions(), refreshUsers()]);
    } catch {
      message = `已为 ${count} 个用户授权，但列表刷新失败，请手动刷新。`;
    }
    batchDialog.close("saved");
    showOperationSuccess(message, usersStatus);
  } catch (error) {
    showAccountError(error, batchStatus);
    batchStatus.textContent = `批量授权失败：${batchStatus.textContent}`;
    batchStatus.classList.add("is-error");
  } finally {
    batchSubmit.disabled = false;
  }
});
const directoryDialog = document.querySelector<HTMLDialogElement>("[data-directory-dialog]")!;
const directoryCurrent = document.querySelector<HTMLElement>("[data-directory-current]")!;
const directoryList = document.querySelector<HTMLElement>("[data-directory-list]")!;
let selectedDirectory = "/";
let directoryTarget: "user" | "batch" = "user";
function openDirectoryPicker(target: "user" | "batch") {
  directoryTarget = target;
  directoryDialog.showModal();
  void loadDirectory("/");
}
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
document.querySelector("[data-open-directory]")!.addEventListener("click", () => openDirectoryPicker("user"));
document.querySelector("[data-batch-open-directory]")!.addEventListener("click", () => openDirectoryPicker("batch"));
document.querySelector("[data-directory-up]")!.addEventListener("click", () => void loadDirectory(selectedDirectory.replace(/\/[^/]*$/, "") || "/"));
directoryDialog.addEventListener("close", () => {
  if (directoryDialog.returnValue !== "confirm") return;
  if (directoryTarget === "batch") {
    batchWorkspacePath = selectedDirectory;
    batchSelectedPath.textContent = `已选择：${selectedDirectory}`;
    return;
  }
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
    await deleteUser(editing.account);
    await refreshUsers();
    userDialog.close("deleted");
    selectUser(null);
    status.textContent = "用户已删除。";
  } catch (error) { showAccountError(error, errorMessage); }
  finally { fields.disabled = false; }
});

async function initialize() {
  if (!await requireAccount(true)) return;
  await refreshWorkspaceOptions();
  await refreshUsers();
  selectUser(null); syncBatchControls();
  document.querySelector<HTMLElement>("[data-users-content]")!.hidden = false;
}
void initialize().catch((error) => showAccountError(error, errorMessage));
