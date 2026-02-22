import { defineConfig } from "vite";

export default defineConfig({
	server: {
		port: 5173,
		headers: {
			"Cross-Origin-Opener-Policy": "same-origin",
			"Cross-Origin-Embedder-Policy": "require-corp",
		},
		proxy: {
			"/api": {
				target: "https://localhost:4444",
				secure: false,
			},
		},
	},
	preview: {
		headers: {
			"Cross-Origin-Opener-Policy": "same-origin",
			"Cross-Origin-Embedder-Policy": "require-corp",
		},
	},
	build: {
		target: "ES2022",
		outDir: "dist",
	},
});
