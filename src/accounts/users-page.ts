import { batchAssignWorkspaces, createUser, deleteUser, importUsers, listFolders, listManagedWorkspaces, listUsers, updateUser, type Account, type ManagedWorkspace } from "./api";
import { requireAccount, showAccountError, showImportError, showOperationSuccess } from "./browser";

type UsersPageOptions = { skipAuth?: boolean };

export async function initUsersPage(scope: ParentNode = document, options: UsersPageOptions = {}) {
  const query = <T extends Element>(selector: string) => scope.querySelector<T>(selector);
  const required = <T extends Element>(selector: string) => {
    const element = query<T>(selector);
    if (!element) throw new Error(`用户管理缺少必要元素：${selector}`);
    return element;
  };
  const form = query<HTMLFormElement>("[data-user-form]");
  if (!form) return;
  const fields = required<HTMLFieldSetElement>("[data-user-fields]");
  const errorMessage = required<HTMLElement>("[data-account-error]");
  const status = required<HTMLElement>("[data-user-status]");
  const usersStatus = required<HTMLElement>("[data-users-status]");
  const batchDialog = required<HTMLDialogElement>("[data-batch-dialog]");
  const batchForm = required<HTMLFormElement>("[data-batch-form]");
  const batchStatus = required<HTMLElement>("[data-batch-status]");
  const batchSubmit = required<HTMLButtonElement>("[data-batch-submit]");
  const batchTargetCount = required<HTMLElement>("[data-batch-target-count]");
  const batchSelectedPath = required<HTMLElement>("[data-batch-selected-path]");
  const removeButton = required<HTMLButtonElement>("[data-user-delete]");
  const deleteDialog = required<HTMLDialogElement>("[data-delete-dialog]");
  const userDialog = required<HTMLDialogElement>("[data-user-dialog]");
  const directoryDialog = required<HTMLDialogElement>("[data-directory-dialog]");
  const directoryCurrent = required<HTMLElement>("[data-directory-current]");
  const directoryList = required<HTMLElement>("[data-directory-list]");
  const content = required<HTMLElement>("[data-users-content]");
  let editing: Account | null = null;
  let workspaces: ManagedWorkspace[] = [];
  let userPage = 1;
  let userTotal = 0;
  let userPageSize = 10;
  let searchTimer: number | undefined;
  let batchWorkspacePath = "";
  let selectedDirectory = "/";
  let directoryTarget: "user" | "batch" = "user";
  const selectedUsers = new Set<string>();

  function selectUser(user: Account | null) {
    editing = user;
    form.reset();
    required<HTMLElement>("[data-selected-directory]").textContent = "未选择新目录。点击“添加目录”后从服务器目录树中选择。";
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
    required<HTMLElement>("[data-user-form-title]").textContent = user ? `编辑 ${user.username}` : "创建用户";
    required<HTMLElement>("[data-password-label]").textContent = user ? "重置密码（留空不修改）" : "初始密码";
    removeButton.hidden = !user;
    const container = required<HTMLElement>("[data-user-workspaces]");
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
    required<HTMLElement>("[data-account-batch]").hidden = selectedUsers.size === 0;
    required<HTMLElement>("[data-batch-count]").textContent = `${selectedUsers.size} 个用户`;
  }

  async function refreshUsersSafely() {
    try { await refreshUsers(); } catch (error) { showAccountError(error, errorMessage); }
  }

  async function refreshWorkspaceOptions() {
    ({ workspaces } = await listManagedWorkspaces());
  }

  async function refreshUsers() {
    const queryValue = query<HTMLInputElement>("[data-user-search]")?.value.trim() || "";
    const { users, total, page, pageSize } = await listUsers(queryValue, userPage, userPageSize);
    userTotal = total; userPage = page; userPageSize = pageSize;
    const container = required<HTMLTableSectionElement>("[data-users-list]");
    container.replaceChildren();
    required<HTMLElement>("[data-users-empty]").hidden = users.length > 0;
    required<HTMLElement>("[data-users-count]").textContent = String(total);
    required<HTMLElement>("[data-users-page]").textContent = `第 ${page} 页 / 共 ${Math.max(1, Math.ceil(total / userPageSize))} 页`;
    required<HTMLButtonElement>("[data-users-prev]").disabled = page <= 1;
    required<HTMLButtonElement>("[data-users-next]").disabled = page * userPageSize >= total;
    for (const user of users) {
      const row = document.createElement("tr");
      const selectCell = document.createElement("td");
      const checkbox = document.createElement("input");
      checkbox.type = "checkbox";
      checkbox.checked = selectedUsers.has(user.account);
      checkbox.setAttribute("aria-label", `选择 ${user.username}`);
      checkbox.addEventListener("change", () => { if (checkbox.checked) selectedUsers.add(user.account); else selectedUsers.delete(user.account); syncBatchControls(); });
      selectCell.append(checkbox);
      const accountCell = document.createElement("td");
      const accountCode = document.createElement("code");
      accountCode.textContent = user.account;
      accountCell.append(accountCode);
      const nameCell = document.createElement("td"); nameCell.textContent = user.username; nameCell.className = "account-user-name";
      const departmentCell = document.createElement("td"); departmentCell.textContent = user.department || "—";
      const statusCell = document.createElement("td");
      const badge = document.createElement("span"); badge.textContent = user.disabled ? "已禁用" : "正常"; badge.className = `account-user-status${user.disabled ? " is-disabled" : ""}`; statusCell.append(badge);
      const workspaceCell = document.createElement("td"); workspaceCell.textContent = `${user.workspaces.length} 个工作区`;
      const actionCell = document.createElement("td");
      const editButton = document.createElement("button"); editButton.type = "button"; editButton.className = "account-row-action"; editButton.textContent = "编辑"; editButton.addEventListener("click", () => openUserDialog(user)); actionCell.append(editButton);
      row.append(selectCell, accountCell, nameCell, departmentCell, statusCell, workspaceCell, actionCell); container.append(row);
    }
  }

  function openUserDialog(user: Account | null) {
    selectUser(user);
    if (!userDialog.open) userDialog.showModal();
  }

  async function saveUser(event: SubmitEvent) {
    event.preventDefault();
    const values = new FormData(form);
    const value = { account: String(values.get("account")), username: String(values.get("username")), department: String(values.get("department") || "").trim(), password: String(values.get("password")),
      workspaces: values.getAll("workspaces").map(String), workspacePaths: [String(values.get("workspacePath") || "").trim()].filter(Boolean), disabled: values.get("disabled") === "on" };
    fields.disabled = true;
    errorMessage.textContent = "";
    try {
      if (editing) await updateUser(editing.account, value); else await createUser(value);
      await refreshWorkspaceOptions(); await refreshUsers();
      userDialog.close("saved"); selectUser(null); status.textContent = "用户已保存。";
    } catch (error) { showAccountError(error, errorMessage); }
    finally { fields.disabled = false; }
  }

  async function importUserFile() {
    const importInput = required<HTMLInputElement>("[data-user-import-input]");
    const file = importInput.files?.[0];
    importInput.value = "";
    if (!file) return;
    usersStatus.classList.remove("is-error"); usersStatus.textContent = "导入中…";
    let result: { imported: number };
    try { result = await importUsers(file); } catch (error) { showImportError(error, usersStatus); return; }
    try {
      await Promise.all([refreshWorkspaceOptions(), refreshUsers()]);
      usersStatus.classList.remove("is-error"); usersStatus.textContent = `成功导入 ${result.imported} 个用户。`;
    } catch {
      usersStatus.classList.remove("is-error"); usersStatus.textContent = `成功导入 ${result.imported} 个用户，但列表刷新失败，请手动刷新。`;
    }
  }

  function openDirectoryPicker(target: "user" | "batch") {
    directoryTarget = target;
    if (!directoryDialog.open) directoryDialog.showModal();
    void loadDirectory("/");
  }

  async function loadDirectory(path: string) {
    directoryCurrent.textContent = "加载目录中…";
    directoryList.replaceChildren();
    try {
      const result = await listFolders(path);
      selectedDirectory = result.path; directoryCurrent.textContent = result.path;
      const folders = result.folders || [];
      if (!folders.length) directoryList.textContent = "此目录没有可浏览的子目录。";
      for (const folder of folders) {
        const button = document.createElement("button");
        button.type = "button"; button.setAttribute("role", "treeitem"); button.textContent = folder.name; button.title = folder.displayPath || folder.path;
        button.addEventListener("click", () => void loadDirectory(folder.path)); directoryList.append(button);
      }
    } catch (error) { directoryCurrent.textContent = "无法加载目录"; showAccountError(error, errorMessage); }
  }

  async function deleteEditingUser() {
    if (deleteDialog.returnValue !== "confirm" || !editing) return;
    fields.disabled = true;
    try {
      await deleteUser(editing.account); await refreshUsers(); userDialog.close("deleted"); selectUser(null); status.textContent = "用户已删除。";
    } catch (error) { showAccountError(error, errorMessage); }
    finally { fields.disabled = false; }
  }

  form.addEventListener("submit", (event) => void saveUser(event));
  required<HTMLButtonElement>("[data-user-new]").addEventListener("click", () => openUserDialog(null));
  scope.querySelectorAll("[data-user-cancel]").forEach((button) => button.addEventListener("click", () => userDialog.close("cancel")));
  userDialog.addEventListener("close", () => selectUser(null));
  required<HTMLButtonElement>("[data-users-prev]").addEventListener("click", () => { if (userPage > 1) { userPage--; void refreshUsersSafely(); } });
  required<HTMLButtonElement>("[data-users-next]").addEventListener("click", () => { if (userPage * userPageSize < userTotal) { userPage++; void refreshUsersSafely(); } });
  required<HTMLInputElement>("[data-user-search]").addEventListener("input", () => { window.clearTimeout(searchTimer); searchTimer = window.setTimeout(() => { userPage = 1; void refreshUsersSafely(); }, 250); });
  required<HTMLSelectElement>("[data-user-page-size]").addEventListener("change", (event) => { userPageSize = Number((event.target as HTMLSelectElement).value); userPage = 1; void refreshUsersSafely(); });
  required<HTMLInputElement>("[data-user-import-input]").addEventListener("change", () => void importUserFile());
  required<HTMLButtonElement>("[data-user-import]").addEventListener("click", () => required<HTMLInputElement>("[data-user-import-input]").click());
  required<HTMLButtonElement>("[data-batch-open]").addEventListener("click", () => {
    batchWorkspacePath = ""; batchTargetCount.textContent = `${selectedUsers.size} 个用户`; batchSelectedPath.textContent = "尚未选择工作区。";
    batchStatus.classList.remove("is-error"); batchStatus.textContent = ""; batchDialog.showModal();
  });
  scope.querySelectorAll("[data-batch-cancel]").forEach((button) => button.addEventListener("click", () => batchDialog.close("cancel")));
  batchForm.addEventListener("submit", async (event) => {
    event.preventDefault();
    if (!batchWorkspacePath) { batchStatus.classList.add("is-error"); batchStatus.textContent = "请先选择要授权的服务器文件夹。"; return; }
    if (!selectedUsers.size) { batchStatus.classList.add("is-error"); batchStatus.textContent = "请先在用户列表中选择用户。"; return; }
    const count = selectedUsers.size;
    batchSubmit.disabled = true; batchStatus.classList.remove("is-error"); batchStatus.textContent = "授权保存中…";
    try {
      await batchAssignWorkspaces([...selectedUsers], [], [batchWorkspacePath]);
      selectedUsers.clear(); syncBatchControls();
      let message = `已为 ${count} 个用户授权工作区。`;
      try { await Promise.all([refreshWorkspaceOptions(), refreshUsers()]); } catch { message = `已为 ${count} 个用户授权，但列表刷新失败，请手动刷新。`; }
      batchDialog.close("saved"); showOperationSuccess(message, usersStatus);
    } catch (error) { showAccountError(error, batchStatus); batchStatus.textContent = `批量授权失败：${batchStatus.textContent}`; batchStatus.classList.add("is-error"); }
    finally { batchSubmit.disabled = false; }
  });
  required<HTMLButtonElement>("[data-open-directory]").addEventListener("click", () => openDirectoryPicker("user"));
  required<HTMLButtonElement>("[data-batch-open-directory]").addEventListener("click", () => openDirectoryPicker("batch"));
  required<HTMLButtonElement>("[data-directory-up]").addEventListener("click", () => void loadDirectory(selectedDirectory.replace(/\/[^/]*$/, "") || "/"));
  directoryDialog.addEventListener("close", () => {
    if (directoryDialog.returnValue !== "confirm") return;
    if (directoryTarget === "batch") { batchWorkspacePath = selectedDirectory; batchSelectedPath.textContent = `已选择：${selectedDirectory}`; return; }
    (form.elements.namedItem("workspacePath") as HTMLInputElement).value = selectedDirectory;
    required<HTMLElement>("[data-selected-directory]").textContent = `已选择：${selectedDirectory}`;
  });
  removeButton.addEventListener("click", () => { required<HTMLElement>("[data-delete-name]").textContent = editing?.username || ""; deleteDialog.returnValue = "cancel"; deleteDialog.showModal(); });
  deleteDialog.addEventListener("close", () => void deleteEditingUser());

  if (!options.skipAuth && !await requireAccount(true)) return;
  await refreshWorkspaceOptions(); await refreshUsers();
  selectUser(null); syncBatchControls(); content.hidden = false;
}

if (document.querySelector("[data-users-content]") && !document.querySelector("[data-user-management-dialog]")) {
  void initUsersPage(document).catch((error) => {
    const target = document.querySelector<HTMLElement>("[data-account-error]");
    if (target) showAccountError(error, target);
  });
}
