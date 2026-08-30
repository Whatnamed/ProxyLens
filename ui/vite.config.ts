import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { defineConfig } from 'vite';
import type { Plugin } from 'vite';
import react from '@vitejs/plugin-react';

const uiRoot = path.dirname(fileURLToPath(import.meta.url));
const fontLabDir = path.join(uiRoot, '.font-lab');
const fontLabManifest = path.join(fontLabDir, 'font-lab.local.json');

function fontLabDevPlugin(): Plugin {
  return {
    name: 'proxylens-font-lab-dev-server',
    apply: 'serve',
    configureServer(server) {
      server.middlewares.use('/__proxylens_font_lab__', (request, response, next) => {
        const requestUrl = new URL(request.url ?? '/', 'http://font-lab.local');
        if (requestUrl.pathname === '/manifest') {
          fs.readFile(fontLabManifest, 'utf8', (error, content) => {
            const body = error ? JSON.stringify({ version: 1, candidates: {} }) : content;
            response.statusCode = 200;
            response.setHeader('Cache-Control', 'no-store');
            response.setHeader('Content-Type', 'application/json; charset=utf-8');
            response.end(body);
          });
          return;
        }

        if (requestUrl.pathname !== '/file') {
          next();
          return;
        }

        const fileName = requestUrl.searchParams.get('name') ?? '';
        if (!fileName || fileName !== path.basename(fileName) || !/^[\w.-]+$/.test(fileName)) {
          response.statusCode = 400;
          response.end('Invalid Font Lab file name');
          return;
        }

        const filePath = path.join(fontLabDir, fileName);
        const contentType = fileName.endsWith('.woff2')
          ? 'font/woff2'
          : fileName.endsWith('.woff')
            ? 'font/woff'
            : fileName.endsWith('.otf')
              ? 'font/otf'
              : 'font/ttf';
        fs.readFile(filePath, (error, content) => {
          if (error) {
            response.statusCode = error.code === 'ENOENT' ? 404 : 500;
            response.end(error.code === 'ENOENT' ? 'Font Lab file is unavailable' : 'Unable to read Font Lab file');
            return;
          }
          response.statusCode = 200;
          response.setHeader('Cache-Control', 'no-store');
          response.setHeader('Content-Type', contentType);
          response.end(content);
        });
      });
    },
  };
}

// https://vitejs.dev/config/
export default defineConfig(async () => ({
  plugins: [react(), fontLabDevPlugin()],
  clearScreen: false,
  server: {
    port: 1420,
    strictPort: true,
    host: '127.0.0.1',
    proxy: {
      // Browser dev mode talks to the Query API same-origin through this
      // proxy; the API deliberately does not serve CORS preflights.
      '/api': {
        target: `http://127.0.0.1:${process.env.VITE_PROXYLENS_API_PORT || '49152'}`,
        changeOrigin: false,
      },
    },
  },
  envPrefix: ['VITE_', 'TAURI_ENV_*'],
  build: {
    target: process.env.TAURI_ENV_PLATFORM == 'windows' ? 'chrome105' : 'safari13',
    minify: !process.env.TAURI_ENV_DEBUG ? 'esbuild' : false,
    sourcemap: !!process.env.TAURI_ENV_DEBUG,
  },
}));
