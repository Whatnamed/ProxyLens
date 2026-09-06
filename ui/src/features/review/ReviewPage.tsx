import React from 'react';
import { QueryApiClient } from '../../api/client';
import { AuditFinding, AuditFindingKind, MetaResponse } from '../../api/types';
import { useAuditContext, useLocale } from '../../state/AuditContext';
import { useAuditFindingsQuery } from '../../api/queries';
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
import { findingIsEstimated, findingKindKey, findingTargetLabel, historyFiltersForFinding } from './reviewSemantics';

const SECTIONS: { kind: AuditFindingKind; titleKey: string; subKey: string }[] = [
  { kind: 'match_fallback_proxy', titleKey: 'review.sectionMatch', subKey: 'review.sectionMatchSub' },
  { kind: 'broad_udp_proxy', titleKey: 'review.sectionUdp', subKey: 'review.sectionUdpSub' },
  { kind: 'ip_only_proxy_target', titleKey: 'review.sectionIp', subKey: 'review.sectionIpSub' },
  { kind: 'large_proxy_connection', titleKey: 'review.sectionLarge', subKey: 'review.sectionLargeSub' },
];

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
          <div>
            <dt>{t('review.rule')}</dt>
            <dd className="pl-mono">{evidence.rule || t('common.notAvailable')}</dd>
          </div>
          <div>
            <dt>{t('review.network')}</dt>
            <dd className="pl-mono">{evidence.network || t('common.notAvailable')}</dd>
          </div>
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
          {evidence.rule && <span>{t('review.ruleFact', { rule: evidence.rule })}</span>}
          {evidence.network && <span>{t('review.networkFact', { network: evidence.network })}</span>}
          <span>{targetFact}</span>
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
  const { resolvedRange, rangeSourceKey, investigateFinding } = useAuditContext();
  const findingsQ = useAuditFindingsQuery(client, resolvedRange.from, resolvedRange.to, rangeSourceKey, 20);
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
            {findingsQ.isLoading ? (
              <SkeletonRows rows={10} />
            ) : findingsQ.isError && !noRun ? (
              <ErrorState title={t('review.queryFailed')} body={String((findingsQ.error as Error)?.message ?? findingsQ.error)} code={errorCodeOf(findingsQ.error)} />
            ) : noRun ? (
              <EmptyState title={t('review.noAccountingTitle')} body={t('review.noAccountingBody')} />
            ) : findings ? (
              <>
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
