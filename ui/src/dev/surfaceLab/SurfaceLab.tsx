import React, { useEffect, useState } from 'react';
import { useAuditContext } from '../../state/AuditContext';
import './surfaceLab.css';

const SURFACE_PRESETS = [
  { id: 'baseline', label: 'Baseline', light: '#fafaf8', dark: '#151715' },
  { id: 'soft', label: 'Soft', light: '#f7f8f6', dark: '#181a18' },
  { id: 'layered', label: 'Layered', light: '#f5f6f3', dark: '#191c19' },
  { id: 'defined', label: 'Defined', light: '#f3f5f2', dark: '#1a1d1b' },
] as const;

type SurfacePreset = (typeof SURFACE_PRESETS)[number];

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

          <div className="pl-surface-lab__presets" role="group" aria-label="Surface presets">
            {SURFACE_PRESETS.map((option) => (
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
