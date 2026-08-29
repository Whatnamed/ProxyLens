import React, { useState } from 'react';
import { QueryApiClient } from '../../api/client';
import { AccountedTrafficRecord, ConnectionRecord } from '../../api/types';
import { useAuditContext, useLocale } from '../../state/AuditContext';
import { useConnectionDetailQuery, useConnectionTrafficQuery } from '../../api/queries';
import {
  CopyButton,
  EvidenceChip,
  ErrorState,
  KeyValue,
  RouteBadge,
  Section,
  SkeletonRows,
} from '../../components/ui/primitives';
import { IconClose } from '../../components/ui/icons';
import { formatBytes } from '../../utils/format';
import { formatLocalDateTime, formatLocalDateTimeCompact } from '../../utils/time';
import { qualityFlagLabels } from '../../utils/qualityFlags';
import { isNoAccountingRunError } from '../common/PageGate';

const accountingClassLabel = (cls: string | undefined, t: (key: string) => string): { label: string; kind: 'neutral' | 'estimated' | 'ambiguous' | 'missing' } => {
  switch (cls) {
    case 'known_application':
      return { label: t('inspector.knownApplication'), kind: 'neutral' };
    case 'direct_application':
      return { label: t('inspector.directApplication'), kind: 'neutral' };
    case 'system_proxy_inbound':
      return { label: t('inspector.systemProxyInbound'), kind: 'neutral' };
    case 'ambiguous_relay_candidate':
      return { label: t('inspector.ambiguousRelayCandidate'), kind: 'ambiguous' };
    case 'unpaired_missing_attribution':
      return { label: t('inspector.unpairedMissingAttribution'), kind: 'missing' };
    default:
      return { label: cls || t('inspector.unknownClass'), kind: 'neutral' };
  }
};

const CausalPath: React.FC<{ c: ConnectionRecord; topGroup?: string }> = ({ c, topGroup }) => {
  const { t } = useLocale();
  const chains = c.chains ?? [];
  const finalEgress = chains[0];
  const top = topGroup ?? (chains.length > 1 ? chains[chains.length - 1] : undefined);
  const intermediates = chains.length > 2 ? chains.slice(1, chains.length - 1) : [];
  const dest = c.metadata?.host || c.metadata?.sniffHost;
  const ip = c.metadata?.destinationIP;
  const port = c.metadata?.destinationPort;

  const steps: { key: string; value: React.ReactNode; mono?: boolean; accent?: boolean }[] = [
    {
      key: t('inspector.process'),
      value: c.metadata?.process ? (
        <>
          {c.metadata.process}
          {c.metadata.processPath && (
            <span className="pl-cell-sub pl-mono" title={c.metadata.processPath}>{c.metadata.processPath}</span>
          )}
        </>
      ) : (
        <EvidenceChip kind="missing" label={t('inspector.missingProcess')} title={t('inspector.missingProcessTitle')} />
      ),
    },
    {
      key: t('inspector.destination'),
      value: dest ? (
        <>
          {dest}
          {ip && <span className="pl-cell-sub pl-mono">{ip}{port ? `:${port}` : ''}</span>}
        </>
      ) : ip ? (
        <>
          <EvidenceChip kind="missing" label={t('inspector.ipOnly')} title={t('inspector.ipOnlyTitle')} />{' '}
          {ip}{port ? `:${port}` : ''}
        </>
      ) : (
        <EvidenceChip kind="missing" label={t('inspector.missingDestination')} />
      ),
      mono: !dest,
    },
    {
      key: t('inspector.rule'),
      value: c.rule ? (
        <span className="pl-mono">
          {c.rule}
          {c.rulePayload ? <span className="pl-secondary"> · {c.rulePayload}</span> : null}
        </span>
      ) : (
        <EvidenceChip kind="missing" label={t('inspector.missingRule')} title={t('inspector.missingRuleTitle')} />
      ),
    },
  ];

  if (top) steps.push({ key: t('inspector.topPolicy'), value: <span className="pl-mono">{top}</span> });
  if (intermediates.length > 0) {
    steps.push({ key: t('inspector.proxyChain'), value: <span className="pl-mono">{intermediates.join(' → ')}</span> });
  }
  steps.push({
    key: t('inspector.egress'),
    value: <span className="pl-mono">{finalEgress ?? (c.route === 'DIRECT' ? 'DIRECT' : '(unknown)')}</span>,
    accent: (c.route || '').toUpperCase() === 'PROXY',
  });

  return (
    <ol className="pl-causal">
      {steps.map((s) => (
        <li key={s.key} className={`pl-causal__step${s.accent ? ' pl-causal__step--accent' : ''}`}>
          <span className="pl-causal__key">{s.key}</span>
          <span className={`pl-causal__value${s.mono ? ' pl-causal__value--mono' : ''}`}>{s.value}</span>
        </li>
      ))}
    </ol>
  );
};

const EventTimeline: React.FC<{ events: AccountedTrafficRecord[] }> = ({ events }) => {
  const { locale, t } = useLocale();
  if (events.length === 0) {
    return <div className="pl-muted pl-small">{t('inspector.noAccountingEvents')}</div>;
  }
  let prev: AccountedTrafficRecord | null = null;
  return (
    <div>
      {events.map((ev, idx) => {
        const changed =
          prev !== null &&
          (prev.process !== ev.process ||
            prev.host !== ev.host ||
            prev.rule !== ev.rule ||
            prev.rulePayload !== ev.rulePayload ||
            prev.finalProxy !== ev.finalProxy ||
            prev.route !== ev.route);
        prev = ev;
        return (
          <div key={`${ev.sourceEventId}-${idx}`} className={`pl-event-row${changed ? ' pl-event-row--changed' : ''}`}>
            <span className="pl-event-row__time">{formatLocalDateTimeCompact(ev.observedAt, locale)}</span>
            <span className="pl-event-row__body">
              <span>{ev.process ?? '-'}</span>
              <span>→ {ev.host ?? ev.destinationIp ?? '-'}</span>
              {ev.rule && <span>· {ev.rule}{ev.rulePayload ? ` ${ev.rulePayload}` : ''}</span>}
              {ev.finalProxy && <span>· {ev.finalProxy}</span>}
              <span>· ↑{formatBytes(ev.accountedUpload)} ↓{formatBytes(ev.accountedDownload)}</span>
              {ev.precision === 'interval_derived' && (
                <EvidenceChip kind="estimated" label={t('common.interval')} title={t('inspector.intervalTitle')} />
              )}
              {changed && (
                <EvidenceChip kind="ambiguous" label={t('inspector.accountingEvent')} title={t('inspector.accountingEventTitle')} />
              )}
            </span>
          </div>
        );
      })}
    </div>
  );
};

export const ConnectionInspector: React.FC<{ client: QueryApiClient | null }> = ({ client }) => {
  const { locale, t } = useLocale();
  const { selected, setSelected } = useAuditContext();
  const [showRawFrames, setShowRawFrames] = useState(false);

  const detailQ = useConnectionDetailQuery(client, selected);
  const trafficQ = useConnectionTrafficQuery(client, selected, showRawFrames && !!selected);

  if (!selected) {
    return (
      <aside className="pl-inspector" aria-label={t('inspector.aria')}>
        <div className="pl-inspector__placeholder">
          <svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5">
            <circle cx="10.5" cy="10.5" r="6" />
            <path d="M15 15l5.5 5.5" />
          </svg>
          <span>{t('inspector.placeholder')}</span>
        </div>
      </aside>
    );
  }

  const detail = detailQ.data;
  const c = detail?.connection;
  const summary = detail?.accountingSummary;
  const events = detail?.accountingEvents ?? [];
  const cls = accountingClassLabel(summary?.accountingClass ?? c?.latestAttributionClass, t);
  const hasInterval = events.some((e) => e.precision === 'interval_derived');
  const identity = `${selected.sessionId} · epoch ${selected.epochId} · ${selected.connectionId}`;

  return (
    <aside className="pl-inspector" aria-label={t('inspector.aria')}>
      <div className="pl-inspector__header">
        <div className="pl-inspector__title-row">
          <span className="pl-inspector__title">
            {c?.metadata?.process ?? t('inspector.connection')}
            {c && (c.metadata?.host || c.metadata?.sniffHost || c.metadata?.destinationIP)
              ? ` → ${c.metadata.host || c.metadata.sniffHost || c.metadata.destinationIP}`
              : ''}
          </span>
          {c && <RouteBadge route={c.route} />}
          <button className="pl-icon-btn" aria-label={t('inspector.close')} title={t('inspector.closeTitle')} onClick={() => setSelected(null)}>
            <IconClose />
          </button>
        </div>
        <div className="pl-inspector__id-row">
          <span className="pl-inspector__id" title={identity}>{identity}</span>
          <CopyButton text={identity} title={t('inspector.copyIdentity')} />
        </div>
      </div>

      <div className="pl-inspector__body">
        {detailQ.isLoading ? (
          <SkeletonRows rows={10} />
        ) : detailQ.isError ? (
          isNoAccountingRunError(detailQ.error) ? (
            <Section title={t('inspector.causalPath')}>
              {c ? <CausalPath c={c} /> : null}
              <div className="pl-muted pl-small" style={{ marginTop: 8 }}>
                {t('inspector.noCompletedRun')}
              </div>
            </Section>
          ) : (
            <ErrorState
              title={t('inspector.detailUnavailable')}
              body={String((detailQ.error as Error)?.message ?? detailQ.error)}
            />
          )
        ) : c ? (
          <>
            <Section title={t('inspector.causalPath')} sub={t('inspector.causalPathSub')}>
              <CausalPath c={c} topGroup={summary?.latestTopPolicyGroup} />
            </Section>

            <Section title={t('inspector.trafficAccounting')} sub={hasInterval ? t('inspector.intervalSub') : t('inspector.exactSub')}>
              {summary ? (
                <>
                  <KeyValue
                    items={[
                      { key: t('common.upload'), value: t('inspector.uploadRaw', { accounted: formatBytes(summary.accountedUploadTotal), raw: formatBytes(summary.rawUploadTotal) }), mono: true },
                      { key: t('common.download'), value: t('inspector.downloadRaw', { accounted: formatBytes(summary.accountedDownloadTotal), raw: formatBytes(summary.rawDownloadTotal) }), mono: true },
                      { key: t('inspector.total'), value: formatBytes(summary.accountedUploadTotal + summary.accountedDownloadTotal), mono: true },
                      { key: t('inspector.route'), value: <RouteBadge route={summary.route} quiet /> },
                      { key: t('inspector.class'), value: <EvidenceChip kind={cls.kind} label={cls.label} /> },
                    ]}
                  />
                  {summary.accountedUploadTotal !== summary.rawUploadTotal || summary.accountedDownloadTotal !== summary.rawDownloadTotal ? (
                    <div className="pl-muted pl-small" style={{ marginTop: 6 }}>
                      {t('inspector.reconciliationNote')}
                    </div>
                  ) : null}
                </>
              ) : (
                <KeyValue
                  items={[
                    { key: t('common.upload'), value: formatBytes(c.monitoredUploadTotal), mono: true },
                    { key: t('common.download'), value: formatBytes(c.monitoredDownloadTotal), mono: true },
                    { key: t('inspector.total'), value: formatBytes(c.monitoredUploadTotal + c.monitoredDownloadTotal), mono: true },
                  ]}
                />
              )}
              <div className="pl-muted pl-small" style={{ marginTop: 6 }} title={t('inspector.unroundedTitle')}>
                {t('inspector.exactIntegers', { upload: summary ? summary.accountedUploadTotal : c.monitoredUploadTotal, download: summary ? summary.accountedDownloadTotal : c.monitoredDownloadTotal })}
              </div>
            </Section>

            <Section title={t('inspector.evidenceQuality')}>
              <div style={{ display: 'flex', flexWrap: 'wrap', gap: 6 }}>
                <EvidenceChip kind={cls.kind} label={cls.label} title={t('inspector.attributionTitle', { value: summary?.accountingClass ?? c.latestAttributionClass ?? t('common.notAvailable') })} />
                {c.preexistingAtStart && (
                  <EvidenceChip kind="estimated" label={t('inspector.preexisting')} title={t('inspector.preexistingTitle')} />
                )}
                {c.possibleUnobservedTail && (
                  <EvidenceChip kind="estimated" label={t('inspector.unobservedTail')} title={t('inspector.unobservedTailTitle')} />
                )}
                {qualityFlagLabels(c.qualityFlags).map((f) => (
                  <EvidenceChip key={f} kind="neutral" label={f} />
                ))}
                {!c.metadata?.process && <EvidenceChip kind="missing" label={t('inspector.missingProcess')} />}
                {!c.metadata?.host && !c.metadata?.sniffHost && <EvidenceChip kind="missing" label={t('inspector.missingHost')} />}
                {!c.rule && <EvidenceChip kind="missing" label={t('inspector.missingRule')} />}
                {(!c.chains || c.chains.length === 0) && <EvidenceChip kind="missing" label={t('inspector.missingChain')} />}
              </div>
              {c.relayEvidence && Object.keys(c.relayEvidence).length > 0 && (
                <details className="pl-disclosure" style={{ marginTop: 8 }}>
                  <summary>{t('inspector.relayEvidence')}</summary>
                  <pre className="pl-mono pl-small" style={{ whiteSpace: 'pre-wrap', overflowWrap: 'anywhere', color: 'var(--pl-text-secondary)' }}>
                    {JSON.stringify(c.relayEvidence, null, 2)}
                  </pre>
                </details>
              )}
            </Section>

            <Section title={t('inspector.lifecycle')}>
              <KeyValue
                items={[
                  { key: t('inspector.firstObserved'), value: formatLocalDateTime(c.firstObservedAt, locale), mono: true },
                  { key: t('inspector.lastObserved'), value: formatLocalDateTime(c.lastObservedAt, locale), mono: true },
                  { key: t('inspector.state'), value: c.observationActive ? t('inspector.activeState') : t('inspector.endedState', { state: c.state }) },
                  { key: t('inspector.endReason'), value: c.observationEndReason ?? '—' },
                  { key: t('inspector.session'), value: c.sessionId, mono: true },
                  { key: t('inspector.epoch'), value: String(c.epochId), mono: true },
                  { key: t('inspector.connectionId'), value: c.connectionId, mono: true },
                ]}
              />
            </Section>

            <Section title={t('inspector.accountingEventsTitle')} sub={t('inspector.accountingEvents', { count: events.length })}>
              <EventTimeline events={events} />
            </Section>

            <Section title={t('inspector.rawTrafficFrames')} sub={t('inspector.rawTrafficFramesSub')}>
              <details className="pl-disclosure" open={showRawFrames} onToggle={(e) => setShowRawFrames((e.target as HTMLDetailsElement).open)}>
                <summary>{showRawFrames ? t('inspector.hideRawFrames') : t('inspector.showRawFrames')}</summary>
                {showRawFrames && (
                  <div style={{ marginTop: 8 }}>
                    {trafficQ.isLoading ? (
                      <SkeletonRows rows={4} />
                    ) : trafficQ.isError ? (
                      <div className="pl-muted pl-small">{t('inspector.rawUnavailable', { message: String((trafficQ.error as Error)?.message ?? trafficQ.error) })}</div>
                    ) : (trafficQ.data?.traffic ?? []).length === 0 ? (
                      <div className="pl-muted pl-small">{t('inspector.noRawFrames')}</div>
                    ) : (
                      <div>
                        <div className="pl-traffic-frame-row" style={{ color: 'var(--pl-text-muted)', borderBottom: '1px solid var(--pl-border)' }}>
                          <span>{t('history.time')}</span>
                          <span>{t('inspector.precision')}</span>
                          <span style={{ textAlign: 'right' }}>{t('inspector.delta')}</span>
                          <span style={{ textAlign: 'right' }}>{t('inspector.counters')}</span>
                        </div>
                        {(trafficQ.data?.traffic ?? []).map((f) => (
                          <div key={f.eventId} className="pl-traffic-frame-row">
                            <span title={f.observedAt}>{formatLocalDateTimeCompact(f.observedAt, locale)}</span>
                            <span>{f.precision === 'exact' ? t('common.exact') : <EvidenceChip kind="estimated" label={t('common.interval')} />}</span>
                            <span style={{ textAlign: 'right' }}>
                              {formatBytes(f.deltaUpload)} / {formatBytes(f.deltaDownload)}
                            </span>
                            <span style={{ textAlign: 'right' }}>
                              {formatBytes(f.observedUploadCounter)} / {formatBytes(f.observedDownloadCounter)}
                            </span>
                          </div>
                        ))}
                      </div>
                    )}
                  </div>
                )}
              </details>
            </Section>
          </>
        ) : (
          <div className="pl-muted pl-small" style={{ padding: '12px 0' }}>
            {t('inspector.noPayload', { identity })}
          </div>
        )}
      </div>
    </aside>
  );
};
