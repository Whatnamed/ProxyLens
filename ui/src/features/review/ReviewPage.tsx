import React from 'react';
import { QueryApiClient } from '../../api/client';
import { AuditFinding, AuditFindingKind, HostRouteChangeFinding, MetaResponse, ProcessChangeFinding, TemporalFindingsResult } from '../../api/types';
import { useAuditContext, useLocale } from '../../state/AuditContext';
import { useAuditFindingsQuery, useTemporalFindingsQuery } from '../../api/queries';
import { PageGate, errorCodeOf, isNoAccountingRunError } from '../common/PageGate';
import {
  EmptyState,
  ErrorState,
  EvidenceChip,
  RouteBadge,
  Section,
  SkeletonRows,
  StatusIndicator,
} from '../../components/ui/primitives';
import { TimeRangeControl } from '../../components/audit/AuditContextBar';
import { formatBytes } from '../../utils/format';
import { deriveTemporalComparisonRange, TemporalComparisonRange, formatLocalDateTime } from '../../utils/time';
import { findingIsEstimated, findingKindKey, findingTargetLabel, historyFiltersForFinding } from './reviewSemantics';

const SECTIONS: { kind: AuditFindingKind; titleKey: string; subKey: string }[] = [
  { kind: 'match_fallback_proxy', titleKey: 'review.sectionMatch', subKey: 'review.sectionMatchSub' },
  { kind: 'broad_udp_proxy', titleKey: 'review.sectionUdp', subKey: 'review.sectionUdpSub' },
  { kind: 'ip_only_proxy_target', titleKey: 'review.sectionIp', subKey: 'review.sectionIpSub' },
  { kind: 'large_proxy_connection', titleKey: 'review.sectionLarge', subKey: 'review.sectionLargeSub' },
];

function processPeriodHasEstimatedBytes(period: ProcessChangeFinding['recent']): boolean {
  return [period.proxy, period.direct, period.reject].some(
    (route) => route.estimatedUploadBytes > 0 || route.estimatedDownloadBytes > 0,
  );
}

function comparisonWindowLabel(result: Pick<TemporalFindingsResult['baseline'], 'from' | 'to'>, locale: 'en' | 'zh-CN'): string {
  return `${formatLocalDateTime(result.from, locale)} – ${formatLocalDateTime(result.to, locale)}`;
}

const TemporalFindingRow: React.FC<{
  finding: ProcessChangeFinding;
  onInvestigate: (finding: ProcessChangeFinding) => void;
}> = ({ finding, onInvestigate }) => {
  const { t } = useLocale();
  const estimated = processPeriodHasEstimatedBytes(finding.baseline) || processPeriodHasEstimatedBytes(finding.recent);
  const isGrowth = finding.kind === 'process_proxy_growth';
  return (
    <article className="pl-review__finding pl-review__temporal-finding">
      <div className="pl-review__finding-main">
        <div className="pl-review__finding-heading">
          <span className="pl-review__kind">{t(isGrowth ? 'review.temporalGrowthKind' : 'review.temporalNewKind')}</span>
          <span className="pl-review__target pl-mono" title={finding.process}>{finding.process}</span>
          <EvidenceChip
            kind={estimated ? 'estimated' : 'neutral'}
            label={estimated ? t('common.estimated') : t('common.exact')}
            title={estimated ? t('review.temporalEstimated') : t('review.temporalExact')}
          />
        </div>
        <dl className="pl-review__facts pl-review__temporal-facts">
          <div>
            <dt>{t('review.process')}</dt>
            <dd className="pl-mono">{finding.process}</dd>
          </div>
          {isGrowth ? (
            <>
              <div>
                <dt>{t('review.temporalBaselineRate')}</dt>
                <dd className="pl-mono">{formatBytes(finding.baselineProxyBytesPerHour)} /h</dd>
              </div>
              <div>
                <dt>{t('review.temporalRecentRate')}</dt>
                <dd className="pl-mono">{formatBytes(finding.recentProxyBytesPerHour)} /h</dd>
              </div>
              <div>
                <dt>{t('review.temporalDeltaRate')}</dt>
                <dd className="pl-mono">+{formatBytes(finding.deltaBytesPerHour)} /h</dd>
              </div>
              <div>
                <dt>{t('review.temporalBaselineBytes')}</dt>
                <dd className="pl-mono">{formatBytes(finding.baseline.proxy.totalBytes)}</dd>
              </div>
              <div>
                <dt>{t('review.temporalRecentBytes')}</dt>
                <dd className="pl-mono">{formatBytes(finding.recent.proxy.totalBytes)}</dd>
              </div>
            </>
          ) : (
            <>
              <div>
                <dt>{t('review.temporalBaselineProxy')}</dt>
                <dd className="pl-mono">{formatBytes(finding.baseline.proxy.totalBytes)}</dd>
              </div>
              <div>
                <dt>{t('review.temporalRecentProxy')}</dt>
                <dd className="pl-mono">{formatBytes(finding.recent.proxy.totalBytes)}</dd>
              </div>
              {finding.baseline.direct.totalBytes > 0 && (
                <div>
                  <dt>{t('review.temporalBaselineDirect')}</dt>
                  <dd className="pl-mono">{formatBytes(finding.baseline.direct.totalBytes)}</dd>
                </div>
              )}
              {finding.baseline.reject.totalBytes > 0 && (
                <div>
                  <dt>{t('review.temporalBaselineReject')}</dt>
                  <dd className="pl-mono">{formatBytes(finding.baseline.reject.totalBytes)}</dd>
                </div>
              )}
            </>
          )}
        </dl>
        <div className="pl-review__why">
          <span className="pl-review__why-label">{t('review.why')}</span>
          <span>{t(isGrowth ? 'review.temporalGrowthFact' : 'review.temporalNewFact')}</span>
          <span>{t('review.temporalProcessFact', { process: finding.process })}</span>
        </div>
      </div>
      <button
        type="button"
        className="pl-btn pl-btn--quiet pl-btn--compact pl-review__investigate"
        onClick={() => onInvestigate(finding)}
        aria-label={t('review.temporalInvestigateAria', { process: finding.process })}
      >
        {t('review.investigate')}
      </button>
    </article>
  );
};

const TemporalReviewSection: React.FC<{
  comparison: TemporalComparisonRange;
  query: ReturnType<typeof useTemporalFindingsQuery>;
  locale: 'en' | 'zh-CN';
  onInvestigate: (finding: ProcessChangeFinding) => void;
  onHostInvestigate: (finding: HostRouteChangeFinding) => void;
}> = ({ comparison, query, locale, onInvestigate, onHostInvestigate }) => {
  const { t } = useLocale();
  const result = query.data;
  const unavailable = result && result.status !== 'ready';
  return (
    <Section
      title={t('review.comparisonTitle')}
      sub={t('review.comparisonSubtitle')}
      right={result ? <StatusIndicator kind={unavailable ? 'gap' : 'neutral'} label={unavailable ? t('review.comparisonUnavailable') : t('review.comparisonReady')} /> : undefined}
    >
      <div className="pl-review__comparison-windows">
        <span><strong>{t('review.comparisonBaseline')}</strong> <span className="pl-mono">{comparisonWindowLabel(result?.baseline ?? { from: comparison.baselineEffective?.from ?? comparison.baselineRequested.from, to: comparison.baselineEffective?.to ?? comparison.baselineRequested.to }, locale)}</span></span>
        <span><strong>{t('review.comparisonRecent')}</strong> <span className="pl-mono">{comparisonWindowLabel(result?.recent ?? { from: comparison.recentEffective?.from ?? comparison.recentRequested.from, to: comparison.recentEffective?.to ?? comparison.recentRequested.to }, locale)}</span></span>
      </div>
      {!comparison.baselineEffective || !comparison.recentEffective ? (
        <div className="pl-review__empty">{t('review.comparisonInsufficientHours')}</div>
      ) : query.isLoading ? (
        <SkeletonRows rows={4} />
      ) : query.isError ? (
        <div className="pl-review__empty">{t('review.comparisonQueryFailed')}</div>
      ) : unavailable ? (
        <div className="pl-review__comparison-unavailable">
          <strong>{t('review.comparisonUnavailable')}</strong>
          <span>{t(`review.comparisonStatus.${result.status}`)}</span>
        </div>
      ) : result ? (
        <>
          <Section title={t('review.temporalNewSection')} sub={t('review.temporalNewSectionSub')} right={<span className="pl-mono pl-small">{result.countsByKind.process_newly_observed_on_proxy ?? 0}</span>}>
            {result.processItems.filter((item) => item.kind === 'process_newly_observed_on_proxy').length === 0 ? (
              <div className="pl-review__empty">{t('review.temporalNoChanges')}</div>
            ) : (
              <div className="pl-review__list">
                {result.processItems.filter((item) => item.kind === 'process_newly_observed_on_proxy').map((finding) => (
                  <TemporalFindingRow key={finding.id} finding={finding} onInvestigate={onInvestigate} />
                ))}
              </div>
            )}
          </Section>
          <Section title={t('review.temporalGrowthSection')} sub={t('review.temporalGrowthSectionSub')} right={<span className="pl-mono pl-small">{result.countsByKind.process_proxy_growth ?? 0}</span>}>
            {result.processItems.filter((item) => item.kind === 'process_proxy_growth').length === 0 ? (
              <div className="pl-review__empty">{t('review.temporalNoChanges')}</div>
            ) : (
              <div className="pl-review__list">
                {result.processItems.filter((item) => item.kind === 'process_proxy_growth').map((finding) => (
                  <TemporalFindingRow key={finding.id} finding={finding} onInvestigate={onInvestigate} />
                ))}
              </div>
            )}
          </Section>
          <Section title={t('review.temporalHostSection')} sub={t('review.temporalHostSectionSub')} right={<span className="pl-mono pl-small">{result.countsByKind.host_gained_proxy_after_direct_baseline ?? 0}</span>}>
            {result.hostItems.length === 0 ? (
              <div className="pl-review__empty">{t('review.temporalNoHostChanges')}</div>
            ) : (
              <div className="pl-review__list">
                {result.hostItems.map((finding) => (
                  <HostTemporalFindingRow key={finding.id} finding={finding} onInvestigate={onHostInvestigate} />
                ))}
              </div>
            )}
          </Section>
        </>
      ) : null}
    </Section>
  );
};

const HostTemporalFindingRow: React.FC<{
  finding: HostRouteChangeFinding;
  onInvestigate: (finding: HostRouteChangeFinding) => void;
}> = ({ finding, onInvestigate }) => {
  const { t } = useLocale();
  const estimated = processPeriodHasEstimatedBytes(finding.baseline) || processPeriodHasEstimatedBytes(finding.recent);
  const mixedRecent = finding.recent.direct.totalBytes > 0;
  return (
    <article className="pl-review__finding pl-review__temporal-finding">
      <div className="pl-review__finding-main">
        <div className="pl-review__finding-heading">
          <span className="pl-review__kind">{t('review.temporalHostKind')}</span>
          <span className="pl-review__target pl-mono" title={finding.host}>{finding.host}</span>
          <EvidenceChip
            kind={estimated ? 'estimated' : 'neutral'}
            label={estimated ? t('common.estimated') : t('common.exact')}
            title={estimated ? t('review.temporalEstimated') : t('review.temporalExact')}
          />
        </div>
        <dl className="pl-review__facts pl-review__temporal-facts">
          <div>
            <dt>{t('review.temporalRecordedHost')}</dt>
            <dd className="pl-mono">{finding.host}</dd>
          </div>
          <div>
            <dt>{t('review.temporalBaselineDirect')}</dt>
            <dd className="pl-mono">{formatBytes(finding.baseline.direct.totalBytes)}</dd>
          </div>
          <div>
            <dt>{t('review.temporalBaselineProxy')}</dt>
            <dd className="pl-mono">{formatBytes(finding.baseline.proxy.totalBytes)}</dd>
          </div>
          <div>
            <dt>{t('review.temporalRecentProxy')}</dt>
            <dd className="pl-mono">{formatBytes(finding.recent.proxy.totalBytes)}</dd>
          </div>
          {mixedRecent && (
            <div>
              <dt>{t('review.temporalRecentDirect')}</dt>
              <dd className="pl-mono">{formatBytes(finding.recent.direct.totalBytes)}</dd>
            </div>
          )}
          {finding.recent.reject.totalBytes > 0 && (
            <div>
              <dt>{t('review.temporalRecentReject')}</dt>
              <dd className="pl-mono">{formatBytes(finding.recent.reject.totalBytes)}</dd>
            </div>
          )}
        </dl>
        <div className="pl-review__why">
          <span className="pl-review__why-label">{t('review.why')}</span>
          <span>{t('review.temporalHostFact')}</span>
          {mixedRecent && <span>{t('review.temporalHostMixedFact')}</span>}
        </div>
      </div>
      <button
        type="button"
        className="pl-btn pl-btn--quiet pl-btn--compact pl-review__investigate"
        onClick={() => onInvestigate(finding)}
        aria-label={t('review.temporalHostInvestigateAria', { host: finding.host })}
      >
        {t('review.investigate')}
      </button>
    </article>
  );
};

const BackgroundCatalogFindingRow: React.FC<{ finding: AuditFinding; onInvestigate: (finding: AuditFinding) => void }> = ({
  finding,
  onInvestigate,
}) => {
  const { t } = useLocale();
  const subject = finding.subject;
  const evidence = finding.evidence;
  const knowledge = finding.knowledge;
  const target = findingTargetLabel(finding) || t('review.targetMissing');
  const estimated = findingIsEstimated(finding);
  const sourcePublisher = knowledge?.sources.map((source) => source.publisher).filter(Boolean).join(', ') || t('common.notAvailable');
  return (
    <article className="pl-review__finding pl-review__catalog-finding">
      <div className="pl-review__finding-main">
        <div className="pl-review__finding-heading">
          <span className="pl-review__kind">{t('review.kindCatalog')}</span>
          <span className="pl-review__target" title={knowledge?.family}>{knowledge?.family || t('common.notAvailable')}</span>
          <EvidenceChip
            kind={estimated ? 'estimated' : 'neutral'}
            label={estimated ? t('common.estimated') : t('common.exact')}
            title={estimated ? t('review.intervalEvidence') : t('review.exactEvidence')}
          />
        </div>
        <dl className="pl-review__facts pl-review__catalog-facts">
          <div>
            <dt>{t('review.process')}</dt>
            <dd className="pl-mono" title={subject.process}>{subject.process || t('common.unknown')}</dd>
          </div>
          <div>
            <dt>{t('review.catalogPath')}</dt>
            <dd className="pl-mono" title={subject.processPath}>{subject.processPath || t('common.notAvailable')}</dd>
          </div>
          <div>
            <dt>{t('review.target')}</dt>
            <dd className="pl-mono" title={target}>{target}</dd>
          </div>
          <div>
            <dt>{t('review.catalogBytes')}</dt>
            <dd className="pl-mono">{formatBytes(evidence.totalBytes)}</dd>
          </div>
          <div>
            <dt>{t('review.connections')}</dt>
            <dd className="pl-mono">{evidence.connectionCount}</dd>
          </div>
          <div>
            <dt>{t('review.catalogSource')}</dt>
            <dd title={sourcePublisher}>{sourcePublisher}</dd>
          </div>
        </dl>
        <div className="pl-review__why">
          <span className="pl-review__why-label">{t('review.why')}</span>
          <span>{t('review.catalogMatchBasis')}</span>
          <span>{t('review.catalogVerificationBoundary')}</span>
          <span>{t('review.routeFact', { route: evidence.route })}</span>
          <span>{t('review.accountedFact', { bytes: formatBytes(evidence.totalBytes) })}</span>
        </div>
      </div>
      <button
        type="button"
        className="pl-btn pl-btn--quiet pl-btn--compact pl-review__investigate"
        onClick={() => onInvestigate(finding)}
        aria-label={t('review.catalogInvestigateAria', { process: subject.process || t('common.unknown') })}
      >
        {t('review.investigate')}
      </button>
    </article>
  );
};

const FindingRow: React.FC<{ finding: AuditFinding; onInvestigate: (finding: AuditFinding) => void }> = ({
  finding,
  onInvestigate,
}) => {
  const { t } = useLocale();
  const target = findingTargetLabel(finding) || t('review.targetMissing');
  const estimated = findingIsEstimated(finding);
  const subject = finding.subject;
  const evidence = finding.evidence;
  const precisionTitle = estimated ? t('review.intervalEvidence') : t('review.exactEvidence');
  const showRule = finding.kind === 'match_fallback_proxy' || finding.kind === 'broad_udp_proxy';
  const showNetwork = finding.kind === 'broad_udp_proxy';
  const targetFact = subject.targetKind === 'destination_ip'
    ? t('review.targetIpFact')
    : subject.targetKind === 'sniff_host'
      ? t('review.targetSniffHostFact')
      : subject.targetKind === 'host'
        ? t('review.targetHostFact')
        : t('review.targetMissingFact');

  return (
    <article className="pl-review__finding">
      <div className="pl-review__finding-main">
        <div className="pl-review__finding-heading">
          <span className="pl-review__kind">{t(findingKindKey(finding.kind))}</span>
          <span className="pl-review__target pl-mono" title={target}>{target}</span>
          <EvidenceChip
            kind={estimated ? 'estimated' : 'neutral'}
            label={estimated ? t('common.estimated') : t('common.exact')}
            title={precisionTitle}
          />
        </div>
        <dl className="pl-review__facts">
          <div>
            <dt>{t('review.process')}</dt>
            <dd className="pl-mono">{subject.process || t('common.unknown')}</dd>
          </div>
          <div>
            <dt>{t('review.target')}</dt>
            <dd className="pl-mono">{target}</dd>
          </div>
          {showRule && (
            <div>
              <dt>{t('review.rule')}</dt>
              <dd className="pl-mono">{evidence.rule || t('common.notAvailable')}</dd>
            </div>
          )}
          {showNetwork && (
            <div>
              <dt>{t('review.network')}</dt>
              <dd className="pl-mono">{evidence.network || t('common.notAvailable')}</dd>
            </div>
          )}
          <div>
            <dt>{t('review.bytes')}</dt>
            <dd className="pl-mono">{formatBytes(evidence.totalBytes)}</dd>
          </div>
          <div>
            <dt>{t('review.connections')}</dt>
            <dd className="pl-mono">{evidence.connectionCount}</dd>
          </div>
          {evidence.thresholdBytes !== undefined && (
            <div>
              <dt>{t('review.threshold')}</dt>
              <dd className="pl-mono">{formatBytes(evidence.thresholdBytes)}</dd>
            </div>
          )}
        </dl>
        <div className="pl-review__why">
          <span className="pl-review__why-label">{t('review.why')}</span>
          <span>{t('review.routeFact', { route: evidence.route })}</span>
          {finding.kind === 'match_fallback_proxy' && evidence.rule && <span>{t('review.ruleFact', { rule: evidence.rule })}</span>}
          {finding.kind === 'broad_udp_proxy' && evidence.rule && <span>{t('review.ruleFact', { rule: evidence.rule })}</span>}
          {finding.kind === 'broad_udp_proxy' && evidence.network && <span>{t('review.networkFact', { network: evidence.network })}</span>}
          <span>{targetFact}</span>
          {finding.kind === 'large_proxy_connection' && <span>{t('review.physicalConnectionFact')}</span>}
          <span>{t('review.accountedFact', { bytes: formatBytes(evidence.totalBytes) })}</span>
          {evidence.thresholdBytes !== undefined && <span>{t('review.thresholdFact', { threshold: formatBytes(evidence.thresholdBytes) })}</span>}
        </div>
      </div>
      <button
        type="button"
        className="pl-btn pl-btn--quiet pl-btn--compact pl-review__investigate"
        onClick={() => onInvestigate(finding)}
        aria-label={t('review.investigateAria', { target })}
      >
        {t('review.investigate')}
      </button>
    </article>
  );
};

export const ReviewPage: React.FC<{
  client: QueryApiClient | null;
  sessionError: string | null;
  meta: MetaResponse | undefined;
}> = ({ client, sessionError, meta }) => {
  const { t } = useLocale();
  const { resolvedRange, rangeSourceKey, timeRange, investigateFinding } = useAuditContext();
  const { locale } = useLocale();
  const comparison = React.useMemo(
    () => deriveTemporalComparisonRange(timeRange.kind, resolvedRange),
    [timeRange.kind, resolvedRange],
  );
  const findingsQ = useAuditFindingsQuery(client, resolvedRange.from, resolvedRange.to, rangeSourceKey, 20);
  const temporalFindingsQ = useTemporalFindingsQuery(client, comparison, rangeSourceKey, 20);
  const findings = findingsQ.data;
  const noRun = isNoAccountingRunError(findingsQ.error);

  return (
    <PageGate client={client} sessionError={sessionError} meta={meta}>
      <div className="pl-page" data-pl-page="review">
        <div className="pl-page__header">
          <div>
            <h1 className="pl-page__title">{t('review.title')}</h1>
            <div className="pl-page__subtitle">{t('review.subtitle')}</div>
          </div>
          <div className="pl-page__header-right">
            {meta?.freshness && (
              <StatusIndicator
                kind={meta.freshness.isFresh ? 'fresh' : 'stale'}
                label={meta.freshness.isFresh ? t('review.accountingFresh') : t('review.accountingStale')}
                title={t('review.accountingVersion', { version: findings?.accountingVersion ?? meta.latestAccountingRun?.algorithmVersion ?? t('common.notAvailable') })}
              />
            )}
          </div>
        </div>

        <div className="pl-context-bar">
          <TimeRangeControl />
          <div className="pl-context-bar__sep" />
          <span className="pl-review__scope" aria-label={t('review.scopeAria')}>
            <RouteBadge route="PROXY" />
            <span>{t('review.scopeProxy')}</span>
          </span>
        </div>

        <div className="pl-page__scroll">
          <div className="pl-review">
            <div className="pl-review__intro">{t('review.description')}</div>
            <TemporalReviewSection
              comparison={comparison}
              query={temporalFindingsQ}
              locale={locale}
              onInvestigate={(finding) => investigateFinding({ process: finding.process })}
              onHostInvestigate={(finding) => investigateFinding({ host: finding.host })}
            />
            {findingsQ.isLoading ? (
              <SkeletonRows rows={10} />
            ) : findingsQ.isError && !noRun ? (
              <ErrorState title={t('review.queryFailed')} body={String((findingsQ.error as Error)?.message ?? findingsQ.error)} code={errorCodeOf(findingsQ.error)} />
            ) : noRun ? (
              <EmptyState title={t('review.noAccountingTitle')} body={t('review.noAccountingBody')} />
            ) : findings ? (
              <>
                <Section
                  title={t('review.catalogSection')}
                  sub={t('review.catalogSectionSub')}
                  right={<span className="pl-mono pl-small">{findings.countsByKind.cataloged_background_process_proxy ?? 0}</span>}
                >
                  {findings.items.filter((item) => item.kind === 'cataloged_background_process_proxy').length === 0 ? (
                    <div className="pl-review__empty">{t('review.catalogNoMatches')}</div>
                  ) : (
                    <div className="pl-review__list">
                      {findings.items
                        .filter((item) => item.kind === 'cataloged_background_process_proxy')
                        .map((finding) => (
                          <BackgroundCatalogFindingRow key={finding.id} finding={finding} onInvestigate={(item) => investigateFinding(historyFiltersForFinding(item))} />
                        ))}
                    </div>
                  )}
                </Section>
                {SECTIONS.map((section) => {
                  const items = findings.items.filter((item) => item.kind === section.kind);
                  return (
                    <Section key={section.kind} title={t(section.titleKey)} sub={t(section.subKey)} right={<span className="pl-mono pl-small">{findings.countsByKind[section.kind] ?? 0}</span>}>
                      {items.length === 0 ? (
                        <div className="pl-review__empty">{t('review.noCandidates')}</div>
                      ) : (
                        <div className="pl-review__list">
                          {items.map((finding) => (
                            <FindingRow key={finding.id} finding={finding} onInvestigate={(item) => investigateFinding(historyFiltersForFinding(item))} />
                          ))}
                        </div>
                      )}
                    </Section>
                  );
                })}
                <div className="pl-review__method">{t('review.methodNote')}</div>
              </>
            ) : null}
          </div>
        </div>
      </div>
    </PageGate>
  );
};
