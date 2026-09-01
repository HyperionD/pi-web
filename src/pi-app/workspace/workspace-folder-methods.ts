import {
  cloneWorkspace as cloneWorkspaceRequest,
  deleteWorkspace as deleteWorkspaceRequest,
  getWorkspaces,
  listFolders,
  openWorkspace as openWorkspaceRequest,
} from "../../lib/api";
import { requestErrorMessage } from "./workspace-methods";

export const workspaceFolderMethods = {
  async browseFolder(purpose = "workspace") {
    this.folderBrowsePurpose = purpose;
    if (purpose === "allowed-root" && this.dataset.route !== "picker") {
      this.allowedRootPickerReturnRoute = this.dataset.route || "workspace";
      this.allowedRootPickerSettingsWasOpen = !!this.settingsModal && !this.settingsModal.hidden;
      this.settingsModal?.setAttribute("hidden", "");
      this.dataset.route = "picker";
      this.querySelector('[data-view="picker"]')?.removeAttribute("hidden");
      this.querySelector('[data-view="workspace"]')?.setAttribute("hidden", "");
    }
    this.querySelector("[data-folder-browser]")?.removeAttribute("hidden");
    await this.loadFolder(this.currentFolder || "~", purpose);
  },

  async loadFolder(path = "~", purpose = this.folderBrowsePurpose || "workspace") {
    if (!this.apiConnected) {
      this.setConnection("err");
      return;
    }

    try {
      const listing = await listFolders(path || "~", purpose);
      this.currentFolder = listing.path;
      this.currentFolderParent = listing.parent || listing.path;
      this.renderFolderListing(listing);
    } catch (error) {
      this.renderFolderError(error);
      this.setConnection("err");
    }
  },

  renderFolderError(error) {
    const list = this.querySelector("[data-folder-list]");
    if (!list) return;
    const message = document.createElement("div");
    message.className = "folder-error";
    message.textContent = requestErrorMessage(error);
    list.replaceChildren(message);
  },

  renderFolderListing(listing) {
    const input = this.querySelector('[data-path-form] input[name="path"]');
    if (input) input.value = listing.path;
    const label = this.querySelector("[data-folder-path]");
    if (label) label.textContent = listing.displayPath || listing.path;
    const list = this.querySelector("[data-folder-list]");
    if (!list) return;
    list.replaceChildren();
    for (const folder of listing.folders || []) list.append(this.folderRow(folder));
    if (!listing.folders?.length) list.append(this.emptyFolderRow());
  },

  emptyFolderRow() {
    const empty = document.createElement("div");
    empty.className = "folder-empty";
    empty.textContent = "暂无文件夹";
    return empty;
  },

  folderRow(folder) {
    const row = document.createElement("button");
    row.type = "button";
    row.className = "folder-row";
    row.dataset.action = "folder-enter";
    row.dataset.path = folder.path;
    row.innerHTML = `<span>▸</span><span class="folder-name"></span><span class="folder-path"></span>`;
    row.querySelector(".folder-name").textContent = folder.name;
    row.querySelector(".folder-path").textContent = folder.displayPath || folder.path;
    return row;
  },

  async openWorkspacePath(path) {
    if (!path) return;
    await openWorkspaceRequest(path);
    const { workspaces } = await getWorkspaces();
    const workspaceList = this.renderFilteredWorkspaces(workspaces || []);
    const workspace = workspaceList.find((item) => item.path === path) || workspaceList[0];
    if (workspace) await this.openWorkspace(workspace.id);
  },

  async submitCloneWorkspace(event) {
    event.preventDefault();
    const form = event.currentTarget;
    const gitUrl = form.querySelector("input[name='gitUrl']")?.value.trim();
    const name = form.querySelector("input[name='name']")?.value.trim() || "";
    if (!gitUrl || !this.apiConnected) return;

    const button = form.querySelector("button[type='submit']");
    if (button) button.disabled = true;
    try {
      const cloned = await cloneWorkspaceRequest(this.currentFolder || "~", gitUrl, name);
      form.reset();
      const { workspaces } = await getWorkspaces();
      const workspaceList = this.renderFilteredWorkspaces(workspaces || []);
      const workspace = cloned.workspace
        ? workspaceList.find((item) => item.id === cloned.workspace.id || item.path === cloned.workspace.path)
        : workspaceList[0];
      if (workspace) await this.openWorkspace(workspace.id);
    } catch (error) {
      if (!this.recoverForbiddenWorkspace?.(error)) {
        alert(requestErrorMessage(error));
        this.setConnection("err");
      }
    } finally {
      if (button) button.disabled = false;
    }
  },

  async submitWorkspacePath(event) {
    event.preventDefault();
    const input = event.currentTarget.querySelector("input[name='path']");
    const path = input?.value.trim();
    if (!path) return;
    if (this.apiConnected) {
      try {
        await this.openWorkspacePath(path);
        return;
      } catch (error) {
        if (!this.recoverForbiddenWorkspace?.(error)) {
          this.setConnection("err");
          this.route("workspace");
        }
        return;
      }
    }
    this.route("workspace");
  },

  async deleteWorkspace(workspaceId) {
    if (!workspaceId || !this.apiConnected) return;
    if (!confirm(`要从当前视图移除工作区 ${workspaceId} 吗？`)) return;
    try {
      await deleteWorkspaceRequest(workspaceId);
      const { workspaces } = await getWorkspaces();
      this.renderFilteredWorkspaces(workspaces || []);
    } catch (error) {
      if (!this.recoverForbiddenWorkspace?.(error)) this.setConnection("err");
    }
  },

  async openWorkspace(workspaceId) {
    this.activateWorkspaceForSession(workspaceId, { loadContext: true, forceLoadContext: true });
    this.route("workspace");
  },
};
