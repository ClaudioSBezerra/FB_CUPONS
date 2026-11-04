import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    // Em desenvolvimento o /api é encaminhado para a API, para o cookie de
    // sessão ser first-party também aqui — igual à produção.
    proxy: {
      "/api": {
        // Só no servidor de desenvolvimento do vite. O build nunca embute
        // endereço: em contêiner o nginx é que repassa /api.
        target: process.env.API_DEV_URL || "http://localhost:8080",
        changeOrigin: true,
      },
    },
  },
  build: { outDir: "dist", sourcemap: false },
});
