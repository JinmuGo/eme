import { defineConfig } from "vite";

// Served at the domain root (eme.jinmu.me), so the default base "/" is correct.
export default defineConfig({
  base: "/",
  build: { outDir: "dist", emptyOutDir: true },
});
