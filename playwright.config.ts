import { defineConfig } from "@playwright/test";

const baseURL = process.env.PI_WEB_E2E_BASE_URL;
const chrome107Path = process.env.PI_WEB_CHROME107_PATH;
if (!baseURL) {
  throw new Error("PI_WEB_E2E_BASE_URL is required; source .env.test before running e2e tests");
}

export default defineConfig({
  testDir: "./e2e",
  fullyParallel: false,
  workers: 1,
  retries: 0,
  reporter: [["list"]],
  use: {
    baseURL,
    trace: "on-first-retry",
  },
  projects: [
    {
      name: "chromium",
      use: { browserName: "chromium" },
    },
    ...(chrome107Path ? [{
      name: "chromium-107",
      use: { browserName: "chromium" as const, executablePath: chrome107Path },
    }] : []),
  ],
  webServer: {
    command: "rm -rf /tmp/pi-web-session-e2e && mkdir -p /tmp/pi-web-session-e2e/sessions /tmp/pi-web-session-e2e/workspace && rm -rf public/file-viewer && mkdir -p public/file-viewer && cp -R static/file-viewer/. public/file-viewer/ && (PI_CODING_AGENT_SESSION_DIR=/tmp/pi-web-session-e2e/sessions go run . --host 127.0.0.1 --port 18732 & backend=$!; trap 'kill $backend 2>/dev/null || true; rm -rf public/file-viewer' EXIT; bun run dev -- --port 4321)",
    env: { PI_WEB_BACKEND_URL: "http://127.0.0.1:18732" },
    url: baseURL,
    reuseExistingServer: true,
    timeout: 120_000,
  },
});
