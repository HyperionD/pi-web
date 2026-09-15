import {
  getSessionContextUsage,
  getWorkspaceRuntimeModel,
  getWorkspaceRuntimeQuota,
  getWorkspaceRuntimeStatus,
} from "../../lib/api";

export const runtimeStatusMethods = {
  async loadRuntimeStatus(workspaceId = this.dataset.activeWorkspaceId, options: any = {}) {
    if (!workspaceId || !this.apiConnected) return;
    const requestToken = Symbol(`runtime-status:${workspaceId}`);
    const expectedBootstrapGeneration = options.bootstrapGeneration || this.bootstrapGeneration;
    const expectedOpenGeneration = this.workspaceOpenGeneration;
    const sessionId = this.dataset.activeSessionId || "";
    this.runtimeStatusRequestToken = requestToken;
    const isCurrent = () => (
      this.runtimeStatusRequestToken === requestToken
      && this.dataset.activeWorkspaceId === workspaceId
      && (!expectedBootstrapGeneration || this.bootstrapGeneration === expectedBootstrapGeneration)
      && this.workspaceOpenGeneration === expectedOpenGeneration
    );
    const contextUsagePromise = this.loadContextUsage(sessionId, {
      runtimeRequestToken: requestToken,
      workspaceId,
      bootstrapGeneration: expectedBootstrapGeneration,
      openGeneration: expectedOpenGeneration,
    });
    try {
      const { status } = await getWorkspaceRuntimeModel(workspaceId, sessionId);
      if (!isCurrent()) return;
      this.applyRuntimeStatus(status);
      void this.loadRuntimeQuota(workspaceId, status?.model, {
        runtimeRequestToken: requestToken,
        bootstrapGeneration: expectedBootstrapGeneration,
        openGeneration: expectedOpenGeneration,
      });
    } catch {
      if (!isCurrent()) return;
      try {
        const { status } = await getWorkspaceRuntimeStatus(workspaceId, sessionId);
        if (!isCurrent()) return;
        this.applyRuntimeStatus(status);
      } catch {}
    }
    await contextUsagePromise;
  },

  async loadContextUsage(sessionId = this.dataset.activeSessionId, options: any = {}) {
    if (!this.apiConnected) return;
    const hasOpenGeneration = Object.prototype.hasOwnProperty.call(options, "openGeneration");
    const isCurrent = () => (
      (!options.runtimeRequestToken || this.runtimeStatusRequestToken === options.runtimeRequestToken)
      && (!options.workspaceId || this.dataset.activeWorkspaceId === options.workspaceId)
      && (!options.bootstrapGeneration || this.bootstrapGeneration === options.bootstrapGeneration)
      && (!hasOpenGeneration || this.workspaceOpenGeneration === options.openGeneration)
    );
    if (!sessionId) {
      if (isCurrent()) this.applyRuntimeStatus({ contextUsage: null });
      return;
    }
    if (this.durableEvents) {
      if (isCurrent() && this.dataset.activeSessionId === sessionId) {
        this.applyRuntimeStatus({ contextUsage: this.transcriptState?.contextUsage || null });
      }
      return;
    }
    try {
      const { contextUsage } = await getSessionContextUsage(sessionId);
      if (isCurrent() && this.dataset.activeSessionId === sessionId) {
        this.applyRuntimeStatus({ contextUsage: contextUsage || null });
      }
    } catch {}
  },

  async loadRuntimeQuota(workspaceId = this.dataset.activeWorkspaceId, model = this.runtimeStatus?.model, options: any = {}) {
    if (!workspaceId || !this.apiConnected) return;
    const requestToken = options.runtimeRequestToken || this.runtimeStatusRequestToken;
    const hasOpenGeneration = Object.prototype.hasOwnProperty.call(options, "openGeneration");
    const isCurrent = () => (
      (!requestToken || this.runtimeStatusRequestToken === requestToken)
      && this.dataset.activeWorkspaceId === workspaceId
      && (!options.bootstrapGeneration || this.bootstrapGeneration === options.bootstrapGeneration)
      && (!hasOpenGeneration || this.workspaceOpenGeneration === options.openGeneration)
    );
    try {
      const { status } = await getWorkspaceRuntimeQuota(workspaceId, model || "");
      if (isCurrent()) this.applyRuntimeStatus(status);
    } catch {}
  },

  applyRuntimeStatus(status) {
    if (!status) return;
    this.updatePromptMeta(status);
    if (status.warning) this.notifyRuntimeWarning?.(status.warning);
  },
};
