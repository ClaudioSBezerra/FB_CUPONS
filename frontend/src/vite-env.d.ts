/// <reference types="vite/client" />

interface ImportMetaEnv {
  /**
   * URL base da API. Vazio significa "mesmo domínio, em /api" — é o que a
   * produção usa, para o cookie de sessão ser first-party.
   */
  readonly VITE_API_URL?: string;
}

interface ImportMeta {
  readonly env: ImportMetaEnv;
}
