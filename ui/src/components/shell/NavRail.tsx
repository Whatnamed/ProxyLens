import React from 'react';
import { SurfaceKey } from '../../state/AuditContext';
import { QueryApiClient } from '../../api/client';
import { SystemStatus } from './SystemStatus';
import { IconOverview, IconHistory, IconCoverage } from '../ui/icons';

/**
 * Three destinations only.
 *
 * Connection Detail is not here — it is a contextual Inspector opened from
 * History. System Status is not here either — it is a persistent secondary
 * affordance anchored at the bottom of this rail.
 */

const NAV_ITEMS: { key: SurfaceKey; label: string; icon: React.ReactNode; hint: string }[] = [
  { key: 'overview', label: 'Overview', icon: <IconOverview size={15} />, hint: 'Scan aggregate proxy behaviour' },
  { key: 'history', label: 'History', icon: <IconHistory size={15} />, hint: 'Investigate underlying connections' },
  { key: 'coverage', label: 'Coverage', icon: <IconCoverage size={15} />, hint: 'Verify evidence completeness' }
];

interface Props {
  surface: SurfaceKey;
  onNavigate: (s: SurfaceKey) => void;
  client: QueryApiClient | null;
}

export const NavRail: React.FC<Props> = ({ surface, onNavigate, client }) => (
  <nav className="rail" aria-label="Primary">
    <div className="rail-brand">
      <div className="rail-mark">PL</div>
      <div className="rail-wordmark">
        <span className="rail-title">ProxyLens</span>
        <span className="rail-subtitle">Traffic Audit</span>
      </div>
    </div>

    <div className="rail-nav">
      <div className="rail-section-label">Audit</div>
      {NAV_ITEMS.map((item) => (
        <button
          key={item.key}
          className={`rail-item ${surface === item.key ? 'is-active' : ''}`}
          onClick={() => onNavigate(item.key)}
          aria-current={surface === item.key ? 'page' : undefined}
          title={item.hint}
        >
          <span className="rail-item-icon">{item.icon}</span>
          <span className="rail-item-label">{item.label}</span>
        </button>
      ))}
    </div>

    <div className="rail-spacer" />

    <div className="rail-footer">
      <SystemStatus client={client} />
    </div>
  </nav>
);
