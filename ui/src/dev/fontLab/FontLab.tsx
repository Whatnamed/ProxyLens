import React, { useEffect, useMemo, useState } from 'react';
import { useLocale } from '../../state/AuditContext';
import manifest from './font-lab-manifest.json';
import './fontLab.css';

type FontLanguage = 'en' | 'zh-CN';
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
const DEFAULT_FONT_ID = 'ibm-plex-sans-sc';
const FONT_LAB_MANIFEST_URL = '/__proxylens_font_lab__/manifest';
const MISSING_FONT_FAMILY = '__ProxyLens_Font_Lab_Missing__';

const STATUS_LABELS: Record<FontLabStatus, string> = {
  loading: 'Loading',
  loaded: 'Loaded',
  system: 'System available',
  unavailable: 'Unavailable',
};

function initialStatuses(): Record<string, FontLabStatus> {
  return Object.fromEntries(
    BASE_CANDIDATES.map((candidate) => [candidate.id, candidate.kind === 'bundled' ? 'loaded' : 'loading']),
  );
}

function candidateFamily(candidate: FontCandidate, language: FontLanguage): string {
  return candidate.families?.[language] ?? candidate.family ?? candidate.systemFamily ?? '';
}

function cssFamily(family: string): string {
  const safeFamily = family.replace(/["\\]/g, '');
  return `"${safeFamily}", var(--pl-font-narrative)`;
}

function supportsLanguage(candidate: FontCandidate, language: FontLanguage): boolean {
  return candidate.coverage.includes(language);
}

function isSelectable(candidate: FontCandidate, language: FontLanguage, statuses: Record<string, FontLabStatus>): boolean {
  const status = statuses[candidate.id];
  return supportsLanguage(candidate, language) && (status === 'loaded' || status === 'system');
}

function isBilingualSelectable(candidate: FontCandidate, statuses: Record<string, FontLabStatus>): boolean {
  return isSelectable(candidate, 'en', statuses) && isSelectable(candidate, 'zh-CN', statuses);
}

function fontFileUrl(file: string): string {
  return `/__proxylens_font_lab__/file?name=${encodeURIComponent(file)}`;
}

function measureTextWidth(fontFamily: string, text: string): number {
  const canvas = document.createElement('canvas');
  const context = canvas.getContext('2d');
  if (!context) return 0;
  context.font = `400 64px ${fontFamily}`;
  return context.measureText(text).width;
}

function hasGlyphs(fontFamily: string, text: string): boolean {
  const fallback = measureTextWidth(`"${MISSING_FONT_FAMILY}", sans-serif`, text);
  const actual = measureTextWidth(`"${fontFamily}", "${MISSING_FONT_FAMILY}", sans-serif`, text);
  return Math.abs(actual - fallback) > 0.5;
}

function systemFontAvailable(family: string): boolean {
  if (!document.fonts.check(`400 16px "${family}"`, 'AaMm')) return false;
  return ['AaMmWw0123', '中文测试'].some((sample) => hasGlyphs(family, sample));
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
    await fontFace.load();
    document.fonts.add(fontFace);
  }));
}

function firstSelectable(
  candidates: FontCandidate[],
  language: FontLanguage,
  statuses: Record<string, FontLabStatus>,
): FontCandidate | undefined {
  return candidates.find((candidate) => isSelectable(candidate, language, statuses));
}

function firstBilingualSelectable(candidates: FontCandidate[], statuses: Record<string, FontLabStatus>): FontCandidate | undefined {
  return candidates.find((candidate) => isBilingualSelectable(candidate, statuses));
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
  const coverage = candidate.coverage.length === 2 ? 'EN + ZH' : candidate.coverage[0] === 'en' ? 'EN only' : 'ZH only';
  const renderedFamily = status === 'loaded' || status === 'system' ? family : '—';

  return (
    <div className="pl-font-lab__probe" data-font-lab-probe data-status={status} data-font-lab-rendered-family={renderedFamily}>
      <span className="pl-font-lab__status">Status: {STATUS_LABELS[status]}</span>
      <span>Rendered: {renderedFamily} · {coverage} · 400 / 500</span>
      {message && <span className="pl-font-lab__probe-note">{message}</span>}
    </div>
  );
};

export const FontLab: React.FC = () => {
  const { locale } = useLocale();
  const [manualCandidates, setManualCandidates] = useState<FontCandidate[]>([]);
  const [statuses, setStatuses] = useState<Record<string, FontLabStatus>>(initialStatuses);
  const [statusMessages, setStatusMessages] = useState<Record<string, string>>({});
  const [ready, setReady] = useState(false);
  const [englishFontId, setEnglishFontId] = useState(DEFAULT_FONT_ID);
  const [chineseFontId, setChineseFontId] = useState(DEFAULT_FONT_ID);
  const [linked, setLinked] = useState(false);
  const [actionMessage, setActionMessage] = useState<string | null>(null);
  const [loadError, setLoadError] = useState<string | null>(null);
  const candidates = useMemo(() => [...BASE_CANDIDATES, ...manualCandidates], [manualCandidates]);
  const activeLanguage: FontLanguage = locale === 'zh-CN' ? 'zh-CN' : 'en';
  const englishOptions = candidates.filter((candidate) => supportsLanguage(candidate, 'en'));
  const chineseOptions = candidates.filter((candidate) => supportsLanguage(candidate, 'zh-CN'));
  const englishFont = candidates.find((candidate) => candidate.id === englishFontId) ?? candidates[0];
  const chineseFont = candidates.find((candidate) => candidate.id === chineseFontId) ?? candidates[0];
  const activeFont = activeLanguage === 'en' ? englishFont : chineseFont;

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
          const systemFamily = candidate.systemFamily ?? candidate.family ?? '';
          nextStatuses[candidate.id] = systemFontAvailable(systemFamily) ? 'system' : 'unavailable';
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
    if (linked) {
      const current = candidates.find((candidate) => candidate.id === englishFontId && isBilingualSelectable(candidate, statuses));
      const next = current ?? firstBilingualSelectable(candidates, statuses);
      if (next && (englishFontId !== next.id || chineseFontId !== next.id)) {
        setEnglishFontId(next.id);
        setChineseFontId(next.id);
      }
      return;
    }

    const nextEnglish = candidates.find((candidate) => candidate.id === englishFontId && isSelectable(candidate, 'en', statuses))
      ?? firstSelectable(candidates, 'en', statuses);
    const nextChinese = candidates.find((candidate) => candidate.id === chineseFontId && isSelectable(candidate, 'zh-CN', statuses))
      ?? firstSelectable(candidates, 'zh-CN', statuses);
    if (nextEnglish && nextEnglish.id !== englishFontId) setEnglishFontId(nextEnglish.id);
    if (nextChinese && nextChinese.id !== chineseFontId) setChineseFontId(nextChinese.id);
  }, [candidates, chineseFontId, englishFontId, linked, ready, statuses]);

  useEffect(() => {
    const root = document.documentElement;
    if (!ready || !activeFont || !isSelectable(activeFont, activeLanguage, statuses)) {
      root.style.removeProperty('--pl-narrative-override');
      return;
    }
    root.style.setProperty('--pl-narrative-override', cssFamily(candidateFamily(activeFont, activeLanguage)));
    return () => {
      root.style.removeProperty('--pl-narrative-override');
    };
  }, [activeFont, activeLanguage, ready, statuses]);

  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if (!event.altKey || (event.key !== 'ArrowDown' && event.key !== 'ArrowUp')) return;
      event.preventDefault();
      const cycleCandidates = candidates.filter((candidate) => (
        isSelectable(candidate, activeLanguage, statuses) && (!linked || isBilingualSelectable(candidate, statuses))
      ));
      if (cycleCandidates.length < 2) return;
      const currentId = activeLanguage === 'en' ? englishFontId : chineseFontId;
      const currentIndex = Math.max(0, cycleCandidates.findIndex((candidate) => candidate.id === currentId));
      const direction = event.key === 'ArrowDown' ? 1 : -1;
      const next = cycleCandidates[(currentIndex + direction + cycleCandidates.length) % cycleCandidates.length];
      if (linked) {
        setEnglishFontId(next.id);
        setChineseFontId(next.id);
      } else if (activeLanguage === 'en') {
        setEnglishFontId(next.id);
      } else {
        setChineseFontId(next.id);
      }
      setActionMessage(null);
    };

    window.addEventListener('keydown', onKeyDown);
    return () => window.removeEventListener('keydown', onKeyDown);
  }, [activeLanguage, candidates, chineseFontId, englishFontId, linked, statuses]);

  const selectFont = (language: FontLanguage, id: string) => {
    const candidate = candidates.find((item) => item.id === id);
    if (!candidate || !isSelectable(candidate, language, statuses)) return;
    if (linked) {
      if (!isBilingualSelectable(candidate, statuses)) {
        setActionMessage('Link EN / ZH requires a loaded bilingual candidate.');
        return;
      }
      setEnglishFontId(id);
      setChineseFontId(id);
    } else if (language === 'en') {
      setEnglishFontId(id);
    } else {
      setChineseFontId(id);
    }
    setActionMessage(null);
  };

  const handleLinkChange = (event: React.ChangeEvent<HTMLInputElement>) => {
    if (!event.target.checked) {
      setLinked(false);
      setActionMessage(null);
      return;
    }
    const current = candidates.find((candidate) => candidate.id === englishFontId && isBilingualSelectable(candidate, statuses));
    const next = current ?? firstBilingualSelectable(candidates, statuses);
    if (!next) {
      setActionMessage('No loaded bilingual candidate is available.');
      return;
    }
    setEnglishFontId(next.id);
    setChineseFontId(next.id);
    setLinked(true);
    setActionMessage(null);
  };

  const reset = () => {
    setEnglishFontId(DEFAULT_FONT_ID);
    setChineseFontId(DEFAULT_FONT_ID);
    setLinked(false);
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
      const face = new FontFace(family, await file.arrayBuffer(), { style: 'normal', weight: '400 900' });
      await face.load();
      document.fonts.add(face);
      const hasHan = hasGlyphs(family, '中文');
      const id = `manual-${displayFamily.toLowerCase().replace(/[^a-z0-9]+/g, '-')}-${Date.now()}`;
      const candidate: FontCandidate = {
        id,
        label: `${displayFamily} (local)`,
        kind: 'local',
        family,
        coverage: hasHan ? ['en', 'zh-CN'] : ['en'],
      };
      setManualCandidates((current) => [...current, candidate]);
      setStatuses((current) => ({ ...current, [id]: 'loaded' }));
      setActionMessage(`Loaded ${displayFamily} · ${hasHan ? 'EN + ZH' : 'EN only'}`);
    } catch (error: unknown) {
      setActionMessage(error instanceof Error ? error.message : 'Unable to load the local font.');
    }
  };

  return (
    <aside className="pl-font-lab" aria-label="Typography Lab">
      <div className="pl-font-lab__title">Typography Lab</div>
      <div className="pl-font-lab__field">
        <label htmlFor="font-lab-english">English Narrative</label>
        <div className="pl-font-lab__select-wrap">
          <select id="font-lab-english" value={englishFontId} onChange={(event) => selectFont('en', event.target.value)}>
            {englishOptions.map((candidate) => (
              <option
                key={candidate.id}
                value={candidate.id}
                disabled={!isSelectable(candidate, 'en', statuses) || (linked && !isBilingualSelectable(candidate, statuses))}
              >
                {candidate.label} · {STATUS_LABELS[statuses[candidate.id] ?? 'loading']}
              </option>
            ))}
          </select>
        </div>
        <FontProbe candidate={englishFont} language="en" status={statuses[englishFont.id] ?? 'loading'} message={statusMessages[englishFont.id]} />
      </div>
      <div className="pl-font-lab__field">
        <label htmlFor="font-lab-chinese">Chinese Narrative</label>
        <div className="pl-font-lab__select-wrap">
          <select id="font-lab-chinese" value={chineseFontId} onChange={(event) => selectFont('zh-CN', event.target.value)}>
            {chineseOptions.map((candidate) => (
              <option
                key={candidate.id}
                value={candidate.id}
                disabled={!isSelectable(candidate, 'zh-CN', statuses) || (linked && !isBilingualSelectable(candidate, statuses))}
              >
                {candidate.label} · {STATUS_LABELS[statuses[candidate.id] ?? 'loading']}
              </option>
            ))}
          </select>
        </div>
        <FontProbe candidate={chineseFont} language="zh-CN" status={statuses[chineseFont.id] ?? 'loading'} message={statusMessages[chineseFont.id]} />
      </div>
      <div className="pl-font-lab__actions">
        <label className="pl-font-lab__link">
          <input type="checkbox" checked={linked} onChange={handleLinkChange} />
          Link EN / ZH
        </label>
        <button type="button" className="pl-font-lab__button" onClick={reset}>Reset</button>
      </div>
      <label className="pl-font-lab__button pl-font-lab__load">
        Load local font
        <input type="file" accept=".ttf,.otf,.woff,.woff2" onChange={handleLocalFont} />
      </label>
      <div className="pl-font-lab__note">Alt+↑ / Alt+↓ &nbsp;Cycle font</div>
      <div className="pl-font-lab__note">Run <code>npm run fontlab:setup</code> for official local candidates.</div>
      <div className="pl-font-lab__note">Fonts stay outside the build in <code>ui/.font-lab/</code>.</div>
      {loadError && <div className="pl-font-lab__message pl-font-lab__message--error">Setup manifest: {loadError}</div>}
      {actionMessage && <div className="pl-font-lab__message">{actionMessage}</div>}
    </aside>
  );
};
