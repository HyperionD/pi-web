import react from "@astrojs/react";
import { defineConfig } from "astro/config";

export default defineConfig({
  integrations: [react()],
  vite: {
    server: { proxy: { "/api": process.env.PI_WEB_BACKEND_URL || "http://127.0.0.1:8732" } },
    build: {
      target: "chrome107",
    },
  },
  build: {
    assets: "assets",
  },
});
