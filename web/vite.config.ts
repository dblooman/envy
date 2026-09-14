import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import path from "path";

const apiPort = process.env.ENVY_API_PORT || "8081";
const apiTarget = `http://127.0.0.1:${apiPort}`;

const apiToken = process.env.ENVY_API_TOKEN?.trim();

// https://vite.dev/config/
export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "./src"),
    },
  },
  server: {
    port: 5173,
    proxy: {
      "/v1": {
        target: apiTarget,
        changeOrigin: true,
        headers: apiToken ? { Authorization: `Bearer ${apiToken}` } : undefined,
      },
      "/healthz": {
        target: apiTarget,
        changeOrigin: true,
      },
      "/readyz": {
        target: apiTarget,
        changeOrigin: true,
      },
    },
  },
});
