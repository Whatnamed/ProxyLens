import { QueryApiSession } from '../api/types';

export function isTauriEnvironment(): boolean {
  return typeof window !== 'undefined' && '__TAURI_INTERNALS__' in window;
}

export async function getQueryApiSession(): Promise<QueryApiSession> {
  if (!isTauriEnvironment()) {
    // 浏览器独立运行模式（用于 Vite 开发或脱机诊断调试）
    const devPort = import.meta.env.VITE_PROXYLENS_API_PORT || '49152';
    const devToken = import.meta.env.VITE_PROXYLENS_API_TOKEN || 'dev-local-session-token';
    return {
      baseUrl: `http://127.0.0.1:${devPort}`,
      token: devToken,
      apiVersion: 'v1'
    };
  }

  try {
    const { invoke } = await import('@tauri-apps/api/core');
    const session = await invoke<QueryApiSession>('get_query_api_session');
    return session;
  } catch (err: unknown) {
    const message = err instanceof Error ? err.message : String(err);
    throw new Error(`Failed to initialize query API session from Tauri: ${message}`);
  }
}
