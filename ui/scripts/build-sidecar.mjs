#!/usr/bin/env node

import { execSync } from 'node:child_process';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const rootDir = path.resolve(__dirname, '..', '..');
const uiDir = path.resolve(__dirname, '..');
const binariesDir = path.resolve(uiDir, 'src-tauri', 'binaries');

console.log('[Sidecar Build] Detecting Rust target triple...');
let targetTriple = '';
try {
  const rustcVv = execSync('rustc -Vv', { encoding: 'utf8' });
  const hostMatch = rustcVv.match(/host:\s*([^\r\n]+)/);
  if (hostMatch && hostMatch[1]) {
    targetTriple = hostMatch[1].trim();
  }
} catch (e) {
  console.error('[Sidecar Build] Failed to detect rustc host triple:', e.message);
  process.exit(1);
}

if (!targetTriple) {
  console.error('[Sidecar Build] Could not determine target triple from rustc -Vv');
  process.exit(1);
}

console.log(`[Sidecar Build] Target triple: ${targetTriple}`);

if (!fs.existsSync(binariesDir)) {
  fs.mkdirSync(binariesDir, { recursive: true });
}

const ext = process.platform === 'win32' ? '.exe' : '';
const sidecarBinaryName = `proxylens-query-api-${targetTriple}${ext}`;
const sidecarTargetPath = path.join(binariesDir, sidecarBinaryName);

console.log(`[Sidecar Build] Building Go Query API into ${sidecarTargetPath}...`);
const goBuildCmd = `go build -o "${sidecarTargetPath}" ./cmd/proxylens-query-api`;

try {
  execSync(goBuildCmd, {
    cwd: path.join(rootDir, 'collector'),
    stdio: 'inherit'
  });
  console.log(`[Sidecar Build] Successfully built ${sidecarBinaryName}`);
} catch (e) {
  console.error('[Sidecar Build] Go build failed:', e.message);
  process.exit(1);
}
