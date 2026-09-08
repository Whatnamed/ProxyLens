#!/usr/bin/env node

import { execFileSync, execSync } from 'node:child_process';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const rootDir = path.resolve(__dirname, '..', '..');
const uiDir = path.resolve(__dirname, '..');
const binariesDir = path.resolve(uiDir, 'src-tauri', 'binaries');

console.log('[Binary Build] Detecting Rust target triple...');
let targetTriple = '';
try {
  const rustcVv = execSync('rustc -Vv', { encoding: 'utf8' });
  const hostMatch = rustcVv.match(/host:\s*([^\r\n]+)/);
  if (hostMatch && hostMatch[1]) {
    targetTriple = hostMatch[1].trim();
  }
} catch (e) {
  console.error('[Binary Build] Failed to detect rustc host triple:', e.message);
  process.exit(1);
}

if (!targetTriple) {
  console.error('[Binary Build] Could not determine target triple from rustc -Vv');
  process.exit(1);
}

console.log(`[Binary Build] Target triple: ${targetTriple}`);

if (!fs.existsSync(binariesDir)) {
  fs.mkdirSync(binariesDir, { recursive: true });
}

const ext = process.platform === 'win32' ? '.exe' : '';
const binaries = [
  { name: 'proxylens-query-api', packagePath: './cmd/proxylens-query-api' },
  { name: 'proxylens-runtime', packagePath: './cmd/proxylens-runtime' },
  { name: 'proxylens-supervisor', packagePath: './cmd/proxylens-supervisor' },
  {
    name: 'proxylens-supervisor-host',
    packagePath: './cmd/proxylens-supervisor-host',
    windowsGui: true,
  },
];

for (const binary of binaries) {
  const binaryName = `${binary.name}-${targetTriple}${ext}`;
  const targetPath = path.join(binariesDir, binaryName);
  console.log(`[Binary Build] Building ${binary.name} into ${targetPath}...`);
  try {
    const buildArgs = ['build'];
    if (binary.windowsGui && process.platform === 'win32') {
      buildArgs.push('-ldflags=-H=windowsgui');
    }
    buildArgs.push('-o', targetPath, binary.packagePath);
    execFileSync('go', buildArgs, {
      cwd: path.join(rootDir, 'collector'),
      stdio: 'inherit',
    });
  } catch (e) {
    console.error(`[Binary Build] Go build failed for ${binary.name}:`, e.message);
    process.exit(1);
  }
  console.log(`[Binary Build] Successfully built ${binaryName}`);
}
