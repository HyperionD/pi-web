import { compactSession } from "../../lib/api";

export const compactionMethods = {
  syncCompactionControl() {
    const button = this.querySelector("[data-action='compact-session']");
    if (!button) return;
    const requestPending = this.compactionRequest?.sessionId === this.dataset.activeSessionId;
    const compacting = this.compacting || requestPending;
    const disabled = !this.dataset.activeSessionId || !this.apiConnected || this.running || compacting;
    button.disabled = disabled;
    button.textContent = compacting ? "compacting…" : "compact";
    button.setAttribute("aria-label", compacting ? "compacting context" : "compact context");
    button.title = compacting ? "compacting context" : "compact context";
  },

  async compactActiveSession() {
    const sessionId = this.dataset.activeSessionId;
    if (!sessionId || !this.apiConnected || this.running || this.compacting) return;
    const requestToken = Symbol(sessionId);
    this.compactionRequest = { sessionId, token: requestToken };
    this.compacting = true;
    this.syncCompactionControl();
    try {
      await compactSession(sessionId);
      if (this.dataset.activeSessionId === sessionId) await this.loadSession(sessionId);
    } catch {
      this.setConnection("err");
    } finally {
      if (this.compactionRequest?.token === requestToken) {
        this.compactionRequest = undefined;
        this.compacting = false;
      }
      this.syncCompactionControl();
    }
  },
};
