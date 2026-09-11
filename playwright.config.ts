import { defineConfig } from "@playwright/test";

const baseURL = process.env.PI_WEB_E2E_BASE_URL;
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
      use: { browserName: "chromium", channel: "chrome" },
    },
  ],
  webServer: {
    command: "bun run dev -- --port 4321",
    url: baseURL,
    reuseExistingServer: true,
    timeout: 120_000,
  },
});
