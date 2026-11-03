import React from "react";
import { createRoot } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { App } from "./App";
import "./estilos.css";

const clienteQuery = new QueryClient({
  defaultOptions: {
    queries: {
      // Importação e revisão são operações caras: não revalidar a cada foco.
      refetchOnWindowFocus: false,
      retry: 1,
      staleTime: 30_000,
    },
  },
});

const raiz = document.getElementById("raiz");
if (!raiz) throw new Error("elemento #raiz não encontrado no index.html");

createRoot(raiz).render(
  <React.StrictMode>
    <QueryClientProvider client={clienteQuery}>
      <App />
    </QueryClientProvider>
  </React.StrictMode>,
);
