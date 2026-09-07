import React, { useEffect, useMemo, useRef, useState } from "react";
import { Tree } from "react-arborist";
import {
  createWorkspaceFile,
  deleteWorkspaceFile,
  downloadWorkspaceFile,
  renameWorkspaceFile,
  searchWorkspaceFiles,
  uploadWorkspaceFile,
} from "../lib/api";
import { formatTransferBytes, transferPercent } from "../lib/file-transfer";
import { decorateFileTree, type FileTreeNode, type GitStatusMap, type RawFileNode } from "../lib/file-tree-model";
import { resolveMaterialFileIcon } from "../lib/material-file-icons";

type WorkspaceTreeUpdate = { files?: RawFileNode[]; statusMap?: GitStatusMap; selectedPath?: string };
type Props = { initialFiles?: RawFileNode[]; initialStatusMap?: GitStatusMap };
type MenuTarget = { x: number; y: number; path: string; kind: "file" | "dir" | "root" };
type TransferOperation = "upload" | "download";
type TransferState =
  | { kind: "idle" }
  | { kind: "progress"; operation: TransferOperation; filename: string; loaded: number; total?: number }
  | { kind: "success"; operation: TransferOperation; filename: string }
  | { kind: "error"; operation: TransferOperation; filename: string; message: string };

const ROW_HEIGHT = 38;
const DEFAULT_HEIGHT = 480;

export default function WorkspaceFileTree({ initialFiles = [], initialStatusMap = {} }: Props) {
  const [files, setFiles] = useState<RawFileNode[]>(initialFiles);
  const [statusMap, setStatusMap] = useState<GitStatusMap>(initialStatusMap);
  const [selectedPath, setSelectedPath] = useState("");
  const [expandedPaths, setExpandedPaths] = useState<Set<string>>(() => new Set());
  const [height, setHeight] = useState(DEFAULT_HEIGHT);
  const [menu, setMenu] = useState<MenuTarget | null>(null);
  const [searchTerm, setSearchTerm] = useState("");
  const [contentMatches, setContentMatches] = useState<Set<string>>(() => new Set());
  const [transferState, setTransferState] = useState<TransferState>({ kind: "idle" });
  const treeArea = useRef<HTMLDivElement>(null);
  const uploadInput = useRef<HTMLInputElement>(null);
  const uploadTarget = useRef<MenuTarget | null>(null);

  useEffect(() => {
    const onUpdate = (event: Event) => {
      const detail = (event as CustomEvent<WorkspaceTreeUpdate>).detail || {};
      setFiles(detail.files || []);
      setStatusMap(detail.statusMap || {});
      setSelectedPath(detail.selectedPath || "");
    };
    const onRootMenu = (event: Event) => {
      const detail = (event as CustomEvent<{ x?: number; y?: number }>).detail || {};
      setMenu({ x: detail.x || 24, y: detail.y || 88, path: "", kind: "root" });
    };
    window.addEventListener("pi-workspace-tree:update", onUpdate);
    window.addEventListener("pi-workspace-tree:root-menu", onRootMenu);
    return () => {
      window.removeEventListener("pi-workspace-tree:update", onUpdate);
      window.removeEventListener("pi-workspace-tree:root-menu", onRootMenu);
    };
  }, []);

  useEffect(() => {
    const container = treeArea.current;
    if (!container || typeof ResizeObserver === "undefined") return;
    const resize = () => setHeight(Math.max(120, Math.floor(container.getBoundingClientRect().height || DEFAULT_HEIGHT)));
    resize();
    const observer = new ResizeObserver(resize);
    observer.observe(container);
    return () => observer.disconnect();
  }, []);

  useEffect(() => {
    if (!menu) return;
    const close = () => setMenu(null);
    window.addEventListener("click", close);
    window.addEventListener("keydown", close);
    return () => {
      window.removeEventListener("click", close);
      window.removeEventListener("keydown", close);
    };
  }, [menu]);

  useEffect(() => {
    const query = searchTerm.trim();
    if (!query) {
      setContentMatches(new Set());
      return;
    }
    const workspaceId = activeWorkspaceId();
    if (!workspaceId) return;
    let cancelled = false;
    const timeout = window.setTimeout(() => {
      void searchWorkspaceFiles(workspaceId, query)
        .then((result) => {
          if (!cancelled) setContentMatches(new Set(result.matches || []));
        })
        .catch(() => {
          if (!cancelled) setContentMatches(new Set());
        });
    }, 200);
    return () => {
      cancelled = true;
      window.clearTimeout(timeout);
    };
  }, [searchTerm]);

  const data = useMemo(
    () => decorateFileTree(files, statusMap, selectedPath, expandedPaths),
    [expandedPaths, files, selectedPath, statusMap],
  );

  const contextValue = useMemo(() => ({ setMenu }), []);

  const onEmptyContextMenu = (event: React.MouseEvent<HTMLDivElement>) => {
    if ((event.target as Element).closest?.(".tree-node")) return;
    event.preventDefault();
    setMenu({ x: event.clientX, y: event.clientY, path: "", kind: "root" });
  };

  return (
    <TreeContext.Provider value={contextValue}>
      <div className="tree-arborist" data-testid="workspace-file-tree" onContextMenu={onEmptyContextMenu}>
        <label className="tree-search" aria-label="搜索全部文件">
          <span>搜索</span>
          <input
            type="search"
            placeholder="搜索全部内容"
            value={searchTerm}
            onChange={(event) => setSearchTerm(event.currentTarget.value)}
          />
        </label>
        <div ref={treeArea} className="tree-arborist-body">
          {!files.length ? (
            <div className="tree-empty">文件树正在从后端加载。</div>
          ) : data.length ? (
            <Tree<FileTreeNode>
              data={data}
              width="100%"
              height={height}
              rowHeight={ROW_HEIGHT}
              indent={14}
              overscanCount={8}
              idAccessor="id"
              childrenAccessor="children"
              openByDefault={false}
              searchTerm={searchTerm}
              searchMatch={(node, term) => fileTreeSearchMatch(node.data as FileTreeNode, term, contentMatches)}
              selection={selectedPath || undefined}
              disableDrag
              disableDrop
              disableEdit
              onToggle={(id) => {
                setExpandedPaths((current) => {
                  const next = new Set(current);
                  if (next.has(id)) next.delete(id);
                  else next.add(id);
                  return next;
                });
              }}
            >
              {FileTreeRow}
            </Tree>
          ) : (
            <div className="tree-empty">没有搜索结果。</div>
          )}
        </div>
        <input
          ref={uploadInput}
          type="file"
          multiple
          hidden
          onChange={(event) => void uploadFiles(event.currentTarget.files)}
        />
        {menu && (
          <TreeActionMenu
            target={menu}
            transferBusy={transferState.kind === "progress"}
            onAction={(action) => void runAction(action, menu)}
          />
        )}
        <TransferStatus state={transferState} />
        <div className="tree-tip">提示：右键点击或轻触 ⋯ 进行文件操作。</div>
      </div>
    </TreeContext.Provider>
  );

  async function runAction(action: string, target: MenuTarget) {
    setMenu(null);
    if (transferState.kind === "progress" && (action === "upload" || action === "download")) return;
    const workspaceId = activeWorkspaceId();
    if (!workspaceId) return;
    try {
      if (action === "new-file" || action === "new-folder") {
        const parent = uploadFolder(target);
        const label = action === "new-file" ? "新建文件路径" : "新建文件夹路径";
        const name = window.prompt(label, joinPath(parent, action === "new-file" ? "untitled.txt" : "new-folder"));
        if (!name) return;
        await createWorkspaceFile(workspaceId, name, action === "new-file" ? "file" : "dir");
        refreshTree(action === "new-file" ? name : "");
      }
      if (action === "rename" && target.kind !== "root") {
        const nextPath = window.prompt("重命名路径", target.path);
        if (!nextPath || nextPath === target.path) return;
        await renameWorkspaceFile(workspaceId, target.path, nextPath);
        refreshTree(nextPath);
      }
      if (action === "delete" && target.kind !== "root") {
        if (!window.confirm(`要删除 ${target.path} 吗？`)) return;
        await deleteWorkspaceFile(workspaceId, target.path);
        refreshTree("");
      }
      if (action === "upload") {
        uploadTarget.current = target;
        uploadInput.current?.click();
      }
      if (action === "download" && target.kind === "file") await downloadFile(workspaceId, target.path);
    } catch (error) {
      showTemporaryError(error);
    }
  }

  async function downloadFile(workspaceId: string, path: string) {
    const fallbackName = path.split("/").pop() || path;
    setTransferState({ kind: "progress", operation: "download", filename: fallbackName, loaded: 0 });
    let objectUrl = "";
    try {
      const result = await downloadWorkspaceFile(workspaceId, path, (loaded, total) => {
        setTransferState({ kind: "progress", operation: "download", filename: fallbackName, loaded, total });
      });
      objectUrl = URL.createObjectURL(result.blob);
      const anchor = document.createElement("a");
      anchor.href = objectUrl;
      anchor.download = result.filename;
      anchor.hidden = true;
      document.body.append(anchor);
      anchor.click();
      anchor.remove();
      setTransferState({ kind: "success", operation: "download", filename: result.filename });
    } catch (error) {
      setTransferError("download", fallbackName, error);
    } finally {
      if (objectUrl) URL.revokeObjectURL(objectUrl);
    }
  }

  async function uploadFiles(fileList: FileList | null) {
    const workspaceId = activeWorkspaceId();
    const target = uploadTarget.current;
    const selectedFiles = fileList ? Array.from(fileList) : [];
    uploadTarget.current = null;
    if (uploadInput.current) uploadInput.current.value = "";
    if (!workspaceId || !target || !selectedFiles.length) return;
    const folder = uploadFolder(target);
    let firstPath = "";
    let currentFilename = selectedFiles[0]?.name || "";
    try {
      for (const file of selectedFiles) {
        currentFilename = file.name;
        const path = joinPath(folder, file.name);
        const onProgress = (loaded: number, total?: number) => {
          setTransferState({ kind: "progress", operation: "upload", filename: file.name, loaded, total });
        };
        setTransferState({ kind: "progress", operation: "upload", filename: file.name, loaded: 0 });
        try {
          await uploadWorkspaceFile(workspaceId, path, file, false, onProgress);
        } catch (error) {
          if (!isAlreadyExistsError(error) || !window.confirm(`${path} 已存在。要覆盖吗？`)) throw error;
          await uploadWorkspaceFile(workspaceId, path, file, true, onProgress);
        }
        if (!firstPath) firstPath = path;
      }
      setTransferState({ kind: "success", operation: "upload", filename: currentFilename });
      refreshTree(firstPath);
    } catch (error) {
      setTransferError("upload", currentFilename, error);
      showTemporaryError(error);
      refreshTree(firstPath);
    }
  }

  function setTransferError(operation: TransferOperation, filename: string, error: unknown) {
    setTransferState({ kind: "error", operation, filename, message: transferErrorMessage(error) });
  }
}

const TreeContext = React.createContext<{ setMenu: (target: MenuTarget) => void }>({ setMenu: () => {} });

function FileTreeRow({ node, style, dragHandle }: any) {
  const { setMenu } = React.useContext(TreeContext);
  const item = node.data as FileTreeNode;
  const isDir = item.kind === "dir";
  const status = item.gitStatus !== "clean" ? item.gitStatus : item.dirtyDescendants ? "dirty" : "clean";
  const classes = ["tree-node", item.kind, status, node.isSelected && "selected"].filter(Boolean).join(" ");
  const glyph = isDir ? (node.isOpen ? "▾" : "▸") : "";
  const icon = resolveMaterialFileIcon({ name: item.name, path: item.path, kind: item.kind, open: node.isOpen });

  const onClick = (event: React.MouseEvent<HTMLDivElement>) => {
    if ((event.target as Element).closest(".tree-row-menu")) return;
    if (isDir) {
      event.preventDefault();
      node.toggle();
      return;
    }
    node.select();
  };
  const onKeyDown = (event: React.KeyboardEvent<HTMLDivElement>) => {
    if ((event.target as Element).closest(".tree-row-menu")) return;
    if (event.key !== "Enter" && event.key !== " ") return;
    event.preventDefault();
    event.currentTarget.click();
  };
  const onContextMenu = (event: React.MouseEvent<HTMLDivElement>) => {
    event.preventDefault();
    setMenu({ x: event.clientX, y: event.clientY, path: item.path, kind: isDir ? "dir" : "file" });
  };
  const onMenuClick = (event: React.MouseEvent) => {
    event.preventDefault();
    event.stopPropagation();
    const rect = (event.currentTarget as HTMLElement).getBoundingClientRect();
    setMenu({ x: rect.left, y: rect.bottom, path: item.path, kind: isDir ? "dir" : "file" });
  };

  return (
    <div
      ref={dragHandle}
      style={style}
      role="treeitem"
      tabIndex={0}
      className={classes}
      data-action={isDir ? undefined : "open-file"}
      data-file-path={item.path}
      aria-expanded={isDir ? node.isOpen : undefined}
      aria-current={node.isSelected ? "true" : undefined}
      title={item.path}
      onClick={onClick}
      onKeyDown={onKeyDown}
      onContextMenu={onContextMenu}
    >
      <span className="glyph">{glyph}</span>
      <span className="file-icon" data-icon-name={icon.name}>
        <img src={icon.url} alt="" aria-hidden="true" draggable={false} />
      </span>
      <span className="name">{item.name}</span>
      <StatusBadge status={status} />
      <button className="tree-row-menu" type="button" aria-label={`操作：${item.name}`} onClick={onMenuClick}>⋯</button>
    </div>
  );
}

function TreeActionMenu({
  target,
  transferBusy,
  onAction,
}: {
  target: MenuTarget;
  transferBusy: boolean;
  onAction: (action: string) => void;
}) {
  const canTarget = target.kind !== "root";
  const style = menuPosition(target);
  return (
    <div className="tree-action-menu" role="menu" style={style} onClick={(event) => event.stopPropagation()}>
      <button type="button" role="menuitem" onClick={() => onAction("new-file")}>新建文件</button>
      <button type="button" role="menuitem" onClick={() => onAction("new-folder")}>新建文件夹</button>
      <button type="button" role="menuitem" disabled={transferBusy} onClick={() => onAction("upload")}>上传到此处</button>
      {target.kind === "file" && <button type="button" role="menuitem" disabled={transferBusy} onClick={() => onAction("download")}>下载</button>}
      <button type="button" role="menuitem" disabled={!canTarget} onClick={() => onAction("rename")}>重命名</button>
      <button type="button" role="menuitem" disabled={!canTarget} className="danger" onClick={() => onAction("delete")}>删除</button>
    </div>
  );
}

function TransferStatus({ state }: { state: TransferState }) {
  if (state.kind === "idle") return null;
  const operationLabel = state.operation === "upload" ? "上传" : "下载";
  let text = "";
  if (state.kind === "progress") {
    const percent = transferPercent(state.loaded, state.total);
    const bytes = state.total
      ? `${formatTransferBytes(state.loaded)} / ${formatTransferBytes(state.total)}`
      : formatTransferBytes(state.loaded);
    text = `${operationLabel}中：${state.filename} · ${percent === undefined ? bytes : `${percent}% · ${bytes}`}`;
  } else if (state.kind === "success") {
    text = `${operationLabel}完成：${state.filename}`;
  } else {
    text = `${operationLabel}失败：${state.filename} · ${state.message}`;
  }
  return <div className={`tree-transfer-status ${state.kind}`} data-status={state.kind} role="status" aria-live="polite" aria-atomic="true">{text}</div>;
}

function menuPosition(target: MenuTarget) {
  const width = 168;
  const height = 180;
  const margin = 8;
  const maxX = Math.max(margin, window.innerWidth - width - margin);
  const maxY = Math.max(margin, window.innerHeight - height - margin);
  return {
    left: Math.min(Math.max(margin, target.x), maxX),
    top: Math.min(Math.max(margin, target.y), maxY),
  };
}

function StatusBadge({ status }: { status: string }) {
  if (status === "clean") return null;
  const label = statusLabels[status] || "•";
  return <span className="tree-status-badge" aria-label={status}>{label}</span>;
}

function fileTreeSearchMatch(node: FileTreeNode, term: string, contentMatches: ReadonlySet<string>) {
  const query = term.trim().toLowerCase();
  return Boolean(
    query && (node.path.toLowerCase().includes(query) || node.name.toLowerCase().includes(query) || contentMatches.has(node.path)),
  );
}

function showTemporaryError(error: unknown) {
  window.alert(transferErrorMessage(error));
}

function transferErrorMessage(error: unknown) {
  return error instanceof Error ? error.message : String(error);
}

function activeWorkspaceId() {
  return document.querySelector<HTMLElement>("pi-app")?.dataset.activeWorkspaceId || "";
}

function refreshTree(selectedPath: string) {
  window.dispatchEvent(new CustomEvent("pi-workspace-tree:refresh", { detail: { selectedPath } }));
  if (selectedPath) window.dispatchEvent(new CustomEvent("pi-workspace-file:open", { detail: { path: selectedPath } }));
}

function uploadFolder(target: MenuTarget) {
  if (target.kind === "root") return "";
  if (target.kind === "dir") return target.path;
  return parentPath(target.path);
}

function parentPath(path: string) {
  const index = path.lastIndexOf("/");
  return index > 0 ? path.slice(0, index) : "";
}

function joinPath(parent: string, name: string) {
  return [parent, name].filter(Boolean).join("/").replace(/\/+/g, "/");
}

function isAlreadyExistsError(error: unknown) {
  return String(error instanceof Error ? error.message : error).toLowerCase().includes("already exists");
}

const statusLabels: Record<string, string> = {
  modified: "M",
  added: "A",
  untracked: "U",
  deleted: "D",
  renamed: "R",
  dirty: "•",
};
