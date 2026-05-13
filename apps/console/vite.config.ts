import react from "@vitejs/plugin-react";
import { fileURLToPath } from "node:url";
import { defineConfig } from "vitest/config";

const consoleRoot = fileURLToPath(new URL(".", import.meta.url));

export default defineConfig({
  build: {
    outDir: "dist"
  },
  plugins: [react()],
  root: consoleRoot,
  server: {
    proxy: {
      "/eshu-api": {
        rewrite: (path) => path.replace(/^\/eshu-api/, ""),
        target: "http://127.0.0.1:8080"
      }
    }
  },
  test: {
    environment: "jsdom",
    environmentOptions: {
      jsdom: {
        url: "http://localhost:5174/"
      }
    },
    include: ["src/**/*.test.{ts,tsx}"],
    globals: true,
    setupFiles: "src/test/setup.ts"
  }
});
