import React, { useEffect, useMemo, useState } from 'react';
import manifest from './font-lab-manifest.json';
import './fontLab.css';

type FontLanguage = 'en' | 'zh-CN';
type FontAxis = 'latin' | 'cjk';
type FontKind = 'bundled' | 'system' | 'local';
type FontLabStatus = 'loading' | 'loaded' | 'system' | 'unavailable';

interface FontFaceAsset {
  file: string;
  format: string;
  weight: string;
  locale?: FontLanguage;
  unicodeRange?: string;
}

interface FontCandidate {
  id: string;
  label: string;
  kind: FontKind;
  family?: string;
  families?: Partial<Record<FontLanguage, string>>;
  systemFamily?: string;
  coverage: FontLanguage[];
  assets?: FontFaceAsset[];
}

interface LocalCandidate {
  status: 'loaded' | 'unavailable';
  faces: FontFaceAsset[];
  error?: string;
}

interface LocalManifest {
  version: number;
  candidates: Record<string, LocalCandidate>;
}

interface FontManifest {
  version: number;
  candidates: FontCandidate[];
}

const FONT_MANIFEST = manifest as FontManifest;
const BASE_CANDIDATES = FONT_MANIFEST.candidates;
const DEFAULT_LATIN_FONT_ID = 'manrope';
const DEFAULT_CJK_FONT_ID = 'oppo-sans-4';
const FONT_LAB_MANIFEST_URL = '/__proxylens_font_lab__/manifest';
const MISSING_FONT_FAMILY = '__ProxyLens_Font_Lab_Missing__';

const STATUS_LABELS: Record<FontLabStatus, string> = {
  loading: 'Loading',
  loaded: 'Loaded',
  system: 'System available',
  unavailable: 'Unavailable',
};

const COVERAGE_SAMPLES: Record<FontLanguage, string> = {
  en: 'AaMmWw0123',
  'zh-CN': '中文测试',
};

function initialStatuses(): Record<string, FontLabStatus> {
  return Object.fromEntries(
    BASE_CANDIDATES.map((candidate) => [candidate.id, candidate.kind === 'bundled' ? 'loaded' : 'loading']),
  );
}

function axisLanguage(axis: FontAxis): FontLanguage {
  return axis === 'latin' ? 'en' : 'zh-CN';
}

function candidateFamily(candidate: FontCandidate, language: FontLanguage): string {
  return candidate.families?.[language] ?? candidate.family ?? candidate.systemFamily ?? '';
}

function cssFamily(family: string): string {
  const safeFamily = family.replace(/["\\]/g, '');
  return `"${safeFamily}"`;
}

function supportsLanguage(candidate: FontCandidate, language: FontLanguage): boolean {
  return candidate.coverage.includes(language);
}

function isSelectable(candidate: FontCandidate, language: FontLanguage, statuses: Record<string, FontLabStatus>): boolean {
  const status = statuses[candidate.id];
  return supportsLanguage(candidate, language) && (status === 'loaded' || status === 'system');
}

function fontFileUrl(file: string): string {
  return `/__proxylens_font_lab__/file?name=${encodeURIComponent(file)}`;
}

function renderText(fontFamily: string, text: string): { width: number; alpha: Uint8ClampedArray } | null {
  const canvas = document.createElement('canvas');
  canvas.width = 1024;
  canvas.height = 96;
  const context = canvas.getContext('2d');
  if (!context) return null;
  context.font = `400 64px ${fontFamily}`;
  context.textBaseline = 'top';
  context.fillStyle = '#000';
  context.fillText(text, 8, 8);
  return {
    width: context.measureText(text).width,
    alpha: context.getImageData(0, 0, canvas.width, canvas.height).data,
  };
}

function hasGlyphs(fontFamily: string, text: string): boolean {
  const fallbackFamily = `"${MISSING_FONT_FAMILY}", sans-serif`;
  const actualFamily = `"${fontFamily}", ${fallbackFamily}`;
  const fallback = renderText(fallbackFamily, text);
  const actual = renderText(actualFamily, text);
  if (!fallback || !actual) return false;
  if (Math.abs(actual.width - fallback.width) > 0.5) return true;

  let differenceScore = 0;
  for (let index = 3; index < actual.alpha.length; index += 4) {
    differenceScore += Math.abs(actual.alpha[index] - fallback.alpha[index]);
  }
  return differenceScore > 128;
}

function systemFontAvailable(candidate: FontCandidate): boolean {
  const family = candidate.systemFamily ?? candidate.family ?? '';
  if (!family || !document.fonts.check(`400 16px "${family}"`)) return false;
  return candidate.coverage.every((language) => hasGlyphs(family, COVERAGE_SAMPLES[language]));
}

function candidateWeightDescription(candidate: FontCandidate): string {
  const weights = [...new Set((candidate.assets ?? []).map((asset) => asset.weight.trim()).filter(Boolean))];
  if (weights.length === 0) return 'UI 400 / 500';
  if (weights.length === 1) {
    const variableRange = weights[0].match(/^(\d+)\s+(\d+)$/);
    if (variableRange) return `Axis: ${variableRange[1]}–${variableRange[2]} · UI 400 / 500`;
  }
  return `Faces: ${weights.join(' / ')}`;
}

async function readLocalManifest(): Promise<{ manifest: LocalManifest; error: string | null }> {
  try {
    const response = await fetch(FONT_LAB_MANIFEST_URL, { cache: 'no-store' });
    if (!response.ok) throw new Error(`${response.status} ${response.statusText}`);
    const value = await response.json() as Partial<LocalManifest>;
    if (!value.candidates || typeof value.candidates !== 'object') throw new Error('invalid local manifest');
    return { manifest: { version: value.version ?? 1, candidates: value.candidates as Record<string, LocalCandidate> }, error: null };
  } catch (error: unknown) {
    return {
      manifest: { version: 1, candidates: {} },
      error: error instanceof Error ? error.message : 'local manifest unavailable',
    };
  }
}

async function loadLocalCandidate(candidate: FontCandidate, localCandidate: LocalCandidate): Promise<void> {
  if (localCandidate.status !== 'loaded' || localCandidate.faces.length === 0) {
    throw new Error(localCandidate.error ?? 'run npm run fontlab:setup first');
  }

  await Promise.all(localCandidate.faces.map(async (face) => {
    const language = face.locale ?? (candidate.coverage.includes('zh-CN') ? 'zh-CN' : 'en');
    const family = candidateFamily(candidate, language);
    const response = await fetch(fontFileUrl(face.file), { cache: 'no-store' });
    if (!response.ok) throw new Error(`${face.file}: ${response.status} ${response.statusText}`);
    const source = await response.arrayBuffer();
    const fontFace = new FontFace(family, source, {
      style: 'normal',
      weight: face.weight,
      ...(face.unicodeRange ? { unicodeRange: face.unicodeRange } : {}),
    });
    const loadedFace = await fontFace.load();
    document.fonts.add(loadedFace);
  }));

  const missingLanguage = candidate.coverage.find((language) => (
    !hasGlyphs(candidateFamily(candidate, language), COVERAGE_SAMPLES[language])
  ));
  if (missingLanguage) throw new Error(`loaded font does not cover ${missingLanguage === 'en' ? 'Latin' : 'CJK'} sample text`);
}

function firstSelectable(
  candidates: FontCandidate[],
  language: FontLanguage,
  statuses: Record<string, FontLabStatus>,
): FontCandidate | undefined {
  return candidates.find((candidate) => isSelectable(candidate, language, statuses));
}

function safeLocalFamily(fileName: string): string {
  const stem = fileName
    .replace(/\.[^.]+$/, '')
    .replace(/[^\w\u3400-\u9fff -]+/g, ' ')
    .replace(/\s+/g, ' ')
    .trim();
  return (stem || 'Local Font').slice(0, 80);
}

const FontProbe: React.FC<{
  candidate: FontCandidate;
  language: FontLanguage;
  status: FontLabStatus;
  message?: string;
}> = ({ candidate, language, status, message }) => {
  const family = candidateFamily(candidate, language);
  const renderedFamily = status === 'loaded' || status === 'system' ? family : '—';
  const axisLabel = language === 'en' ? 'Latin' : 'CJK';

  return (
    <div className="pl-font-lab__probe" data-font-lab-probe data-status={status} data-font-lab-rendered-family={renderedFamily}>
      <span className="pl-font-lab__status">Status: {STATUS_LABELS[status]}</span>
      <span>Rendered: {renderedFamily} · {axisLabel} · {candidateWeightDescription(candidate)}</span>
      {message && <span className="pl-font-lab__probe-note">{message}</span>}
    </div>
  );
};

export const FontLab: React.FC = () => {
  const [manualCandidates, setManualCandidates] = useState<FontCandidate[]>([]);
  const [statuses, setStatuses] = useState<Record<string, FontLabStatus>>(initialStatuses);
  const [statusMessages, setStatusMessages] = useState<Record<string, string>>({});
  const [ready, setReady] = useState(false);
  const [latinFontId, setLatinFontId] = useState(DEFAULT_LATIN_FONT_ID);
  const [cjkFontId, setCjkFontId] = useState(DEFAULT_CJK_FONT_ID);
  const [activeAxis, setActiveAxis] = useState<FontAxis>('latin');
  const [actionMessage, setActionMessage] = useState<string | null>(null);
  const [loadError, setLoadError] = useState<string | null>(null);
  const candidates = useMemo(() => [...BASE_CANDIDATES, ...manualCandidates], [manualCandidates]);
  const latinOptions = candidates.filter((candidate) => supportsLanguage(candidate, 'en'));
  const cjkOptions = candidates.filter((candidate) => supportsLanguage(candidate, 'zh-CN'));
  const latinFont = candidates.find((candidate) => candidate.id === latinFontId);
  const cjkFont = candidates.find((candidate) => candidate.id === cjkFontId);

  useEffect(() => {
    let active = true;
    const initialize = async () => {
      const { manifest: localManifest, error: manifestError } = await readLocalManifest();
      if (!active) return;
      const nextStatuses = initialStatuses();
      const nextMessages: Record<string, string> = {};
      if (manifestError) setLoadError(manifestError);

      await document.fonts.ready;
      for (const candidate of BASE_CANDIDATES) {
        if (candidate.kind === 'bundled') {
          nextStatuses[candidate.id] = 'loaded';
        } else if (candidate.kind === 'system') {
          nextStatuses[candidate.id] = systemFontAvailable(candidate) ? 'system' : 'unavailable';
          if (nextStatuses[candidate.id] === 'unavailable') nextMessages[candidate.id] = 'This Windows system font is not available.';
        } else {
          const localCandidate = localManifest.candidates[candidate.id];
          if (!localCandidate || localCandidate.status !== 'loaded') {
            nextStatuses[candidate.id] = 'unavailable';
            nextMessages[candidate.id] = localCandidate?.error ?? 'Run npm run fontlab:setup to prepare the official local file.';
          }
        }
      }
      setStatuses(nextStatuses);
      setStatusMessages(nextMessages);

      const localCandidates = BASE_CANDIDATES.filter((candidate) => {
        const entry = localManifest.candidates[candidate.id];
        return candidate.kind === 'local' && entry?.status === 'loaded';
      });
      const results = await Promise.allSettled(localCandidates.map(async (candidate) => {
        await loadLocalCandidate(candidate, localManifest.candidates[candidate.id]);
        return candidate.id;
      }));
      if (!active) return;

      const finalStatuses = { ...nextStatuses };
      const finalMessages = { ...nextMessages };
      results.forEach((result, index) => {
        const candidate = localCandidates[index];
        if (result.status === 'fulfilled') {
          finalStatuses[candidate.id] = 'loaded';
        } else {
          finalStatuses[candidate.id] = 'unavailable';
          finalMessages[candidate.id] = result.reason instanceof Error ? result.reason.message : 'Unable to load the local font.';
        }
      });
      setStatuses(finalStatuses);
      setStatusMessages(finalMessages);
      setReady(true);
    };

    void initialize();
    return () => {
      active = false;
    };
  }, []);

  useEffect(() => {
    if (!ready) return;
    const nextLatin = candidates.find((candidate) => candidate.id === latinFontId && isSelectable(candidate, 'en', statuses))
      ?? firstSelectable(candidates, 'en', statuses);
    const nextCjk = candidates.find((candidate) => candidate.id === cjkFontId && isSelectable(candidate, 'zh-CN', statuses))
      ?? firstSelectable(candidates, 'zh-CN', statuses);
    if (nextLatin && nextLatin.id !== latinFontId) setLatinFontId(nextLatin.id);
    if (nextCjk && nextCjk.id !== cjkFontId) setCjkFontId(nextCjk.id);
  }, [cjkFontId, candidates, latinFontId, ready, statuses]);

  useEffect(() => {
    const root = document.documentElement;
    const previousLatin = root.style.getPropertyValue('--pl-font-narrative-latin');
    const previousLatinPriority = root.style.getPropertyPriority('--pl-font-narrative-latin');
    const previousCjk = root.style.getPropertyValue('--pl-font-narrative-cjk');
    const previousCjkPriority = root.style.getPropertyPriority('--pl-font-narrative-cjk');

    root.style.removeProperty('--pl-narrative-override');
    if (ready && latinFont && cjkFont && isSelectable(latinFont, 'en', statuses) && isSelectable(cjkFont, 'zh-CN', statuses)) {
      root.style.setProperty('--pl-font-narrative-latin', cssFamily(candidateFamily(latinFont, 'en')));
      root.style.setProperty('--pl-font-narrative-cjk', cssFamily(candidateFamily(cjkFont, 'zh-CN')));
    } else {
      root.style.removeProperty('--pl-font-narrative-latin');
      root.style.removeProperty('--pl-font-narrative-cjk');
    }

    return () => {
      if (previousLatin) root.style.setProperty('--pl-font-narrative-latin', previousLatin, previousLatinPriority);
      else root.style.removeProperty('--pl-font-narrative-latin');
      if (previousCjk) root.style.setProperty('--pl-font-narrative-cjk', previousCjk, previousCjkPriority);
      else root.style.removeProperty('--pl-font-narrative-cjk');
    };
  }, [cjkFont, latinFont, ready, statuses]);

  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if (!event.altKey || (event.key !== 'ArrowDown' && event.key !== 'ArrowUp')) return;
      event.preventDefault();
      const language = axisLanguage(activeAxis);
      const cycleCandidates = candidates.filter((candidate) => isSelectable(candidate, language, statuses));
      if (cycleCandidates.length < 2) return;
      const currentId = activeAxis === 'latin' ? latinFontId : cjkFontId;
      const currentIndex = Math.max(0, cycleCandidates.findIndex((candidate) => candidate.id === currentId));
      const direction = event.key === 'ArrowDown' ? 1 : -1;
      const next = cycleCandidates[(currentIndex + direction + cycleCandidates.length) % cycleCandidates.length];
      if (activeAxis === 'latin') setLatinFontId(next.id);
      else setCjkFontId(next.id);
      setActionMessage(null);
    };

    window.addEventListener('keydown', onKeyDown);
    return () => window.removeEventListener('keydown', onKeyDown);
  }, [activeAxis, candidates, cjkFontId, latinFontId, statuses]);

  const selectFont = (axis: FontAxis, id: string) => {
    const language = axisLanguage(axis);
    const candidate = candidates.find((item) => item.id === id);
    setActiveAxis(axis);
    if (!candidate || !isSelectable(candidate, language, statuses)) return;
    if (axis === 'latin') setLatinFontId(id);
    else setCjkFontId(id);
    setActionMessage(null);
  };

  const reset = () => {
    setLatinFontId(DEFAULT_LATIN_FONT_ID);
    setCjkFontId(DEFAULT_CJK_FONT_ID);
    setActiveAxis('latin');
    setActionMessage(null);
    setLoadError(null);
  };

  const handleLocalFont = async (event: React.ChangeEvent<HTMLInputElement>) => {
    const file = event.target.files?.[0];
    event.target.value = '';
    if (!file) return;
    const displayFamily = safeLocalFamily(file.name);
    const family = `ProxyLens Font Lab - ${displayFamily}`;
    setActionMessage(null);
    try {
      const face = new FontFace(family, await file.arrayBuffer(), { style: 'normal', weight: '400' });
      const loadedFace = await face.load();
      document.fonts.add(loadedFace);
      const hasLatin = hasGlyphs(family, COVERAGE_SAMPLES.en);
      const hasCjk = hasGlyphs(family, COVERAGE_SAMPLES['zh-CN']);
      if (!hasLatin && !hasCjk) throw new Error('font does not cover the Font Lab Latin or CJK sample');
      const coverage: FontLanguage[] = [
        ...(hasLatin ? ['en' as const] : []),
        ...(hasCjk ? ['zh-CN' as const] : []),
      ];
      const id = `manual-${displayFamily.toLowerCase().replace(/[^a-z0-9]+/g, '-')}-${Date.now()}`;
      const candidate: FontCandidate = {
        id,
        label: `${displayFamily} (local)`,
        kind: 'local',
        family,
        coverage,
      };
      setManualCandidates((current) => [...current, candidate]);
      setStatuses((current) => ({ ...current, [id]: 'loaded' }));
      setActionMessage(`Loaded ${displayFamily} · ${coverage.map((language) => language === 'en' ? 'Latin' : 'CJK').join(' + ')}`);
    } catch (error: unknown) {
      setActionMessage(error instanceof Error ? error.message : 'Unable to load the local font.');
    }
  };

  return (
    <aside className="pl-font-lab" aria-label="Typography Lab">
      <div className="pl-font-lab__title">Typography Lab</div>
      <div className="pl-font-lab__field" data-active={activeAxis === 'latin'}>
        <label htmlFor="font-lab-latin">Latin Narrative</label>
        <div className="pl-font-lab__select-wrap">
          <select
            id="font-lab-latin"
            value={latinFontId}
            onFocus={() => setActiveAxis('latin')}
            onChange={(event) => selectFont('latin', event.target.value)}
          >
            {latinOptions.map((candidate) => (
              <option key={candidate.id} value={candidate.id} disabled={!isSelectable(candidate, 'en', statuses)}>
                {candidate.label} · {STATUS_LABELS[statuses[candidate.id] ?? 'loading']}
              </option>
            ))}
          </select>
        </div>
        {latinFont && <FontProbe candidate={latinFont} language="en" status={statuses[latinFont.id] ?? 'loading'} message={statusMessages[latinFont.id]} />}
      </div>
      <div className="pl-font-lab__field" data-active={activeAxis === 'cjk'}>
        <label htmlFor="font-lab-cjk">CJK Narrative</label>
        <div className="pl-font-lab__select-wrap">
          <select
            id="font-lab-cjk"
            value={cjkFontId}
            onFocus={() => setActiveAxis('cjk')}
            onChange={(event) => selectFont('cjk', event.target.value)}
          >
            {cjkOptions.map((candidate) => (
              <option key={candidate.id} value={candidate.id} disabled={!isSelectable(candidate, 'zh-CN', statuses)}>
                {candidate.label} · {STATUS_LABELS[statuses[candidate.id] ?? 'loading']}
              </option>
            ))}
          </select>
        </div>
        {cjkFont && <FontProbe candidate={cjkFont} language="zh-CN" status={statuses[cjkFont.id] ?? 'loading'} message={statusMessages[cjkFont.id]} />}
      </div>
      <div className="pl-font-lab__field">
        <label>Technical / Evidence</label>
        <div className="pl-font-lab__fixed">JetBrains Mono · fixed</div>
      </div>
      <div className="pl-font-lab__focus">Focused: {activeAxis === 'latin' ? 'Latin Narrative' : 'CJK Narrative'}</div>
      <div className="pl-font-lab__actions">
        <button type="button" className="pl-font-lab__button" onClick={reset}>Reset</button>
      </div>
      <label className="pl-font-lab__button pl-font-lab__load">
        Load local font
        <input type="file" accept=".ttf,.otf,.woff,.woff2" onChange={handleLocalFont} />
      </label>
      <div className="pl-font-lab__note">Alt+↑ / Alt+↓ &nbsp;Cycle focused font</div>
      <div className="pl-font-lab__note">Run <code>npm run fontlab:setup</code> for official local candidates.</div>
      <div className="pl-font-lab__note">Fonts stay outside the build in <code>ui/.font-lab/</code>.</div>
      {loadError && <div className="pl-font-lab__message pl-font-lab__message--error">Setup manifest: {loadError}</div>}
      {actionMessage && <div className="pl-font-lab__message">{actionMessage}</div>}
    </aside>
  );
};
