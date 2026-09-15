import { compactSession } from "../../lib/api";

export const compactionMethods = {
  syncCompactionControl() {
    const button = this.querySelector("[data-action='compact-session']");
    if (!button) return;
    const requestPending = this.compactionRequest?.sessionId === this.dataset.activeSessionId;
    const compacting = this.compacting || requestPending;
    const disabled = !this.dataset.activeSessionId || !this.apiConnected || this.running || compacting;
    button.disabled = disabled;
    button.textContent = compacting ? "压缩中…" : "压缩上下文";
    button.setAttribute("aria-label", compacting ? "正在压缩上下文" : "压缩上下文");
    button.title = compacting ? "正在压缩上下文" : "压缩上下文";
  },

  async compactActiveSession() {
    const sessionId = this.dataset.activeSessionId;
    if (!sessionId || !this.apiConnected || this.running || this.compacting) return;
    const requestToken = Symbol(sessionId);
    this.compactionRequest = { sessionId, token: requestToken };
    if (!this.durableEvents) this.compacting = true;
    this.syncCompactionControl();
    try {
      await compactSession(sessionId);
      if (!this.durableEvents && this.dataset.activeSessionId === sessionId) await this.loadSession(sessionId);
    } catch {
      this.setConnection("err");
    } finally {
      if (this.compactionRequest?.token === requestToken) {
        this.compactionRequest = undefined;
        if (!this.durableEvents) this.compacting = false;
      }
      this.syncCompactionControl();
    }
  },
};
