import {
  closeWorkspace as closeWorkspaceRequest,
  getWorkspaces,
  listFolders,
  openWorkspace as openWorkspaceRequest,
} from "../../lib/api";
import { requestErrorMessage } from "./workspace-methods";

export const workspaceFolderMethods = {
  beginFolderPicker(purpose) {
    const token = Symbol(`folder-picker:${purpose}`);
    this.folderPickerToken = token;
    this.folderBrowsePurpose = purpose;
    return token;
  },

  setFolderLoading(loading) {
    const browser = this.querySelector("[data-folder-browser]");
    const loadingLabel = this.querySelector("[data-folder-loading]");
    browser?.setAttribute("aria-busy", String(loading));
    loadingLabel?.toggleAttribute("hidden", !loading);
    const trigger = this.folderPickerTrigger;
    if (trigger) trigger.disabled = loading;
    this.querySelectorAll("[data-folder-browser] [data-action='folder-toggle'], [data-folder-browser] [data-action='folder-select'], [data-folder-browser] [data-action='folder-open-current']")
      .forEach((button) => { button.disabled = loading; });
    const confirm = this.querySelector("[data-action='folder-open-current']");
    if (confirm) confirm.disabled = loading || this.folderOpeningPending || !this.folderListingValid;
  },

  setFolderOpening(opening) {
    this.folderOpeningPending = opening;
    const confirm = this.querySelector("[data-action='folder-open-current']");
    if (!confirm) return;
    confirm.disabled = opening || this.folderLoadPending || !this.folderListingValid;
    confirm.setAttribute("aria-busy", String(opening));
    confirm.textContent = opening ? "正在打开…" : "选择此文件夹";
  },

  resetFolderPopover() {
    this.folderPickerToken = Symbol("reset-folder-picker");
    this.folderOpeningToken = Symbol("reset-folder-opening");
    this.folderLoadPending = false;
    this.folderOpeningPending = false;
    this.folderListingValid = false;
    this.setFolderLoading(false);
    this.setFolderOpening(false);
    this.querySelector("[data-folder-popover]")?.setAttribute("hidden", "");
    this.folderPickerTrigger?.setAttribute("aria-expanded", "false");
    this.querySelectorAll("[data-action='route-picker'], [data-action='folder-open'], [data-allowed-root-add]")
      .forEach((trigger) => trigger.setAttribute("aria-expanded", "false"));
    this.folderPickerTrigger = undefined;
    this.folderPickerTriggerRect = undefined;
    this.folderPickerEvent = undefined;
  },

  resolveFolderTrigger(event) {
    return event?.target?.closest?.("button")
      || this.querySelector("[data-action='route-picker'], [data-action='folder-open'], [data-allowed-root-add]");
  },

  positionFolderPopover(
    event = this.folderPickerEvent,
    trigger = this.folderPickerTrigger || this.resolveFolderTrigger(event),
  ) {
    const popover = this.querySelector("[data-folder-popover]");
    if (!popover) return;
    const bounds = this.folderPickerTriggerRect || trigger?.getBoundingClientRect?.();
    const x = Number.isFinite(event?.clientX) && event.clientX > 0 ? event.clientX : bounds?.left || 16;
    const y = Number.isFinite(event?.clientY) && event.clientY > 0 ? event.clientY : bounds?.bottom || 16;
    const width = popover.offsetWidth || 420;
    const height = popover.offsetHeight || 360;
    const left = Math.max(16, Math.min(x, window.innerWidth - width - 16));
    const top = Math.max(16, Math.min(y + 8, window.innerHeight - height - 16));
    popover.style.left = `${left}px`;
    popover.style.top = `${top}px`;
  },

  async openFolderPopover(purpose = "workspace", event) {
    const popover = this.querySelector("[data-folder-popover]");
    if (purpose === "workspace" && popover && !popover.hidden) return;
    if (this.folderLoadPending) return;
    const pickerToken = this.beginFolderPicker(purpose);
    this.folderTreeRoots = [];
    this.folderTreeNodes = new Map();
    this.folderSelectedPath = undefined;
    const browser = this.querySelector("[data-folder-browser]");
    const trigger = this.resolveFolderTrigger(event);
    this.folderPickerTrigger = trigger;
    this.folderPickerTriggerRect ??= trigger?.getBoundingClientRect?.();
    this.folderPickerEvent = event;
    popover?.removeAttribute("hidden");
    browser?.removeAttribute("hidden");
    trigger?.setAttribute("aria-expanded", "true");
    this.positionFolderPopover(event, trigger);
    await this.loadFolder(purpose === "workspace" ? "~" : this.currentFolder || "~", purpose);
    if (this.folderPickerToken === pickerToken && popover && !popover.hidden) this.focusFolderPopover();
  },

  closeFolderPopover() {
    const popover = this.querySelector("[data-folder-popover]");
    if (!popover || popover.hidden) return;
    const trigger = this.folderPickerTrigger;
    const wasAllowedRoot = this.folderBrowsePurpose === "allowed-root";
    this.resetFolderPopover();
    if (wasAllowedRoot) {
      this.restoreAllowedRootPicker?.();
      return;
    }
    trigger?.focus();
  },

  focusFolderPopover() {
    this.querySelector("[data-folder-popover] [data-action='folder-select'], [data-folder-popover] [data-action='folder-open-current']")?.focus();
  },

  async browseFolder(purpose = "workspace", event) {
    if (purpose === "workspace") {
      if (event) return this.openFolderPopover("workspace", event);
      return this.openFolderPopover("workspace");
    }
    if (this.folderLoadPending) return;
    if (purpose === "allowed-root") {
      this.allowedRootPickerReturnRoute ??= this.dataset.route || "workspace";
      this.allowedRootPickerSettingsWasOpen ??= !!this.settingsModal && !this.settingsModal.hidden;
      this.folderPickerTrigger = this.resolveFolderTrigger(event);
      this.folderPickerTriggerRect = this.folderPickerTrigger?.getBoundingClientRect?.();
      this.folderPickerEvent = event;
      this.settingsModal?.setAttribute("hidden", "");
    }
    return this.openFolderPopover(purpose, event);
  },

  async loadFolder(path = "~", purpose = this.folderBrowsePurpose || "workspace", mode = "root") {
    if (!this.apiConnected) {
      this.folderListingValid = false;
      this.renderFolderError(new Error("后端连接已断开"));
      this.setConnection("err");
      return;
    }
    if (this.folderLoadPending) return;
    const requestToken = Symbol("folder-load");
    const pickerToken = this.folderPickerToken;
    const isCurrent = () => pickerToken
      ? this.folderPickerToken === pickerToken && this.folderBrowsePurpose === purpose
      : !this.folderPickerToken;
    this.folderLoadToken = requestToken;
    this.folderLoadPending = true;
    if (mode !== "children") this.folderListingValid = false;
    this.setFolderLoading(true);

    try {
      const listing = await listFolders(path || "~", purpose);
      if (!isCurrent()) return;
      if (mode === "children") {
        const node = this.folderTreeNodes?.get(listing.path || path);
        if (!node) return;
        node.children = (listing.folders || []).map((folder) => this.createFolderTreeNode(folder));
        node.loaded = true;
        node.expanded = true;
        this.renderFolderTree();
      } else {
        this.currentFolder = listing.path;
        this.renderFolderListing(listing);
      }
      if (this.querySelector("[data-folder-popover]")?.hidden === false) this.positionFolderPopover();
    } catch (error) {
      if (!isCurrent()) return;
      this.renderFolderError(error);
      this.setConnection("err");
    } finally {
      if (this.folderLoadToken === requestToken) {
        this.folderLoadPending = false;
        this.setFolderLoading(false);
      }
    }
  },

  renderFolderError(error) {
    this.folderListingValid = false;
    this.folderTreeRoots = [];
    this.folderTreeNodes = new Map();
    this.setFolderLoading(false);
    const list = this.querySelector("[data-folder-list]");
    if (!list) return;
    const message = document.createElement("div");
    message.className = "folder-error";
    message.textContent = requestErrorMessage(error);
    list.replaceChildren(message);
  },

  renderFolderListing(listing) {
    const isRootListing = listing.rootListing === true;
    const folders = (listing.folders || []).map((folder) => this.createFolderTreeNode(folder));
    const displayPath = isRootListing ? "允许的工作区目录" : listing.displayPath || listing.path;
    this.folderListingValid = !isRootListing;
    this.currentFolder = isRootListing ? "" : listing.path;
    this.folderSelectedPath = isRootListing ? undefined : listing.path;
    const confirm = this.querySelector("[data-action='folder-open-current']");
    if (confirm) confirm.disabled = isRootListing || this.folderLoadPending || this.folderOpeningPending;
    this.folderTreeRoots = isRootListing ? folders : [{
      name: displayPath,
      path: listing.path,
      displayPath,
      children: folders,
      loaded: true,
      expanded: true,
    }];
    this.renderFolderTree();
  },

  createFolderTreeNode(folder) {
    return { ...folder, children: [], loaded: false, expanded: false };
  },

  renderFolderTree() {
    const list = this.querySelector("[data-folder-list]");
    if (!list) return;
    this.folderTreeNodes = new Map();
    if (!this.folderTreeRoots?.length) {
      list.replaceChildren(this.emptyFolderRow());
      return;
    }
    list.replaceChildren(...this.folderTreeRoots.map((root) => this.folderTreeItem(root, 0)));
  },

  folderTreeItem(node, depth) {
    this.folderTreeNodes.set(node.path, node);
    const item = document.createElement("div");
    item.className = "folder-tree-node";
    item.dataset.folderTreeNode = "";
    item.dataset.path = node.path;
    item.setAttribute("role", "treeitem");
    item.setAttribute("aria-level", String(depth + 1));
    item.setAttribute("aria-selected", String(node.path === this.folderSelectedPath));

    const row = document.createElement("div");
    row.className = "folder-tree-row";
    row.style.setProperty("--folder-depth", String(depth));
    const toggle = document.createElement("button");
    toggle.type = "button";
    toggle.className = "folder-toggle";
    toggle.dataset.action = "folder-toggle";
    toggle.dataset.path = node.path;
    toggle.disabled = node.loaded && !node.children.length;
    toggle.setAttribute("aria-expanded", String(node.expanded));
    toggle.setAttribute("aria-label", `${node.expanded ? "收起" : "展开"} ${node.name || node.path}`);
    toggle.textContent = node.loaded && !node.children.length ? "" : node.expanded ? "⌄" : "›";
    row.append(toggle, this.folderRow(node));
    item.append(row);

    if (node.expanded && node.children.length) {
      const children = document.createElement("div");
      children.className = "folder-tree-children";
      children.setAttribute("role", "group");
      node.children.forEach((child) => children.append(this.folderTreeItem(child, depth + 1)));
      item.append(children);
    } else if (node.expanded && node.loaded) {
      item.append(this.emptyFolderRow());
    }
    return item;
  },

  async toggleFolder(path) {
    const node = this.folderTreeNodes?.get(path);
    if (!node || this.folderLoadPending) return;
    if (node.loaded) {
      if (!node.children.length) return;
      node.expanded = !node.expanded;
      this.renderFolderTree();
      return;
    }
    return this.loadFolder(path, this.folderBrowsePurpose || "workspace", "children");
  },

  selectFolder(path) {
    const node = this.folderTreeNodes?.get(path);
    if (!node) return;
    this.folderSelectedPath = node.path;
    this.currentFolder = node.path;
    this.folderListingValid = true;
    this.renderFolderTree();
    this.setFolderLoading(false);
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
    row.dataset.action = "folder-select";
    row.dataset.path = folder.path;
    const name = document.createElement("span");
    name.className = "folder-name";
    name.textContent = folder.name;
    const path = document.createElement("span");
    path.className = "folder-path";
    path.textContent = folder.displayPath || folder.path;
    row.append(name, path);
    return row;
  },

  async openWorkspacePath(path, openingToken) {
    if (!path) return;
    const openGeneration = Symbol("workspace-open");
    this.workspaceOpenGeneration = openGeneration;
    const isCurrentOpening = () => (
      (openingToken === undefined || this.folderOpeningToken === openingToken)
      && this.workspaceOpenGeneration === openGeneration
    );
    const opened = await openWorkspaceRequest(path);
    if (!isCurrentOpening()) return;
    const { workspaces } = await getWorkspaces();
    if (!isCurrentOpening()) return;
    const openedWorkspace = opened?.workspace || opened;
    const candidates = [...(workspaces || [])];
    if (openedWorkspace?.id && !candidates.some((item) => item.id === openedWorkspace.id)) {
      candidates.push(openedWorkspace);
    }
    const workspaceList = this.renderFilteredWorkspaces(candidates);
    const workspace = openedWorkspace?.id
      ? workspaceList.find((item) => item.id === openedWorkspace.id)
      : workspaceList.find((item) => item.path === path);
    if (workspace) await this.openWorkspace(workspace.id);
  },

  async closeWorkspace(workspaceId) {
    if (!workspaceId || !this.apiConnected) return;
    if (!confirm(`要从当前视图移除工作区 ${workspaceId} 吗？`)) return;
    try {
      await closeWorkspaceRequest(workspaceId);
      const { workspaces } = await getWorkspaces();
      this.renderFilteredWorkspaces(workspaces || []);
    } catch (error) {
      if (!this.recoverForbiddenWorkspace?.(error)) this.setConnection("err");
    }
  },

  async openWorkspace(workspaceId) {
    this.workspaceOpenGeneration = Symbol("workspace-open");
    this.activateWorkspaceForSession(workspaceId, { loadContext: true, forceLoadContext: true });
    this.route("workspace");
  },
};
