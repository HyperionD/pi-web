export const sessionMenuMethods = {
  positionSessionMenu(row) {
    const menu = row?.querySelector(".session-menu");
    const button = row?.querySelector(".session-menu-button");
    if (!menu || !button) return;
    const buttonRect = button.getBoundingClientRect();
    const menuRect = menu.getBoundingClientRect();
    const margin = 8;
    const gap = 4;
    const menuWidth = menuRect.width || 132;
    const menuHeight = menuRect.height || 84;
    const viewportWidth = window.innerWidth || document.documentElement.clientWidth;
    const viewportHeight = window.innerHeight || document.documentElement.clientHeight;
    const maxLeft = Math.max(margin, viewportWidth - menuWidth - margin);
    const left = Math.min(Math.max(margin, buttonRect.right - menuWidth), maxLeft);
    const opensBelow = buttonRect.bottom + gap + menuHeight <= viewportHeight - margin;
    const top = opensBelow
      ? buttonRect.bottom + gap
      : Math.max(margin, buttonRect.top - menuHeight - gap);
    menu.style.position = "fixed";
    menu.style.left = `${Math.round(left)}px`;
    menu.style.top = `${Math.round(top)}px`;
    menu.style.right = "auto";
    menu.dataset.placement = opensBelow ? "below" : "above";
  },

  toggleSessionMenu(row) {
    if (!row) return;
    const menu = row.querySelector(".session-menu");
    const button = row.querySelector(".session-menu-button");
    const open = menu?.hidden;
    this.closeSessionMenus(row);
    menu?.toggleAttribute("hidden", !open);
    button?.setAttribute("aria-expanded", String(!!open));
    if (open) {
      this.positionSessionMenu(row);
      this.startSessionMenuPositionTracking(row);
    } else {
      this.stopSessionMenuPositionTracking();
    }
  },

  startSessionMenuPositionTracking(row) {
    this.stopSessionMenuPositionTracking();
    const reposition = () => {
      if (!row.isConnected || row.querySelector(".session-menu")?.hidden) {
        this.stopSessionMenuPositionTracking();
        return;
      }
      this.positionSessionMenu(row);
    };
    this.sessionMenuPositionRow = row;
    this.sessionMenuPositionHandler = reposition;
    window.addEventListener("resize", reposition);
    window.addEventListener("scroll", reposition, true);
  },

  stopSessionMenuPositionTracking() {
    const reposition = this.sessionMenuPositionHandler;
    if (!reposition) return;
    window.removeEventListener("resize", reposition);
    window.removeEventListener("scroll", reposition, true);
    this.sessionMenuPositionHandler = undefined;
    this.sessionMenuPositionRow = undefined;
  },

  closeSessionMenus(except) {
    if (!except || this.sessionMenuPositionRow !== except || except.querySelector(".session-menu")?.hidden) {
      this.stopSessionMenuPositionTracking();
    }
    this.querySelectorAll(".session-row").forEach((row) => {
      if (except && row === except) return;
      row.querySelector(".session-menu")?.setAttribute("hidden", "");
      row.querySelector(".session-menu-button")?.setAttribute("aria-expanded", "false");
    });
  },

  closeModals() {
    this.closeSessionMenus();
    this.closeGitDetail?.();
    this.closeFilePreview?.();
    this.closeSettingsModal?.();
    this.closeSkillManager?.();
    this.closeUserManagement?.();
  },

  closeUserManagement() {
    const dialogs = [...this.querySelectorAll("[data-user-management-dialog][open], [data-user-management-dialog] dialog[open]")];
    (dialogs.at(-1) as HTMLDialogElement | undefined)?.close("cancel");
  },
};
