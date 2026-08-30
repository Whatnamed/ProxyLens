import { copyFile, mkdir, readFile, rename, rm, stat, writeFile } from 'node:fs/promises';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { execFile as execFileCallback } from 'node:child_process';
import { promisify } from 'node:util';

const execFile = promisify(execFileCallback);
const uiRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const manifestPath = path.join(uiRoot, 'src', 'dev', 'fontLab', 'font-lab-manifest.json');
const fontLabDir = path.join(uiRoot, '.font-lab');
const localManifestPath = path.join(fontLabDir, 'font-lab.local.json');
const workDir = path.join(fontLabDir, '.work');
const maxDownloadBytes = 120 * 1024 * 1024;

const manifest = JSON.parse(await readFile(manifestPath, 'utf8'));
const localCandidates = {};
const archiveRoots = new Map();
const errors = [];

async function isFile(filePath) {
  try {
    return (await stat(filePath)).isFile();
  } catch {
    return false;
  }
}

async function fetchBytes(url) {
  const response = await fetch(url, {
    headers: { 'User-Agent': 'ProxyLens-FontLab-Setup' },
    redirect: 'follow',
  });
  if (!response.ok) throw new Error(`${response.status} ${response.statusText}`);

  const contentLength = Number(response.headers.get('content-length') ?? 0);
  if (contentLength > maxDownloadBytes) {
    throw new Error(`download is ${contentLength} bytes, above the ${maxDownloadBytes}-byte safety limit`);
  }

  const bytes = Buffer.from(await response.arrayBuffer());
  if (bytes.length === 0) throw new Error('download returned an empty file');
  if (bytes.length > maxDownloadBytes) {
    throw new Error(`download is ${bytes.length} bytes, above the ${maxDownloadBytes}-byte safety limit`);
  }
  return bytes;
}

async function ensureDownload(url, destination) {
  if (await isFile(destination)) return;
  const partial = `${destination}.part`;
  const bytes = await fetchBytes(url);
  await writeFile(partial, bytes);
  await rename(partial, destination);
}

async function archiveRoot(url) {
  const cached = archiveRoots.get(url);
  if (cached) return cached;

  const archiveFile = path.join(workDir, `${archiveRoots.size}.zip`);
  const extractRoot = path.join(workDir, `${archiveRoots.size}-extract`);
  await mkdir(extractRoot, { recursive: true });
  await ensureDownload(url, archiveFile);
  await execFile('tar', ['-xf', archiveFile, '-C', extractRoot], { windowsHide: true });
  archiveRoots.set(url, extractRoot);
  return extractRoot;
}

function extractedEntryPath(root, entry) {
  const resolvedRoot = path.resolve(root);
  const resolvedEntry = path.resolve(root, entry);
  const prefix = `${resolvedRoot}${path.sep}`;
  if (!resolvedEntry.startsWith(prefix)) throw new Error(`archive entry escapes extraction root: ${entry}`);
  return resolvedEntry;
}

async function prepareArchiveAsset(asset) {
  const root = await archiveRoot(asset.archiveUrl);
  const source = extractedEntryPath(root, asset.entry);
  if (!(await isFile(source))) throw new Error(`archive entry was not found: ${asset.entry}`);
  const destination = path.join(fontLabDir, asset.file);
  if (!(await isFile(destination))) await copyFile(source, destination);
  return assetFace(asset);
}

function assetFace(asset) {
  return {
    file: asset.file,
    format: asset.format,
    weight: asset.weight,
    ...(asset.locale ? { locale: asset.locale } : {}),
    ...(asset.unicodeRange ? { unicodeRange: asset.unicodeRange } : {}),
  };
}

async function prepareFileAsset(asset) {
  const destination = path.join(fontLabDir, asset.file);
  await ensureDownload(asset.url, destination);
  return assetFace(asset);
}

function cssFaceBlocks(css) {
  return [...css.matchAll(/@font-face\s*\{([\s\S]*?)\}/gi)].map((match) => match[1]);
}

function parseCssFaces(css, cssUrl, candidateId) {
  const faces = [];
  const seen = new Set();
  for (const block of cssFaceBlocks(css)) {
    const source = block.match(/src\s*:\s*url\(["']?([^\)"']+\.woff2)["']?\)/i)?.[1];
    if (!source) continue;
    const url = new URL(source, cssUrl).href;
    const weight = block.match(/font-weight\s*:\s*([^;]+)/i)?.[1]?.trim() ?? '400';
    const unicodeRange = block.match(/unicode-range\s*:\s*([^;]+)/i)?.[1]?.trim();
    const key = `${url}|${weight}|${unicodeRange ?? ''}`;
    if (seen.has(key)) continue;
    seen.add(key);
    const file = `${candidateId}-${String(faces.length).padStart(2, '0')}.woff2`;
    faces.push({ file, url, format: 'woff2', weight, ...(unicodeRange ? { unicodeRange } : {}) });
  }
  if (faces.length === 0) throw new Error('official CSS did not contain any WOFF2 faces');
  return faces;
}

async function prepareCssCandidate(candidate) {
  const cssBytes = await fetchBytes(candidate.setup.url);
  const faces = parseCssFaces(cssBytes.toString('utf8'), candidate.setup.url, candidate.id);
  for (const face of faces) await ensureDownload(face.url, path.join(fontLabDir, face.file));
  return faces.map(({ file, format, weight, unicodeRange }) => ({
    file,
    format,
    weight,
    ...(unicodeRange ? { unicodeRange } : {}),
  }));
}

async function prepareCandidate(candidate) {
  if (candidate.kind !== 'local') return;
  try {
    let faces;
    if (candidate.setup?.type === 'css') {
      faces = await prepareCssCandidate(candidate);
    } else {
      faces = [];
      for (const asset of candidate.assets ?? []) {
        faces.push(asset.type === 'archive' ? await prepareArchiveAsset(asset) : await prepareFileAsset(asset));
      }
    }
    localCandidates[candidate.id] = { status: 'loaded', faces };
    console.log(`[fontlab] Loaded ${candidate.label} (${faces.length} face${faces.length === 1 ? '' : 's'})`);
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error);
    localCandidates[candidate.id] = { status: 'unavailable', faces: [], error: message };
    errors.push(`${candidate.label}: ${message}`);
    console.error(`[fontlab] Unavailable ${candidate.label}: ${message}`);
  }
}

await mkdir(fontLabDir, { recursive: true });
await mkdir(workDir, { recursive: true });

try {
  for (const candidate of manifest.candidates) await prepareCandidate(candidate);
  await writeFile(localManifestPath, `${JSON.stringify({ version: manifest.version, candidates: localCandidates }, null, 2)}\n`, 'utf8');
} finally {
  await rm(workDir, { recursive: true, force: true });
}

if (errors.length > 0) {
  console.error(`[fontlab] Setup finished with ${errors.length} unavailable candidate${errors.length === 1 ? '' : 's'}.`);
  process.exitCode = 1;
} else {
  console.log(`[fontlab] Setup complete: ${Object.keys(localCandidates).length} local candidates ready.`);
}
