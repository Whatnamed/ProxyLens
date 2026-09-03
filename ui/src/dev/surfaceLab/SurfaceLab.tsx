import React, { useEffect, useState } from 'react';
import { useAuditContext } from '../../state/AuditContext';
import './surfaceLab.css';

type SurfacePreset = {
  readonly id: string;
  readonly label: string;
  readonly light: string;
  readonly dark: string;
};

type SurfaceFamily = {
  readonly id: string;
  readonly label: string;
  readonly presets: readonly SurfacePreset[];
};

const SURFACE_FAMILIES: readonly SurfaceFamily[] = [
  {
    id: 'neutral',
    label: 'Neutral',
    presets: [
      { id: 'baseline', label: 'Baseline', light: '#fafaf8', dark: '#151715' },
      { id: 'neutral-veil', label: 'Neutral Veil', light: '#f8f8f6', dark: '#171917' },
      { id: 'neutral-layered', label: 'Neutral Layered', light: '#f5f6f3', dark: '#191c19' },
    ],
  },
  {
    id: 'warm',
    label: 'Warm',
    presets: [
      { id: 'light-stone', label: 'Light Stone', light: '#f8f6f3', dark: '#191817' },
      { id: 'warm-paper', label: 'Warm Paper', light: '#f8f7f4', dark: '#191817' },
      { id: 'mushroom-mist', label: 'Mushroom Mist', light: '#f7f5f3', dark: '#1a1818' },
    ],
  },
  {
    id: 'earth-gray',
    label: 'Earth / Gray',
    presets: [
      { id: 'soft-greige', label: 'Soft Greige', light: '#f6f6f2', dark: '#191a17' },
      { id: 'ash', label: 'Ash', light: '#f6f7f4', dark: '#181b18' },
      { id: 'taupe-gray', label: 'Taupe Gray', light: '#f7f5f4', dark: '#1a1918' },
    ],
  },
  {
    id: 'cool',
    label: 'Cool',
    presets: [
      { id: 'porcelain', label: 'Porcelain', light: '#f6f8f7', dark: '#171a1a' },
      { id: 'fog', label: 'Fog', light: '#f5f7f7', dark: '#181a1b' },
      { id: 'slate-veil', label: 'Slate Veil', light: '#f5f6f7', dark: '#181a1c' },
    ],
  },
];

const SURFACE_PRESETS = SURFACE_FAMILIES.flatMap((family) => family.presets);

function presetFor(id: string): SurfacePreset {
  return SURFACE_PRESETS.find((preset) => preset.id === id) ?? SURFACE_PRESETS[0];
}

function restoreStyle(root: HTMLElement, property: string, value: string, priority: string): void {
  if (value) {
    root.style.setProperty(property, value, priority);
  } else {
    root.style.removeProperty(property);
  }
}

export const SurfaceLab: React.FC = () => {
  const { theme } = useAuditContext();
  const [expanded, setExpanded] = useState(false);
  const [presetId, setPresetId] = useState('baseline');
  const preset = presetFor(presetId);

  useEffect(() => {
    const root = document.documentElement;
    const previousValue = root.style.getPropertyValue('--pl-inspector');
    const previousPriority = root.style.getPropertyPriority('--pl-inspector');
    root.style.setProperty('--pl-inspector', preset[theme]);
    return () => restoreStyle(root, '--pl-inspector', previousValue, previousPriority);
  }, [preset, theme]);

  return (
    <aside className="pl-surface-lab" aria-label="Surface Lab">
      {expanded ? (
        <div className="pl-surface-lab__panel">
          <div className="pl-surface-lab__header">
            <div>
              <div className="pl-eyebrow">Inspector surface</div>
              <strong>Surface Lab</strong>
            </div>
            <button
              type="button"
              className="pl-icon-btn"
              aria-label="Collapse Surface Lab"
              title="Collapse Surface Lab"
              onClick={() => setExpanded(false)}
            >
              ×
            </button>
          </div>

          <div className="pl-surface-lab__current">
            <span>Current theme</span>
            <span className="pl-mono">{theme === 'light' ? 'Light' : 'Dark'} · {preset[theme]}</span>
          </div>

          <div className="pl-surface-lab__families" role="group" aria-label="Surface presets">
            {SURFACE_FAMILIES.map((family) => (
              <div key={family.id} className="pl-surface-lab__family">
                <div className="pl-eyebrow pl-surface-lab__family-heading">{family.label}</div>
                <div className="pl-surface-lab__presets">
                  {family.presets.map((option) => (
                    <button
                      key={option.id}
                      type="button"
                      className={`pl-surface-lab__preset${option.id === presetId ? ' pl-surface-lab__preset--active' : ''}`}
                      aria-pressed={option.id === presetId}
                      onClick={() => setPresetId(option.id)}
                    >
                      <span className="pl-surface-lab__swatches" aria-hidden="true">
                        <span style={{ background: option.light }} />
                        <span style={{ background: option.dark }} />
                      </span>
                      <span className="pl-surface-lab__preset-copy">
                        <span className="pl-surface-lab__preset-name">{option.label}</span>
                        <span className="pl-surface-lab__values">
                          <span>Light <code>{option.light}</code></span>
                          <span>Dark <code>{option.dark}</code></span>
                        </span>
                      </span>
                    </button>
                  ))}
                </div>
              </div>
            ))}
          </div>
        </div>
      ) : null}
      <button
        type="button"
        className="pl-surface-lab__trigger"
        aria-expanded={expanded}
        aria-label={expanded ? 'Collapse Surface Lab' : 'Open Surface Lab'}
        title={expanded ? 'Collapse Surface Lab' : 'Open Surface Lab'}
        onClick={() => setExpanded((current) => !current)}
      >
        ◐
      </button>
    </aside>
  );
};
