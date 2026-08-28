import {
  getSessionContextUsage,
  getWorkspaceRuntimeModel,
  getWorkspaceRuntimeQuota,
  getWorkspaceRuntimeStatus,
} from "../../lib/api";

export const runtimeStatusMethods = {
  async loadRuntimeStatus(workspaceId = this.dataset.activeWorkspaceId) {
    if (!workspaceId || !this.apiConnected) return;
    const contextUsagePromise = this.loadContextUsage();
    try {
      const { status } = await getWorkspaceRuntimeModel(workspaceId);
      this.applyRuntimeStatus(status);
      void this.loadRuntimeQuota(workspaceId, status?.model);
    } catch {
      try {
        const { status } = await getWorkspaceRuntimeStatus(workspaceId);
        this.applyRuntimeStatus(status);
      } catch {}
    }
    await contextUsagePromise;
  },

  async loadContextUsage(sessionId = this.dataset.activeSessionId) {
    if (!this.apiConnected) return;
    if (!sessionId) {
      this.applyRuntimeStatus({ contextUsage: null });
      return;
    }
    try {
      const { contextUsage } = await getSessionContextUsage(sessionId);
      if (this.dataset.activeSessionId === sessionId) {
        this.applyRuntimeStatus({ contextUsage: contextUsage || null });
      }
    } catch {}
  },

  async loadRuntimeQuota(workspaceId = this.dataset.activeWorkspaceId, model = this.runtimeStatus?.model) {
    if (!workspaceId || !this.apiConnected) return;
    try {
      const { status } = await getWorkspaceRuntimeQuota(workspaceId, model || "");
      this.applyRuntimeStatus(status);
    } catch {}
  },

  applyRuntimeStatus(status) {
    if (!status) return;
    this.updatePromptMeta(status);
    if (status.warning) this.notifyRuntimeWarning?.(status.warning);
  },
};
