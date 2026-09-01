import {
  deleteWorkspace as deleteWorkspaceRequest,
  getWorkspaces,
  listFolders,
  openWorkspace as openWorkspaceRequest,
} from "../../lib/api";
import { requestErrorMessage } from "./workspace-methods";

export const workspaceFolderMethods = {
  async openFolderPopover(purpose = "workspace") {
    this.folderBrowsePurpose = purpose;
    const popover = this.querySelector("[data-folder-popover]");
    const browser = this.querySelector("[data-folder-browser]");
    const trigger = this.querySelector("[data-action='folder-open']");
    popover?.removeAttribute("hidden");
    browser?.removeAttribute("hidden");
    trigger?.setAttribute("aria-expanded", "true");
    await this.loadFolder("~", purpose);
    this.focusFolderPopover();
  },

  closeFolderPopover() {
    const popover = this.querySelector("[data-folder-popover]");
    if (!popover || popover.hidden) return;
    const trigger = this.querySelector("[data-action='folder-open']");
    const wasAllowedRoot = this.folderBrowsePurpose === "allowed-root";
    popover.setAttribute("hidden", "");
    trigger?.setAttribute("aria-expanded", "false");
    if (wasAllowedRoot) this.restoreAllowedRootPicker?.();
    if (!trigger?.closest('[data-view="picker"]')?.hasAttribute("hidden")) trigger?.focus();
  },

  focusFolderPopover() {
    this.querySelector("[data-folder-popover] [data-folder-list] button, [data-folder-popover] [data-action='folder-open-current']")?.focus();
  },

  async browseFolder(purpose = "workspace") {
    if (purpose === "workspace") return this.openFolderPopover("workspace");
    this.folderBrowsePurpose = purpose;
    if (purpose === "allowed-root" && this.dataset.route !== "picker") {
      this.allowedRootPickerReturnRoute = this.dataset.route || "workspace";
      this.allowedRootPickerSettingsWasOpen = !!this.settingsModal && !this.settingsModal.hidden;
      this.settingsModal?.setAttribute("hidden", "");
      this.dataset.route = "picker";
      this.querySelector('[data-view="picker"]')?.removeAttribute("hidden");
      this.querySelector('[data-view="workspace"]')?.setAttribute("hidden", "");
    }
    const popover = this.querySelector("[data-folder-popover]");
    const browser = this.querySelector("[data-folder-browser]");
    const trigger = this.querySelector("[data-action='folder-open']");
    popover?.removeAttribute("hidden");
    browser?.removeAttribute("hidden");
    trigger?.setAttribute("aria-expanded", "true");
    await this.loadFolder(this.currentFolder || "~", purpose);
    this.focusFolderPopover();
  },

  async loadFolder(path = "~", purpose = this.folderBrowsePurpose || "workspace") {
    if (!this.apiConnected) {
      this.setConnection("err");
      return;
    }
    if (this.folderLoadPending) return;
    this.folderLoadPending = true;

    try {
      const listing = await listFolders(path || "~", purpose);
      this.currentFolder = listing.path;
      this.currentFolderParent = listing.parent || listing.path;
      this.renderFolderListing(listing);
    } catch (error) {
      this.renderFolderError(error);
      this.setConnection("err");
    } finally {
      this.folderLoadPending = false;
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
    const label = this.querySelector("[data-folder-path]");
    if (label) label.textContent = listing.displayPath || listing.path;
    const upButton = this.querySelector("[data-folder-browser] [data-action='folder-up']");
    if (upButton) upButton.disabled = (listing.parent || listing.path) === listing.path;
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
    const marker = document.createElement("span");
    marker.textContent = "▸";
    const name = document.createElement("span");
    name.className = "folder-name";
    name.textContent = folder.name;
    const path = document.createElement("span");
    path.className = "folder-path";
    path.textContent = folder.displayPath || folder.path;
    row.append(marker, name, path);
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
