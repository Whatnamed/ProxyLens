import React, { useEffect, useMemo, useState } from 'react';
import { useLocale } from '../../state/AuditContext';
import './fontLab.css';

type FontSource = 'bundled' | 'system' | 'local';
type HanSupport = 'yes' | 'no' | 'unknown';

interface FontOption {
  id: string;
  label: string;
  family: string;
  source: FontSource;
  hanSupport: HanSupport;
}

interface FontProbeResult {
  latinAvailable: boolean;
  hanAvailable: boolean;
}

const DEFAULT_FONT_ID = 'ibm-plex-sans-sc';

const BASE_FONTS: FontOption[] = [
  { id: 'ibm-plex-sans-sc', label: 'IBM Plex Sans SC', family: 'IBM Plex Sans SC', source: 'bundled', hanSupport: 'yes' },
  { id: 'public-sans', label: 'Public Sans', family: 'Public Sans', source: 'system', hanSupport: 'no' },
  { id: 'inter', label: 'Inter', family: 'Inter', source: 'system', hanSupport: 'no' },
  { id: 'geist-sans', label: 'Geist Sans', family: 'Geist Sans', source: 'system', hanSupport: 'no' },
  { id: 'manrope', label: 'Manrope', family: 'Manrope', source: 'system', hanSupport: 'no' },
  { id: 'segoe-ui', label: 'Segoe UI', family: 'Segoe UI', source: 'system', hanSupport: 'no' },
  { id: 'noto-sans-sc', label: 'Noto Sans SC', family: 'Noto Sans SC', source: 'system', hanSupport: 'yes' },
  { id: 'source-han-sans-sc', label: 'Source Han Sans SC', family: 'Source Han Sans SC', source: 'system', hanSupport: 'yes' },
  { id: 'microsoft-yahei-ui', label: 'Microsoft YaHei UI', family: 'Microsoft YaHei UI', source: 'system', hanSupport: 'yes' },
];

function cssFamily(family: string): string {
  const safeFamily = family.replace(/["\\]/g, '');
  return `"${safeFamily}", var(--pl-font-narrative)`;
}

function safeLocalFamily(fileName: string): string {
  const stem = fileName.replace(/\.[^.]+$/, '').replace(/[^\w\u3400-\u9fff -]+/g, ' ').replace(/\s+/g, ' ').trim();
  return (stem || 'Local Font').slice(0, 80);
}

function fontId(family: string): string {
  return `local-${family.toLowerCase().replace(/[^a-z0-9]+/g, '-')}-${Date.now()}`;
}

function measureGlyphs(family: string, text: string): number {
  const canvas = document.createElement('canvas');
  const context = canvas.getContext('2d');
  if (!context) return 0;
  context.font = `400 64px "${family.replace(/["\\]/g, '')}", sans-serif`;
  return context.measureText(text).width;
}

function probeFont(family: string): FontProbeResult {
  const latin = 'AaMm';
  const han = '中文';
  const latinFallback = measureGlyphs('sans-serif', latin);
  const hanFallback = measureGlyphs('sans-serif', han);
  return {
    latinAvailable: Math.abs(measureGlyphs(family, latin) - latinFallback) > 0.5,
    hanAvailable: Math.abs(measureGlyphs(family, han) - hanFallback) > 0.5,
  };
}

const FontProbe: React.FC<{ option: FontOption }> = ({ option }) => {
  const [probe, setProbe] = useState<FontProbeResult | null>(null);

  useEffect(() => {
    let active = true;
    void document.fonts.ready.then(() => {
      if (active) setProbe(probeFont(option.family));
    });
    return () => {
      active = false;
    };
  }, [option.family]);

  const rendered = probe === null ? 'checking…' : probe.latinAvailable ? option.family : 'fallback chain';
  const han = option.hanSupport === 'no'
    ? 'Han fallback likely'
    : option.source === 'bundled'
      ? 'Han candidate (bundled)'
      : probe === null
        ? 'Han checking…'
        : probe.hanAvailable
          ? 'Han candidate'
          : option.hanSupport === 'unknown'
            ? 'Han fallback possible'
            : 'Han probe inconclusive';

  return (
    <div className="pl-font-lab__probe" data-font-lab-probe>
      <span>Requested: {option.label}</span>
      <span>Rendered probe: {rendered} · {han}</span>
    </div>
  );
};

export const FontLab: React.FC = () => {
  const { locale } = useLocale();
  const [localFonts, setLocalFonts] = useState<FontOption[]>([]);
  const [englishFontId, setEnglishFontId] = useState(DEFAULT_FONT_ID);
  const [chineseFontId, setChineseFontId] = useState(DEFAULT_FONT_ID);
  const [linked, setLinked] = useState(false);
  const [loadMessage, setLoadMessage] = useState<string | null>(null);
  const [loadError, setLoadError] = useState<string | null>(null);
  const fonts = useMemo(() => [...BASE_FONTS, ...localFonts], [localFonts]);
  const englishFont = fonts.find((font) => font.id === englishFontId) ?? BASE_FONTS[0];
  const chineseFont = fonts.find((font) => font.id === chineseFontId) ?? BASE_FONTS[0];

  useEffect(() => {
    const root = document.documentElement;
    const previous = root.style.getPropertyValue('--pl-narrative-override');
    const activeFont = locale === 'zh-CN' ? chineseFont : englishFont;
    root.style.setProperty('--pl-narrative-override', cssFamily(activeFont.family));
    return () => {
      if (previous) root.style.setProperty('--pl-narrative-override', previous);
      else root.style.removeProperty('--pl-narrative-override');
    };
  }, [chineseFont.family, englishFont.family, locale]);

  const selectFont = (language: 'en' | 'zh-CN', id: string) => {
    if (language === 'en') {
      setEnglishFontId(id);
      if (linked) setChineseFontId(id);
    } else {
      setChineseFontId(id);
      if (linked) setEnglishFontId(id);
    }
  };

  const handleLinkChange = (event: React.ChangeEvent<HTMLInputElement>) => {
    const nextLinked = event.target.checked;
    setLinked(nextLinked);
    if (nextLinked) setChineseFontId(englishFontId);
  };

  const reset = () => {
    setEnglishFontId(DEFAULT_FONT_ID);
    setChineseFontId(DEFAULT_FONT_ID);
    setLinked(false);
    setLoadMessage(null);
    setLoadError(null);
  };

  const handleLocalFont = async (event: React.ChangeEvent<HTMLInputElement>) => {
    const file = event.target.files?.[0];
    event.target.value = '';
    if (!file) return;
    const family = safeLocalFamily(file.name);
    setLoadMessage(null);
    setLoadError(null);
    try {
      const face = new FontFace(family, await file.arrayBuffer(), { style: 'normal', weight: '400 900' });
      await face.load();
      document.fonts.add(face);
      setLocalFonts((current) => [...current, { id: fontId(family), label: `${family} (local)`, family, source: 'local', hanSupport: 'unknown' }]);
      setLoadMessage(`Loaded ${family}`);
    } catch (error: unknown) {
      setLoadError(error instanceof Error ? error.message : 'Unable to load font');
    }
  };

  return (
    <aside className="pl-font-lab" aria-label="Typography Lab">
      <div className="pl-font-lab__title">Typography Lab</div>
      <div className="pl-font-lab__field">
        <label htmlFor="font-lab-english">English Narrative</label>
        <div className="pl-font-lab__select-wrap">
          <select id="font-lab-english" value={englishFontId} onChange={(event) => selectFont('en', event.target.value)}>
            {fonts.map((font) => <option key={font.id} value={font.id}>{font.label}</option>)}
          </select>
        </div>
        <FontProbe option={englishFont} />
      </div>
      <div className="pl-font-lab__field">
        <label htmlFor="font-lab-chinese">Chinese Narrative</label>
        <div className="pl-font-lab__select-wrap">
          <select id="font-lab-chinese" value={chineseFontId} onChange={(event) => selectFont('zh-CN', event.target.value)}>
            {fonts.map((font) => <option key={font.id} value={font.id}>{font.label}</option>)}
          </select>
        </div>
        <FontProbe option={chineseFont} />
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
      <div className="pl-font-lab__note">Local files stay outside the build; use <code>ui/.font-lab/</code>.</div>
      {loadMessage && <div className="pl-font-lab__message">{loadMessage}</div>}
      {loadError && <div className="pl-font-lab__message pl-font-lab__message--error">{loadError}</div>}
    </aside>
  );
};
