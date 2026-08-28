import react from "@astrojs/react";
import { defineConfig } from "astro/config";

export default defineConfig({
  integrations: [react()],
  vite: {
    build: {
      target: "chrome107",
    },
  },
  build: {
    assets: "assets",
  },
});
