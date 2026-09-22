import { expect, test } from "@playwright/test";

test("serves Office viewer workers and fonts from the local deployment", async ({ request }) => {
  const assets = [
    ["/file-viewer/vendor/docx/docx.worker.js", /javascript/],
    ["/file-viewer/vendor/xlsx/sheet.worker.js", /javascript/],
    ["/file-viewer/vendor/pptx/pptx.worker.js", /javascript/],
    ["/file-viewer/vendor/pdf/pdf.worker.mjs", /javascript/],
    ["/file-viewer/vendor/pdf/fonts/files/noto-sans-sc-119-wght-normal.woff2", /font|octet-stream/],
  ] as const;

  for (const [path, contentType] of assets) {
    const response = await request.get(path);
    expect(response.status(), path).toBe(200);
    expect(response.headers()["content-type"], path).toMatch(contentType);
  }

  const manifest = await request.get("/file-viewer/manifest.json");
  expect(manifest.status()).toBe(200);
  const body = await manifest.json();
  expect(body.packages["@file-viewer/preset-office"]).toBe("3.1.2");
});
