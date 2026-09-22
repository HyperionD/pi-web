import { getWorkspaceFileAssetURL } from "./api";

const OFFICE_EXTENSIONS = /\.(?:doc|docx|docm|dot|dotx|dotm|xls|xlsx|xlsm|xlsb|xlt|xltx|xltm|ppt|pptx|pptm|pot|potx|potm|pps|ppsx|ppsm)$/i;

export type OfficePreviewIdentity = {
  workspaceId: string;
  path: string;
  version?: string;
};

export type OfficePreviewHandle = {
  ready: Promise<void>;
  destroy: () => void;
};

type OfficeMountOptions = {
  onDownload?: () => void;
  signal?: AbortSignal;
};

export function isOfficePreviewPath(path: string): boolean {
  return OFFICE_EXTENSIONS.test(String(path || "").replaceAll("\\", "/"));
}

export async function mountOfficePreview(
  container: HTMLElement,
  identity: OfficePreviewIdentity,
  options: OfficeMountOptions = {},
): Promise<OfficePreviewHandle> {
  throwIfAborted(options.signal);
  const [{ mountViewer }, { default: officePreset }] = await Promise.all([
    import("@file-viewer/web"),
    import("@file-viewer/preset-office"),
  ]);
  throwIfAborted(options.signal);

  const filename = basename(identity.path);
  const controller = mountViewer(
    container,
    {
      url: getWorkspaceFileAssetURL(identity.workspaceId, identity.path),
      filename,
      options: {
        preset: officePreset,
        rendererMode: "replace",
        builtinRenderers: "none",
        autoRenderers: false,
        toolbar: true,
      },
    },
    {
      fetchFile: async ({ url, signal, source }) => {
        const response = await fetch(url, { credentials: "include", signal });
        if (!response.ok) throw new Error(`Office file request failed (${response.status})`);
        const blob = await response.blob();
        if (signal?.aborted) throw abortError();
        return new File([blob], source.filename || filename, {
          type: response.headers.get("content-type") || blob.type || "application/octet-stream",
        });
      },
    },
  );

  return {
    ready: viewerReady(controller),
    destroy: () => controller.destroy(),
  };
}

function viewerReady(controller: {
  getState?: () => { ready?: boolean; error?: unknown };
  subscribe?: (listener: (state: { ready?: boolean; error?: unknown }) => void) => () => void;
}): Promise<void> {
  const state = controller.getState?.();
  if (state?.ready) return Promise.resolve();
  if (!controller.subscribe) return Promise.resolve();
  return new Promise((resolve, reject) => {
    let settled = false;
    let unsubscribe = () => undefined;
    const listener = (next: { ready?: boolean; error?: unknown }) => {
      if (settled) return;
      if (next.error) {
        settled = true;
        unsubscribe();
        reject(next.error);
      } else if (next.ready) {
        settled = true;
        unsubscribe();
        resolve();
      }
    };
    unsubscribe = controller.subscribe(listener);
    if (settled) unsubscribe();
  });
}

function basename(path: string): string {
  return String(path || "file").replaceAll("\\", "/").split("/").pop() || "file";
}

function throwIfAborted(signal?: AbortSignal): void {
  if (signal?.aborted) throw abortError();
}

function abortError(): DOMException {
  return new DOMException("The operation was aborted.", "AbortError");
}
