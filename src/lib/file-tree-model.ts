export type RawFileNode = {
  type?: "file" | "dir" | string;
  name: string;
  path?: string;
  depth?: number;
  open?: boolean;
  status?: GitFileStatus | string;
  children?: RawFileNode[];
};

export type GitFileStatus = "modified" | "added" | "untracked" | "deleted" | "renamed" | "clean";

export type FileTreeNode = {
  id: string;
  name: string;
  path: string;
  kind: "file" | "dir";
  children?: FileTreeNode[];
  childrenLoaded: boolean;
  gitStatus: GitFileStatus;
  dirtyDescendants: boolean;
  selected: boolean;
  expanded: boolean;
};

export type GitStatusMap = Record<string, GitFileStatus | string | undefined>;

const DIRTY_STATUSES = new Set<GitFileStatus>(["modified", "added", "untracked", "deleted", "renamed"]);

export function decorateFileTree(
  nodes: RawFileNode[] = [],
  statusMap: GitStatusMap = {},
  selectedPath = "",
  expandedPaths: ReadonlySet<string> = new Set(),
  searchMatches: ReadonlySet<string> = new Set(),
): FileTreeNode[] {
  const visibleNodes = searchMatches.size ? addSearchMatches(nodes, searchMatches) : nodes;
  return sortFileTreeNodes(visibleNodes).map((node) => decorateNode(node, statusMap, selectedPath, expandedPaths));
}

export function isDirtyStatus(status: GitFileStatus | string | undefined): boolean {
  return DIRTY_STATUSES.has(normalizeGitStatus(status));
}

function decorateNode(
  node: RawFileNode,
  statusMap: GitStatusMap,
  selectedPath: string,
  expandedPaths: ReadonlySet<string>,
): FileTreeNode {
  const path = normalizeFileTreePath(node.path || node.name);
  const children = sortFileTreeNodes(node.children || []).map((child) => decorateNode(child, statusMap, selectedPath, expandedPaths));
  const gitStatus = normalizeGitStatus(statusMap[path] || node.status);
  const dirtyDescendants = children.some((child) => isDirtyStatus(child.gitStatus) || child.dirtyDescendants);
  const kind = node.type === "dir" ? "dir" : "file";
  return {
    id: path || node.name,
    name: node.name,
    path,
    kind,
    children: kind === "dir" ? children : undefined,
    childrenLoaded: Array.isArray(node.children),
    gitStatus,
    dirtyDescendants,
    selected: path === selectedPath,
    expanded: Boolean(node.open || expandedPaths.has(path)),
  };
}

function addSearchMatches(nodes: RawFileNode[], matches: ReadonlySet<string>): RawFileNode[] {
  const index = new Map<string, RawFileNode>();
  const result = copySearchTree(nodes, index);
  for (const match of matches) {
    const path = normalizeFileTreePath(match);
    const parts = path.split("/");
    if (/^[a-z]:/i.test(path) || parts.some((part) => !part || part === "." || part === "..")) continue;
    let siblings = result;
    let parentPath = "";
    for (let depth = 0; depth < parts.length; depth += 1) {
      const name = parts[depth];
      const nodePath = parentPath ? `${parentPath}/${name}` : name;
      const isFile = depth === parts.length - 1;
      let node = index.get(nodePath);
      if (!node) {
        node = { name, path: nodePath, type: isFile ? "file" : "dir" };
        siblings.push(node);
        index.set(nodePath, node);
      }
      if (!isFile) {
        if (node.type !== "dir") break;
        node.children ??= [];
        siblings = node.children;
      }
      parentPath = nodePath;
    }
  }
  return result;
}

function copySearchTree(nodes: RawFileNode[], index: Map<string, RawFileNode>): RawFileNode[] {
  return nodes.map((node) => {
    const copy = { ...node };
    if (node.children) copy.children = copySearchTree(node.children, index);
    index.set(normalizeFileTreePath(node.path || node.name), copy);
    return copy;
  });
}

function sortFileTreeNodes(nodes: RawFileNode[]): RawFileNode[] {
  return nodes
    .map((node, index) => ({ node, index }))
    .sort((left, right) => {
      const leftKind = left.node.type === "dir" ? 0 : 1;
      const rightKind = right.node.type === "dir" ? 0 : 1;
      if (leftKind !== rightKind) return leftKind - rightKind;

      const nameOrder = left.node.name.toLowerCase().localeCompare(right.node.name.toLowerCase());
      return nameOrder || left.index - right.index;
    })
    .map(({ node }) => node);
}

function normalizeGitStatus(status: GitFileStatus | string | undefined): GitFileStatus {
  if (status === "modified" || status === "added" || status === "untracked" || status === "deleted" || status === "renamed") {
    return status;
  }
  return "clean";
}

export function normalizeFileTreePath(path: string): string {
  return String(path || "").replace(/^\.\//, "").replace(/\\/g, "/");
}
