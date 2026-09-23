import { fullToolBody } from "../messages/tool-output-rendering";
import { renderAnsiBody } from "../../lib/renderers";

const SIDEBAR_WIDTH_KEY = "pi.sb.width";
const INSPECTOR_WIDTH_KEY = "pi.inspector.width";

function readStoredSidebarWidth() {
  try {
    const storedWidthValue = localStorage.getItem(SIDEBAR_WIDTH_KEY);
    if (!storedWidthValue) return undefined;
    const width = Number(storedWidthValue);
    return Number.isFinite(width) ? Math.min(480, Math.max(200, width)) : undefined;
  } catch {
    return undefined;
  }
}

function storeSidebarWidth(width) {
  try { localStorage.setItem(SIDEBAR_WIDTH_KEY, String(width)); } catch {}
}

function readStoredInspectorWidth() {
  try {
    const storedWidthValue = localStorage.getItem(INSPECTOR_WIDTH_KEY);
    if (!storedWidthValue) return undefined;
    const width = Number(storedWidthValue);
    return Number.isFinite(width) ? Math.min(720, Math.max(320, width)) : undefined;
  } catch {
    return undefined;
  }
}

function storeInspectorWidth(width) {
  try { localStorage.setItem(INSPECTOR_WIDTH_KEY, String(width)); } catch {}
}

const DIALOG_FOCUSABLE_SELECTOR = "a[href],button:not([disabled]),input:not([disabled]),select:not([disabled]),textarea:not([disabled]),[tabindex]:not([tabindex='-1'])";

function trapDialogFocus(dialog, event) {
  if (!dialog || event.key !== "Tab") return false;
  const focusables = [...dialog.querySelectorAll(DIALOG_FOCUSABLE_SELECTOR)]
    .filter((element) => !element.closest("[hidden]") && element.getAttribute("aria-hidden") !== "true");
  if (!focusables.length) {
    event.preventDefault();
    dialog.focus();
    return true;
  }
  const first = focusables[0];
  const last = focusables.at(-1);
  const activeElement = document.activeElement;
  if (!dialog.contains(activeElement)) {
    event.preventDefault();
    (event.shiftKey ? last : first).focus();
  } else if ((event.shiftKey && activeElement === first) || (!event.shiftKey && activeElement === last)) {
    event.preventDefault();
    (event.shiftKey ? last : first).focus();
  }
  return true;
}

export const layoutMethods = {
  shortcut(event) {
    if (event.key === "Tab" && this.trapFilePreviewFocus?.(event)) return;
    if (event.key === "Tab" && this.trapGitDetailFocus?.(event)) return;
    if (event.key === "Tab") this.trapSettingsFocus?.(event);
    if (event.key === "Escape" && (this.promptComposing || event.isComposing || event.keyCode === 229)) return;
    if (event.key === "Escape") {
      const userManagement = this.querySelector("[data-user-management-dialog]");
      if (userManagement?.open) {
        event.preventDefault();
        this.closeUserManagement?.();
        return;
      }
      const popover = this.querySelector("[data-folder-popover]");
      if (popover && !popover.hidden) {
        event.preventDefault();
        this.closeFolderPopover();
        return;
      }
      const gitDetail = this.querySelector("[data-git-detail-panel]");
      if (gitDetail && !gitDetail.hidden) {
        event.preventDefault();
        this.closeGitDetail?.();
        return;
      }
      const filePreview = this.querySelector("[data-file-preview]");
      if (filePreview && !filePreview.hidden) {
        event.preventDefault();
        this.closeFilePreview?.();
        return;
      }
      const body = this.querySelector(".app-body");
      if (body?.classList.contains("drawer-open")) {
        event.preventDefault();
        this.toggleDrawer(false);
        return;
      }
      if (this.running) {
        event.preventDefault();
        void this.cancelActiveSession();
        return;
      }
      this.closeModals();
    }
  },

  route(route) {
    this.dataset.route = route === "picker" ? "workspace" : route;
    this.querySelector('[data-view="workspace"]')?.removeAttribute("hidden");
    if (route === "workspace") this.scrollTerm();
  },

  toggleTree(forceOpen) {
    const tree = this.querySelector(".tree");
    const treeEnabled = forceOpen ?? this.dataset.tree !== "on";
    this.dataset.tree = treeEnabled ? "on" : "off";
    this.querySelector('[data-action="toggle-tree"]')?.classList.toggle("on", treeEnabled);
    tree?.toggleAttribute("hidden", !treeEnabled);
  },

  beginInspectorRequest() {
    this.inspectorRequestId = (this.inspectorRequestId || 0) + 1;
    return this.inspectorRequestId;
  },

  inspectorRequestIsCurrent(requestId, workspaceId) {
    return this.inspectorRequestId === requestId && this.dataset.activeWorkspaceId === workspaceId;
  },

  toggleFilePreviewPanel(forceOpen) {
    const panel = this.querySelector("[data-file-preview]");
    if (!panel) return;
    const wasOpen = !panel.hidden;
    const previewEnabled = forceOpen ?? !wasOpen;
    this.dataset.preview = previewEnabled ? "on" : "off";
    panel.toggleAttribute("hidden", !previewEnabled);
    if (previewEnabled && !wasOpen) {
      this.filePreviewReturnFocus = document.activeElement;
      (panel.querySelector("[data-action='close-file-preview']") || panel.querySelector(".fp-dialog"))?.focus();
    } else if (!previewEnabled && wasOpen) {
      const returnFocus = this.filePreviewReturnFocus;
      this.filePreviewReturnFocus = undefined;
      if (returnFocus?.isConnected) returnFocus.focus();
    }
  },

  trapFilePreviewFocus(event) {
    const dialog = this.querySelector("[data-file-preview]:not([hidden]) .fp-dialog");
    return trapDialogFocus(dialog, event);
  },

  toggleGitDetailPanel(forceOpen) {
    const panel = this.querySelector("[data-git-detail-panel]");
    if (!panel) return;
    const wasOpen = !panel.hidden;
    const detailEnabled = forceOpen ?? !wasOpen;
    this.dataset.gitDetail = detailEnabled ? "on" : "off";
    panel.toggleAttribute("hidden", !detailEnabled);
    if (detailEnabled && !wasOpen) {
      this.gitDetailReturnFocus = document.activeElement;
      (panel.querySelector(".git-detail-close") || panel.querySelector(".git-detail-dialog"))?.focus();
    } else if (!detailEnabled && wasOpen) {
      const returnFocus = this.gitDetailReturnFocus;
      this.gitDetailReturnFocus = undefined;
      if (returnFocus?.isConnected) returnFocus.focus();
    }
  },

  trapGitDetailFocus(event) {
    const dialog = this.querySelector("[data-git-detail-panel]:not([hidden]) .git-detail-dialog");
    return trapDialogFocus(dialog, event);
  },

  toggleDrawer(forceOpen) {
    const body = this.querySelector(".app-body");
    if (!body) return;
    const wasOpen = body.classList.contains("drawer-open");
    const open = forceOpen ?? !wasOpen;
    body.classList.toggle("drawer-open", open);
    const button = this.querySelector('[data-action="open-drawer"]');
    button?.setAttribute("aria-expanded", String(open));
    button?.setAttribute("aria-label", open ? "关闭侧栏" : "打开侧栏");
    if (!open && wasOpen && this.querySelector(".sidebar")?.contains(document.activeElement)) button?.focus();
  },

  collapseSidebar(collapsed) {
    this.dataset.sidebar = collapsed ? "collapsed" : "open";
    this.querySelector(".sidebar-wrap")?.toggleAttribute("hidden", collapsed);
    const expand = this.querySelector(".sb-expand-btn");
    if (expand) expand.style.display = collapsed ? "inline-flex" : "none";
    try { localStorage.setItem("pi.sb.collapsed", collapsed ? "1" : "0"); } catch {}
    this.applyGrid();
  },

  restoreSidebar() {
    const width = readStoredSidebarWidth();
    const inspectorWidth = readStoredInspectorWidth();
    if (width) this.dataset.sidebarWidth = String(width);
    if (inspectorWidth) this.dataset.inspectorWidth = String(inspectorWidth);
    if (this.dataset.sidebar === "collapsed") {
      this.collapseSidebar(true);
      return;
    }
    try { this.collapseSidebar(localStorage.getItem("pi.sb.collapsed") === "1"); } catch { this.applyGrid(width); }
  },

  applyGrid(width = Number(this.dataset.sidebarWidth || 280), inspectorWidth = Number(this.dataset.inspectorWidth || 560)) {
    const body = this.querySelector(".app-body");
    if (!body) return;
    const collapsed = this.dataset.sidebar === "collapsed";
    body.style.setProperty("--inspector-column", collapsed ? "2" : "3");
    this.querySelectorAll(".fp-resizer").forEach((resizer) => {
      resizer.setAttribute("aria-valuenow", String(inspectorWidth));
    });
    const expandedColumns = `${width}px 1fr`;
    const collapsedColumns = "1fr";
    body.style.gridTemplateColumns = collapsed ? collapsedColumns : expandedColumns;
  },

  startResize(event) {
    event.preventDefault();
    const startX = event.clientX;
    const startWidth = Number(this.dataset.sidebarWidth || 280);
    const move = (moveEvent) => {
      const width = Math.min(480, Math.max(200, startWidth + moveEvent.clientX - startX));
      this.dataset.sidebarWidth = String(width);
      this.applyGrid(width);
    };
    const stopResize = () => {
      storeSidebarWidth(Number(this.dataset.sidebarWidth || startWidth));
      window.removeEventListener("pointermove", move);
      window.removeEventListener("pointerup", stopResize);
    };
    window.addEventListener("pointermove", move);
    window.addEventListener("pointerup", stopResize);
  },

  startInspectorResize(event) {
    event.preventDefault();
    const startX = event.clientX;
    const startWidth = Number(this.dataset.inspectorWidth || 560);
    const move = (moveEvent) => {
      const width = Math.min(720, Math.max(320, startWidth + startX - moveEvent.clientX));
      this.dataset.inspectorWidth = String(width);
      this.applyGrid(undefined, width);
    };
    const stopResize = () => {
      storeInspectorWidth(Number(this.dataset.inspectorWidth || startWidth));
      window.removeEventListener("pointermove", move);
      window.removeEventListener("pointerup", stopResize);
    };
    window.addEventListener("pointermove", move);
    window.addEventListener("pointerup", stopResize);
  },

  toggleTool(button) {
    const card = button?.closest?.(".tool-card");
    const body = card?.querySelector(".tc-body");
    if (!card || !body) return;
    card.classList.remove("collapsed");
    body.hidden = false;
    this.notifyTranscriptNodeHeightDidChange(card);
  },

  showFullToolOutput(button) {
    const body = button?.closest(".tc-body");
    const fullBody = fullToolBody(body);
    if (!body || !fullBody) return;
    body.innerHTML = renderAnsiBody(fullBody);
    body.dataset.largeToolBody = "rendered";
    this.notifyTranscriptNodeHeightDidChange(body);
  },

  scrollTerm() {
    if (this.scrollFrame) return;
    const scroll = () => {
      const term = this.querySelector(".term");
      if (term) term.scrollTop = term.scrollHeight;
    };
    this.scrollFrame = window.requestAnimationFrame(() => {
      this.scrollFrame = undefined;
      scroll();
      window.requestAnimationFrame(scroll);
    });
  },
};
