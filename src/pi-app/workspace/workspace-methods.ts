import { gitHistoryMethods } from "./git-history-methods";
import { workspaceBootstrapMethods } from "./workspace-bootstrap-methods";
import { workspaceFolderMethods } from "./workspace-folder-methods";
import { workspaceRenderMethods } from "./workspace-render-methods";

export function requestErrorMessage(error: unknown) {
  return error instanceof Error ? error.message : String(error);
}

export function isForbiddenWorkspaceError(error: unknown) {
  const status = (error as any)?.status ?? (error as any)?.response?.status;
  if (status === 403) return true;
  return /(?:^|\D)403(?:\D|$)|forbidden|outside allowed roots|not allowed/i.test(requestErrorMessage(error));
}

export const workspaceMethods = {
  ...workspaceBootstrapMethods,
  ...workspaceRenderMethods,
  ...workspaceFolderMethods,
  ...gitHistoryMethods,
};
