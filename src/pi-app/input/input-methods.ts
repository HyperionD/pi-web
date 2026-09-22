import {
  cancelSession,
  compactSession,
  createSession,
  postPrompt,
  renameSession,
  runShellCommand,
  sendSessionCommand,
  steerSession,
} from "../../lib/api";
import { fallbackChoicePrompt, fallbackInputPrompt } from "./fallback-choices";
import { formatPromptPreset } from "./prompt-presets";
import { maxUploadBytesToMiB } from "../workspace/app-config-methods";
import {
  commandDescription,
  commandName,
  builtinSlashCommandError,
  commandValue,
  filterSlashCommands,
  getSlashQuery,
  mergeSlashCommands,
  parseBuiltinSlashCommand,
} from "./slash-commands";

const SLASH_COMMAND_TTL_MS = 30_000;

export const inputMethods = {
  async submitPrompt() {
    this.updatePrompt?.();
    const text = this.normalizePromptText(this.prompt?.value);
    const attachments = (this.attachmentContents || []).filter(Boolean);
    if (!this.canSendPrompt(text, attachments)) return;
    if (attachments.length === 0 && this.apiConnected && parseBuiltinSlashCommand(text)) {
      await this.executeBuiltinSlashCommand(text);
      return;
    }
    this.clearPromptDraft?.();
    if (this.running) {
      await this.submitSteeringPrompt(text);
      return;
    }
    const submissionKey = this.dataset.activeSessionId
      || `workspace:${this.dataset.activeWorkspaceId || ""}`;
    this.promptSubmittingKeys ??= new Set();
    if (this.promptSubmittingKeys.has(submissionKey)) return;
    this.promptSubmittingKeys.add(submissionKey);
    try {
      const workspaceId = this.dataset.activeWorkspaceId;
      let sessionId = this.dataset.activeSessionId;
      if (sessionId && workspaceId && !this.activeSessionBelongsToWorkspace(sessionId, workspaceId)) {
        sessionId = "";
      }
      if (!sessionId && this.apiConnected && workspaceId) {
        try {
          const { session } = await createSession(workspaceId);
          this.activateCreatedSession(workspaceId, session);
          sessionId = session.id;
        } catch {
          this.setConnection("err");
          return;
        }
      }
      this.showSessionMain();
      this.finalizeStreamingMessages();
      const waitForServerEcho = this.apiConnected && sessionId;
      const clientRequestId = waitForServerEcho
        ? (globalThis.crypto?.randomUUID?.() || `request-${Date.now()}-${Math.random().toString(36).slice(2)}`)
        : "";
      const useDurablePrompt = Boolean(waitForServerEcho);
      const hasVisiblePrompt = !!text || attachments.length > 0;
      this.writeLastSessionPrompt(sessionId, text);
      if (hasVisiblePrompt) {
        if (!waitForServerEcho) {
          this.appendMessage({ kind: "user", text, attachments });
        }
        this.appendLoadingMessage();
        if (text && !waitForServerEcho) this.autonameActiveSession(text);
      }
      if (this.prompt) this.prompt.value = "";
      this.attachmentContents = [];
      this.attachments?.replaceChildren();
      if (this.attachments) this.attachments.hidden = true;
      this.updatePrompt();
      if (waitForServerEcho) {
        try {
          await postPrompt(sessionId, text, attachments, clientRequestId);
        } catch (error) {
          this.notifyResponseFailure?.(error);
          this.setMode("idle");
          this.removeLoadingMessage();
          this.setConnection("err");
        }
      }
    } finally {
      this.promptSubmittingKeys.delete(submissionKey);
    }
  },

  async executeBuiltinSlashCommand(text) {
    const invocation = parseBuiltinSlashCommand(text);
    if (!invocation || !this.apiConnected) return false;
    if (this.running && !invocation.command.availableWhileRunning) return true;
    if (this.builtinCommandPending) return true;
    this.builtinCommandPending = true;
    try {
      const name = invocation.command.name;
      const usage = builtinSlashCommandError(name, invocation.args);
      if (usage) throw new Error(usage);
      const workspaceId = this.dataset.activeWorkspaceId;
      let sessionId = this.dataset.activeSessionId;
      if (sessionId && workspaceId && !this.activeSessionBelongsToWorkspace(sessionId, workspaceId)) {
        sessionId = "";
      }
      if (!sessionId && workspaceId) {
        try {
          const { session } = await createSession(workspaceId);
          this.activateCreatedSession(workspaceId, session);
          sessionId = session.id;
        } catch (error) {
          this.notifyResponseFailure?.(error);
          this.setConnection("err");
          return true;
        }
      }
      if (!sessionId) return true;
      const clientRequestId = globalThis.crypto?.randomUUID?.()
        || `command-${Date.now()}-${Math.random().toString(36).slice(2)}`;
      if (name === "compact") {
        await compactSession(sessionId, clientRequestId);
      } else if (name === "name") {
        await renameSession(sessionId, invocation.args, clientRequestId);
        this.updateSessionTitle?.({ id: sessionId, title: invocation.args });
      } else if (name === "reload") {
        await sendSessionCommand(sessionId, name, "", clientRequestId);
        await this.loadSession?.(sessionId);
        await this.loadSessionCommands?.(sessionId);
      } else if (name === "session") {
        const result = await sendSessionCommand(sessionId, name, "", clientRequestId);
        const usage = result?.contextUsage;
        this.renderSessionStats(result);
        const detail = usage?.tokens != null && usage?.contextWindow
          ? `上下文 ${usage.tokens}/${usage.contextWindow} tokens (${usage.percent ?? "—"}%)`
          : "会话统计已加载";
        this.showToast?.("success", detail);
      } else if (name === "copy") {
        const result = await sendSessionCommand(sessionId, name, "", clientRequestId);
        const textToCopy = result?.text || "";
        if (!textToCopy) throw new Error("没有可复制的助手消息");
        await navigator.clipboard.writeText(textToCopy);
      } else if (name === "clone") {
        const result = await sendSessionCommand(sessionId, name, "", clientRequestId);
        if (result?.cancelled || !result?.newSessionId) throw new Error("无法克隆当前会话");
        await this.refreshWorkspaces?.();
        const newSession = this.findSessionRow?.(result.newSessionId);
        if (newSession) await this.pickSession?.(newSession);
      }
      this.clearPromptInput();
      this.showToast?.("success", `/${name} 已完成`);
    } catch (error) {
      this.notifyResponseFailure?.(error);
      const message = error instanceof Error ? error.message : String(error);
      if (!message.startsWith("Usage:")) this.setConnection?.("err");
    } finally {
      this.builtinCommandPending = false;
    }
    return true;
  },

  clearPromptInput() {
    if (this.prompt) this.prompt.value = "";
    this.attachmentContents = [];
    this.attachments?.replaceChildren();
    if (this.attachments) this.attachments.hidden = true;
    this.updatePrompt?.();
  },

  hasSendableAttachments() {
    return (this.attachmentContents || []).some(Boolean);
  },

  activeSessionBelongsToWorkspace(sessionId, workspaceId) {
    const row = this.findSessionRow?.(sessionId)
      || [...this.querySelectorAll("[data-session]")].find((item) => item.dataset.session === sessionId);
    const rowWorkspace = row?.dataset.workspace;
    return !rowWorkspace || rowWorkspace === workspaceId;
  },

  async submitSteeringPrompt(text) {
    const normalizedText = this.normalizePromptText(text);
    const attachments = (this.attachmentContents || []).filter(Boolean);
    if (!this.canSendPrompt(normalizedText, attachments)) return;
    const sessionId = this.dataset.activeSessionId;
    if (!sessionId || !this.apiConnected) return;
    this.writeLastSessionPrompt(sessionId, normalizedText);
    try {
      await steerSession(sessionId, normalizedText, attachments);
      if (this.prompt) this.prompt.value = "";
      this.attachmentContents = [];
      this.attachments?.replaceChildren();
      if (this.attachments) this.attachments.hidden = true;
      this.updatePrompt();
    } catch {
      this.setConnection("err");
      // A rejected steer usually means the run already ended; re-read the
      // durable projection instead of leaving the composer stuck on "running".
      await this.resyncCancelledSession(sessionId);
    }
  },

  async submitShellCommand(event) {
    event.preventDefault();
    const input = event.currentTarget.querySelector("input[name='command']");
    const command = input?.value.trim();
    const workspaceId = this.dataset.activeWorkspaceId;
    if (!command || !workspaceId || !this.apiConnected) return;
    const button = event.currentTarget.querySelector("button[type='submit']");
    if (button) button.disabled = true;
    this.showSessionMain();
    this.commitStreamingMessages();
    this.appendMessage({
      kind: "tool",
      tool: "shell",
      args: `$ ${command}`,
      status: "running",
      collapsedByDefault: false,
    });
    try {
      const result = await runShellCommand(workspaceId, command);
      this.finishTool({
        kind: "tool",
        tool: "shell",
        args: `$ ${command}`,
        status: result.exitCode === 0 ? "ok" : "err",
        durationMs: result.durationMs,
        resultMeta: result.exitCode === 0 ? "done" : `exit ${result.exitCode}`,
        body: result.output || "[no output]",
      });
      if (input) input.value = "";
      void this.loadRuntimeStatus?.(workspaceId);
      void this.loadWorkspaceMeta?.(workspaceId);
    } catch (error) {
      this.finishTool({
        kind: "tool",
        tool: "shell",
        args: `$ ${command}`,
        status: "err",
        resultMeta: error instanceof Error ? error.message : String(error),
        body: error instanceof Error ? error.message : String(error),
      });
      this.setConnection("err");
    } finally {
      if (button) button.disabled = false;
    }
  },

  async cancelActiveSession() {
    const sessionId = this.dataset.activeSessionId;
    if (!sessionId || !this.apiConnected) return;
    this.markSessionCancellationPending(sessionId);
    try {
      const result = await cancelSession(sessionId);
      if (result?.cancelled) return;
      this.clearSessionCancellationPending(sessionId);
    } catch {
      this.clearSessionCancellationPending(sessionId);
      this.setConnection("err");
    }
    // The run may have finished already, or the cancel request may have failed.
    // Re-read the durable projection so the UI never stays stuck on "running".
    await this.resyncCancelledSession(sessionId);
  },

  async resyncCancelledSession(sessionId) {
    if (this.dataset.activeSessionId !== sessionId) return;
    await this.requestSessionResync(sessionId);
  },

  handleAppClick(event) {
    const removeAttachmentButton = event.target.closest("[data-remove-attachment]");
    if (removeAttachmentButton) {
      const chip = removeAttachmentButton.closest(".attach-chip");
      const index = Number(chip?.dataset.attachmentIndex);
      if (Number.isInteger(index)) this.attachmentContents[index] = "";
      chip?.remove();
      this.setAttachmentWarning?.("");
      this.updatePrompt();
      return;
    }
    const actionTarget = event.target.closest("[data-action]");
    const button = event.target.closest("button");
    if ((!button && !actionTarget) || !this.contains(button || actionTarget)) return;
    const action = actionTarget?.dataset.action || button?.dataset.action;
    if (["folder-toggle", "folder-select", "folder-open-current", "folder-close"].includes(action)) {
      event.stopPropagation?.();
    }
    if (action === "copy-code") {
      void this.copyCodeBlock(button);
      return;
    }
    if (action === "route-picker") {
      this.route("workspace");
      const popover = this.querySelector("[data-folder-popover]");
      if (popover && !popover.hidden) this.closeFolderPopover();
      else void this.openFolderPopover("workspace", event);
    }
    if (action === "route-workspace") {
      this.restoreAllowedRootPicker?.();
      this.route("workspace");
    }
    if (action === "refresh-workspaces") this.refreshWorkspaces();
    if (action === "browse-folder") this.browseFolder();
    if (action === "folder-open") {
      const popover = this.querySelector("[data-folder-popover]");
      if (popover && !popover.hidden) this.closeFolderPopover();
      else void this.openFolderPopover("workspace", event);
    }
    if (action === "folder-close") this.closeFolderPopover();
    const folderPickerTarget = actionTarget?.closest("[data-folder-browser], [data-folder-popover]");
    if (action === "folder-toggle" && folderPickerTarget && !this.folderLoadPending) {
      void this.toggleFolder(actionTarget.dataset.path);
    }
    if (action === "folder-select" && folderPickerTarget && !this.folderLoadPending) {
      this.selectFolder(actionTarget.dataset.path);
    }
    if (action === "folder-open-current" && folderPickerTarget && !this.folderLoadPending && !this.folderOpeningPending) {
      if (this.folderBrowsePurpose === "allowed-root") {
        this.addAllowedRootFromFolder?.(this.currentFolder);
        this.closeFolderPopover();
      } else {
        const openingToken = Symbol("folder-opening");
        const openingPath = this.currentFolder;
        this.folderOpeningToken = openingToken;
        this.setFolderOpening(true);
        let opening;
        try {
          opening = this.openWorkspacePath(openingPath, openingToken);
        } catch (error) {
          if (this.folderOpeningToken !== openingToken) return;
          if (!this.recoverForbiddenWorkspace?.(error)) {
            this.renderFolderError(error);
            this.setConnection("err");
            this.setFolderOpening(false);
          }
          return;
        }
        void Promise.resolve(opening)
          .then(() => {
            if (this.folderOpeningToken === openingToken) this.closeFolderPopover();
          })
          .catch((error) => {
            if (this.folderOpeningToken !== openingToken) return;
            if (!this.recoverForbiddenWorkspace?.(error)) {
              this.renderFolderError(error);
              this.setConnection("err");
            }
          })
          .finally(() => {
            if (this.folderOpeningToken !== openingToken) return;
            this.setFolderOpening(false);
          });
      }
    }
    if (action === "toggle-tree") this.toggleTree();
    if (action === "show-file-tree") this.showFileTreePanel?.();
    if (action === "show-git-history") this.showGitHistory?.();
    if (action === "load-more-git-history") this.loadMoreGitHistory?.();
    if (action === "select-git-commit") this.selectGitCommit?.(actionTarget.dataset.hash);
    if (action === "close-git-detail") this.closeGitDetail?.();
    if (action === "refresh-tree") {
      this.refreshTree();
      this.refreshGitHistory?.();
    }
    if (action === "tree-root-menu") {
      const rect = button?.getBoundingClientRect?.();
      window.dispatchEvent(new CustomEvent("pi-workspace-tree:root-menu", {
        detail: { x: rect?.left || 24, y: rect?.bottom || 88 },
      }));
    }
    if (action === "open-file") this.openFile(actionTarget);
    if (action === "close-file-preview") this.closeFilePreview();
    if (action === "download-file-preview") this.downloadFilePreview?.();
    if (action === "toggle-file-preview-mode") this.toggleFilePreviewMode();
    if (action === "save-file-preview") this.saveFilePreview();
    if (action === "collapse-sidebar") this.collapseSidebar(true);
    if (action === "expand-sidebar") this.collapseSidebar(false);
    if (action === "open-drawer") this.toggleDrawer();
    if (action === "close-drawer") this.toggleDrawer(false);
    if (action === "toggle-tool") this.toggleTool(button);
    if (action === "show-full-tool-output") this.showFullToolOutput(actionTarget);
    if (action !== "session-menu-toggle") this.closeSessionMenus(actionTarget?.closest(".session-row"));
    if (action === "toggle-workspace") this.toggleWorkspace(button.dataset.workspace);
    if (action === "close-workspace") this.closeWorkspace(actionTarget.dataset.workspace);
    if (action === "delete-workspace-sessions") this.deleteWorkspaceSessions(button.dataset.workspace);
    if (action === "new-session") this.newSession(button.dataset.workspace);
    if (action === "session-menu-toggle") this.toggleSessionMenu(actionTarget.closest(".session-row"));
    if (action === "rename-session") this.renameSession(actionTarget.closest(".session-row")?.dataset.session);
    if (action === "delete-session") this.deleteSession(actionTarget.closest(".session-row")?.dataset.session);
    if (action === "fallback-choice") {
      this.submitFallbackChoice(
        actionTarget.dataset.choiceId,
        actionTarget.dataset.choiceValue,
        actionTarget.closest(".fallback-choice-list"),
      );
    }
    if (action === "fallback-choice-custom") {
      this.submitFallbackChoice(
        actionTarget.dataset.choiceId,
        actionTarget.closest(".choice-custom")?.querySelector("[data-choice-custom-input]")?.value,
        actionTarget.closest(".fallback-choice-list"),
      );
    }
    if (action === "fallback-input-submit") {
      this.submitFallbackInput(
        actionTarget.dataset.inputId,
        actionTarget.closest(".fallback-input-panel")?.querySelector("[data-fallback-input]")?.value,
        actionTarget.closest(".fallback-input-panel"),
      );
    }
    if (action === "open-prompt-presets") {
      void this.openPromptPresetManager?.();
      return;
    }
    if (action === "prompt-preset") {
      const preset = (this.promptPresets || []).find((item) => item.id === actionTarget?.dataset.promptPresetId);
      if (preset?.enabled) this.fillPrompt(formatPromptPreset(preset));
    }
    if (action === "open-settings") this.openSettingsModal?.();
    if (action === "close-settings") this.closeSettingsModal?.();
    if (action === "close-session-stats") this.closeSessionStats();
    if (action === "compact-session") void this.compactActiveSession?.();
    if (action === "save-settings") this.saveSettingsForm?.(event);
    if (action === "save-auth-provider") this.saveAuthForm?.(event);
    if (action === "logout-auth-provider") this.logoutAuthProvider?.();
    if (action === "start-oauth-login") this.startOAuthLogin?.();
    if (action === "send-oauth-input") this.sendOAuthInput?.();
    if (action === "close-tweaks") this.querySelector("[data-tweaks]")?.setAttribute("hidden", "");
    if (!actionTarget?.closest(".session-menu") && action !== "session-menu-toggle" && button?.dataset.session) {
      this.pickSession(button.closest(".session-row") || button);
    }
    if (button?.dataset.workspace && button.classList.contains("recent-row")) {
      this.openWorkspace(button.dataset.workspace);
    }
    if (button?.dataset.seed) this.fillPrompt(button.dataset.seed);
    if (button?.dataset.skill) this.fillPrompt(`/skill ${button.dataset.skill}\n\n`);
    if (button?.dataset.slash) this.pickSlash(button.dataset.slash);
  },

  async submitFallbackChoice(choiceId, value, panel) {
    await this.submitFallbackResponse(fallbackChoicePrompt(choiceId, value), panel);
  },

  async submitFallbackInput(inputId, value, panel) {
    await this.submitFallbackResponse(fallbackInputPrompt(inputId, value), panel);
  },

  async submitFallbackResponse(prompt, panel) {
    if (!prompt || !this.apiConnected || !this.dataset.activeSessionId) return;
    panel?.classList.add("answered");
    panel?.querySelectorAll("button, input, textarea").forEach((item) => item.disabled = true);
    this.finalizeStreamingMessages();
    this.appendLoadingMessage();
    const sessionId = this.dataset.activeSessionId;
    const clientRequestId = globalThis.crypto?.randomUUID?.()
      || `request-${Date.now()}-${Math.random().toString(36).slice(2)}`;
    try {
      await postPrompt(sessionId, prompt, [], clientRequestId);
    } catch (error) {
      this.notifyResponseFailure?.(error);
      this.setMode("idle");
      this.removeLoadingMessage();
      this.setConnection("err");
    }
  },

  renderSessionStats(result) {
    const panel = this.querySelector("[data-session-stats]");
    if (!panel) return;
    const usage = result?.contextUsage || {};
    const formatCount = (value) => value == null || !Number.isFinite(Number(value))
      ? "—" : Number(value).toLocaleString("en-US");
    const context = usage.tokens != null && usage.contextWindow != null
      ? `${formatCount(usage.tokens)} / ${formatCount(usage.contextWindow)} tokens (${usage.percent ?? "—"}%)`
      : "—";
    const messages = result?.totalMessages ?? result?.messages;
    const tools = result?.toolCalls ?? result?.tools;
    const cost = result?.cost == null || !Number.isFinite(Number(result.cost))
      ? "—" : `$${Number(result.cost).toFixed(4)}`;
    const setStat = (name, value, available) => {
      const stat = panel.querySelector(`[data-session-stat='${name}']`);
      if (!stat) return;
      stat.querySelector("strong").textContent = value;
      stat.toggleAttribute("hidden", !available);
    };
    setStat("context", context, context !== "—");
    setStat("messages", formatCount(messages), messages != null);
    setStat("tools", formatCount(tools), tools != null);
    setStat("cost", cost, cost !== "—");
    panel.removeAttribute("hidden");
  },

  closeSessionStats() {
    this.querySelector("[data-session-stats]")?.setAttribute("hidden", "");
  },

  updatePrompt() {
    if (!this.prompt || !this.sendButton) return;
    const value = this.prompt.value;
    const text = this.normalizePromptText(value);
    const hasWorkspace = Boolean(this.dataset.activeWorkspaceId);
    this.prompt.disabled = !hasWorkspace;
    this.savePromptDraft?.();
    const attachments = (this.attachmentContents || []).filter(Boolean);
    const canSend = hasWorkspace && this.canSendPrompt(text, attachments);
    this.updateAttachmentGuidance?.();
    this.sendButton.disabled = !canSend;
    this.sendButton.setAttribute("aria-disabled", canSend ? "false" : "true");
    this.sendButton.classList.toggle("is-disabled", !canSend);
    const slashQuery = getSlashQuery(value);
    if (slashQuery !== null && hasWorkspace && this.apiConnected && !this.slashCommandsLoading
      && Date.now() - (this.slashCommandsLoadedAt || 0) > SLASH_COMMAND_TTL_MS) {
      void this.loadWorkspaceCommands?.(this.dataset.activeWorkspaceId);
    }
    this.slashPopover?.toggleAttribute("hidden", slashQuery === null);
    this.prompt.setAttribute("aria-expanded", String(slashQuery !== null));
    this.filterSlash(value);
    this.prompt.style.height = "auto";
    this.prompt.style.height = Math.min(180, this.prompt.scrollHeight) + "px";
  },

  updateAttachmentGuidance() {
    const guidance = this.querySelector("[data-attachment-guidance]");
    if (!guidance) return;
    const maxUploadMiB = maxUploadBytesToMiB(this.appConfigState?.config?.workspace?.maxUploadBytes);
    guidance.textContent = `对话附件有限制；较大文件请先上传到工作区。工作区单文件上传上限 ${maxUploadMiB} MiB；普通文件超过 256 KiB 不会内联。图片及合计请求预算约 768 KiB。`;
  },

  fillPrompt(value) {
    if (!this.prompt) return;
    this.prompt.value = value;
    this.updatePrompt();
    this.prompt.focus();
  },

  setSlashCommandStatus(status = "") {
    const head = this.querySelector("[data-slash-head]");
    if (!head) return;
    head.textContent = status ? `slash commands · ${status}` : "slash commands · type to filter";
  },

  renderSlashCommands(commands = []) {
    const list = this.querySelector(".slash-list");
    if (!list) return;
    this.slashCommands = mergeSlashCommands(commands);
    list.replaceChildren();
    const groups = new Map();
    for (const command of this.slashCommands) {
      const source = command.source || "command";
      if (!groups.has(source)) groups.set(source, []);
      groups.get(source).push(command);
    }
    const sourceOrder = ["builtin", "extension", "prompt", "skill"];
    const orderedGroups = [...groups.entries()].sort(([left], [right]) => {
      const leftIndex = sourceOrder.indexOf(left);
      const rightIndex = sourceOrder.indexOf(right);
      return (leftIndex < 0 ? 99 : leftIndex) - (rightIndex < 0 ? 99 : rightIndex);
    });
    let commandIndex = 0;
    for (const [source, groupCommands] of orderedGroups) {
      const group = document.createElement("section");
      group.className = "slash-group";
      group.dataset.source = source;
      group.setAttribute("role", "group");
      group.setAttribute("aria-label", source);
      group.innerHTML = `<div class="slash-group-head"><span></span><span class="slash-group-count"></span></div>`;
      group.querySelector(".slash-group-head span").textContent = source;
      group.querySelector(".slash-group-count").textContent = String(groupCommands.length);
      for (const command of groupCommands) {
        const item = document.createElement("button");
        item.type = "button";
        item.className = "slash-item";
        item.setAttribute("role", "option");
        item.setAttribute("aria-selected", "false");
        item.id = `slash-command-${commandIndex++}`;
        item.dataset.slash = commandValue(command);
        item.dataset.commandName = commandName(command);
        const scope = command.scope || command.location || "global";
        item.innerHTML = [
          `<span class="sl-cmd"></span>`,
          `<span class="sl-tags"><span class="sl-scope"></span><span class="sl-source"></span></span>`,
          `<span class="sl-desc"></span>`,
        ].join("");
        item.querySelector(".sl-cmd").textContent = commandValue(command);
        item.querySelector(".sl-scope").textContent = scope;
        item.querySelector(".sl-source").textContent = source;
        item.querySelector(".sl-desc").textContent = commandDescription(command);
        group.append(item);
      }
      list.append(group);
    }
    const empty = document.createElement("div");
    empty.className = "slash-empty";
    empty.textContent = "no slash commands found";
    empty.hidden = true;
    list.append(empty);
    this.setSlashCommandStatus();
    this.filterSlash();
  },

  filterSlash(value = this.prompt?.value || "") {
    const query = getSlashQuery(value);
    const items = [...this.querySelectorAll(".slash-item")];
    const empty = this.querySelector(".slash-empty");
    if (query === null) {
      items.forEach((item) => {
        item.hidden = true;
        item.classList.remove("selected");
        item.setAttribute("aria-selected", "false");
      });
      if (empty) empty.hidden = true;
      this.prompt?.removeAttribute("aria-activedescendant");
      return;
    }
    const commands = this.slashCommands || items.map((item) => ({
      name: item.dataset.commandName || item.dataset.slash?.replace(/^\//, ""),
      command: item.dataset.slash,
      description: item.querySelector(".sl-desc")?.textContent || item.textContent,
    }));
    const matchingCommands = filterSlashCommands(commands, query, Boolean(this.running));
    const matchingNames = new Set(matchingCommands.map((command) => commandName(command).toLowerCase()));
    items.forEach((item) => {
      const itemName = item.dataset.commandName || item.dataset.slash?.replace(/^\//, "") || "";
      const match = matchingNames.has(itemName.toLowerCase());
      item.hidden = !match;
      item.classList.toggle("selected", false);
      item.setAttribute("aria-selected", "false");
    });
    const matchingOrder = new Map(
      matchingCommands.map((command, index) => [commandName(command).toLowerCase(), index]),
    );
    const list = this.querySelector(".slash-list");
    const groups = [...this.querySelectorAll(".slash-group")];
    const groupRank = (group) => Math.min(
      ...[...group.querySelectorAll(".slash-item")].map((item) => (
        matchingOrder.get((item.dataset.commandName || "").toLowerCase()) ?? 99
      )),
    );
    groups.sort((left, right) => groupRank(left) - groupRank(right));
    groups.forEach((group) => {
      const groupItems = [...group.querySelectorAll(".slash-item")];
      groupItems.sort((left, right) => (
        (matchingOrder.get((left.dataset.commandName || "").toLowerCase()) ?? 99)
        - (matchingOrder.get((right.dataset.commandName || "").toLowerCase()) ?? 99)
      ));
      groupItems.forEach((item) => group.append(item));
      const hasVisibleItem = groupItems.some((item) => !item.hidden);
      group.toggleAttribute("hidden", !hasVisibleItem);
      list?.append(group);
    });
    const first = [...this.querySelectorAll(".slash-item")].find((item) => !item.hidden);
    if (empty) empty.hidden = Boolean(first);
    first?.classList.add("selected");
    first?.setAttribute("aria-selected", "true");
    if (first?.id) this.prompt?.setAttribute("aria-activedescendant", first.id);
    else this.prompt?.removeAttribute("aria-activedescendant");
  },

  pickSlash(command) {
    this.fillPrompt(command + " ");
    this.slashPopover?.setAttribute("hidden", "");
  },

  navigateList(event, selector, onSelect) {
    const items = [...this.querySelectorAll(selector)].filter((item) => !item.hidden);
    if (!items.length) return;
    event.preventDefault();
    let index = Math.max(0, items.findIndex((item) => item.classList.contains("selected")));
    if (event.key === "ArrowDown") index = Math.min(items.length - 1, index + 1);
    if (event.key === "ArrowUp") index = Math.max(0, index - 1);
    items.forEach((item) => {
      item.classList.remove("selected");
      item.setAttribute("aria-selected", "false");
    });
    items[index].classList.add("selected");
    items[index].setAttribute("aria-selected", "true");
    if (items[index].id) this.prompt?.setAttribute("aria-activedescendant", items[index].id);
    items[index].scrollIntoView?.({ block: "nearest" });
    if (event.key === "Enter" || event.key === "Tab") onSelect(items[index]);
  },

};
