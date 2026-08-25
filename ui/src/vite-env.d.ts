/// <reference types="vite/client" />

interface ImportMetaEnv {
  readonly VITE_PROXYLENS_API_PORT?: string;
  readonly VITE_PROXYLENS_API_TOKEN?: string;
}

interface ImportMeta {
  readonly env: ImportMetaEnv;
}
