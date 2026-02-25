import { defineConfig } from "vite";
import { resolve } from "path";

export default defineConfig({
  build: {
    target: "ES2022",
    outDir: "dist-lib",
    lib: {
      entry: resolve(__dirname, "src/lib.ts"),
      formats: ["es"],
      fileName: "prism",
    },
    rollupOptions: {
      output: {
        entryFileNames: "prism.js",
      },
    },
  },
});
