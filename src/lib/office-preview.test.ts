import { afterEach, describe, expect, it, vi } from "vitest";
import { isOfficePreviewPath, mountOfficePreview } from "./office-preview";

const mountViewer = vi.fn();
const officePreset = { id: "office" };

vi.mock("@file-viewer/web", () => ({ mountViewer }));
vi.mock("@file-viewer/preset-office", () => ({ default: officePreset }));

afterEach(() => {
  mountViewer.mockReset();
  vi.restoreAllMocks();
});

describe("office preview adapter", () => {
  it("recognizes Office document paths without claiming ordinary previews", () => {
    expect(isOfficePreviewPath("docs/report.DOCX")).toBe(true);
    expect(isOfficePreviewPath("sheets/data.XLSX")).toBe(true);
    expect(isOfficePreviewPath("slides/deck.PPTX")).toBe(true);
    expect(isOfficePreviewPath("README.md")).toBe(false);
    expect(isOfficePreviewPath("manual.pdf")).toBe(false);
    expect(isOfficePreviewPath("photo.png")).toBe(false);
  });

  it("mounts the office preset with the workspace raw source", async () => {
    const destroy = vi.fn();
    mountViewer.mockReturnValue({ destroy, getState: () => ({ ready: true }) });
    const container = document.createElement("div");

    const handle = await mountOfficePreview(container, { workspaceId: "w/1", path: "docs/report.docx" });

    expect(mountViewer).toHaveBeenCalledWith(
      container,
      expect.objectContaining({
        url: "/api/workspaces/w%2F1/files/raw?path=docs%2Freport.docx",
        filename: "report.docx",
        options: expect.objectContaining({
          preset: officePreset,
          rendererMode: "replace",
          builtinRenderers: "none",
          autoRenderers: false,
        }),
      }),
      expect.objectContaining({ fetchFile: expect.any(Function) }),
    );
    handle.destroy();
    expect(destroy).toHaveBeenCalledOnce();
  });

  it("fetches Office bytes with credentials and preserves the response type", async () => {
    const controller = { destroy: vi.fn(), getState: () => ({ ready: true }) };
    mountViewer.mockReturnValue(controller);
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response("docx", {
      status: 200,
      headers: { "Content-Type": "application/vnd.openxmlformats-officedocument.wordprocessingml.document" },
    }));
    const container = document.createElement("div");
    await mountOfficePreview(container, { workspaceId: "w1", path: "report.docx" });
    const fetchFile = mountViewer.mock.calls[0][2].fetchFile;

    const file = await fetchFile({ url: "/raw", signal: undefined, source: { filename: "report.docx" } });

    expect(fetchMock).toHaveBeenCalledWith("/raw", { credentials: "include", signal: undefined });
    expect(file).toBeInstanceOf(File);
    expect(file.name).toBe("report.docx");
    expect(file.type).toBe("application/vnd.openxmlformats-officedocument.wordprocessingml.document");
  });

  it("rejects a cancelled mount before loading the viewer", async () => {
    const controller = new AbortController();
    controller.abort();

    await expect(mountOfficePreview(document.createElement("div"), {
      workspaceId: "w1",
      path: "report.docx",
    }, { signal: controller.signal })).rejects.toMatchObject({ name: "AbortError" });
    expect(mountViewer).not.toHaveBeenCalled();
  });
});
