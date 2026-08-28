export interface QueryApiSession {
  baseUrl: string;
  token: string;
  apiVersion: string;
}

export interface ApiErrorDetail {
  code: string;
  message: string;
  details?: Record<string, unknown>;
}

export interface ApiErrorResponse {
  error: ApiErrorDetail;
}

export interface CollectorSessionRecord {
  sessionId: string;
  startedAt: string;
  endedAt?: string;
  lastEventAt?: string;
  lastFrameSequence: number;
  status: 'running' | 'closed_clean' | 'interrupted';
  collectorVersion?: string;
  lastHeartbeatAt?: string;
  heartbeatIntervalMs?: number;
  createdAt: string;
  updatedAt: string;
}

export interface AccountingRunRecord {
  runId: string;
  algorithmVersion: string;
  startedAt: string;
  completedAt?: string;
  status: 'running' | 'completed' | 'failed';
  sourceJournalEventCount: number;
  sourceJournalSequenceMax?: number;
  sourceBoundaryJson: string;
  failedReason?: string;
  notes?: string;
}

export interface AccountingFreshness {
  runId: string;
  sourceJournalSequenceMax: number;
  currentJournalSequenceMax: number;
  lagEvents: number;
  isFresh: boolean;
  completedAt?: string;
}

export interface MetaResponse {
  apiVersion: string;
  appVersion: string;
  dbState: 'READY' | 'UNAVAILABLE' | 'UNINITIALIZED' | 'INCOMPATIBLE';
  schemaVersion: number;
  maxBinarySchemaVersion: number;
  latestCollectorSession?: CollectorSessionRecord;
  latestAccountingRun?: AccountingRunRecord;
  freshness?: AccountingFreshness;
}

export interface MergedGap {
  source: string;
  sources?: string[];
  startedAt: string;
  endedAt: string;
  durationMs: number;
  reason: string;
  reasons?: string[];
}

export interface CoverageSummary {
  requestedStart?: string;
  requestedEnd?: string;
  knownScopeStart?: string;
  effectiveScopeStart?: string;
  effectiveScopeEnd?: string;
  coveredDurationMs: number;
  uncoveredDurationMs: number;
  outsideKnownScopeMs: number;
  futureDurationMs: number;
  coverageRatio?: number;
  controllerGapDurationMs: number;
  collectorOfflineDurationMs: number;
  mergedGaps: MergedGap[];
}

export interface UsageSummary {
  rawObservedUpload: number;
  rawObservedDownload: number;
  uniqueObservedUpload: number;
  uniqueObservedDownload: number;
  proxyUpload: number;
  proxyDownload: number;
  directUpload: number;
  directDownload: number;
  rejectUpload: number;
  rejectDownload: number;
  unknownRouteUpload: number;
  unknownRouteDownload: number;
  missingAttributionUpload: number;
  missingAttributionDownload: number;
  ambiguousRelayUpload: number;
  ambiguousRelayDownload: number;
  samplingResidualUpload: number;
  samplingResidualDownload: number;
  controllerGapPhysicalUpload: number;
  controllerGapPhysicalDownload: number;
  coverage?: CoverageSummary;
  freshness?: AccountingFreshness;
  accountingVersion: string;
}

export interface TopDimensionItem {
  key: string;
  route: string;
  uploadBytes: number;
  downloadBytes: number;
  totalBytes: number;
  connectionCount: number;
  exactUploadBytes: number;
  exactDownloadBytes: number;
  estimatedUploadBytes: number;
  estimatedDownloadBytes: number;
}

export interface TopDimensionResponse {
  items: TopDimensionItem[];
  limit: number;
}

export interface TopRuleItem {
  rule: string;
  rulePayload: string;
  route: string;
  uploadBytes: number;
  downloadBytes: number;
  totalBytes: number;
  connectionCount: number;
  exactUploadBytes: number;
  exactDownloadBytes: number;
  estimatedUploadBytes: number;
  estimatedDownloadBytes: number;
}

export interface TopRulesResponse {
  items: TopRuleItem[];
  limit: number;
}

/**
 * Metadata completeness flags derived at projection time.
 * NOTE: this is the serialized `types.QualityFlags` object (booleans), not a
 * string array — verified against `collector/pkg/types/types.go`.
 */
export interface QualityFlags {
  missingProcess: boolean;
  missingProcessPath: boolean;
  missingHost: boolean;
  ipOnly: boolean;
  missingRule: boolean;
  missingChain: boolean;
}

export interface ConnectionRecord {
  sessionId: string;
  epochId: number;
  connectionId: string;
  mihomoStart?: string;
  firstObservedAt: string;
  lastObservedAt: string;
  disappearedObservedAt?: string;
  observationEndedAt?: string;
  observationEndReason?: string;
  observationEndEventId?: string;
  observationActive: boolean;
  state: string;
  preexistingAtStart: boolean;
  possibleUnobservedTail: boolean;
  startClassification?: string;
  metadata: {
    process?: string;
    processPath?: string;
    host?: string;
    sniffHost?: string;
    network?: string;
    type?: string;
    sourceIP?: string;
    sourcePort?: string;
    destinationIP?: string;
    remoteDestination?: string;
    destinationPort?: string;
    dnsMode?: string;
    specialProxy?: string;
    specialRules?: string;
    inboundUser?: string;
    inboundName?: string;
    inboundPort?: string;
  };
  rule?: string;
  rulePayload?: string;
  chains?: string[];
  providerChains?: string[];
  route: string;
  latestAttributionClass?: string;
  qualityFlags?: QualityFlags;
  relayEvidence?: Record<string, unknown>;
  baselineUploadCounter: number;
  baselineDownloadCounter: number;
  lastObservedUploadCounter: number;
  lastObservedDownloadCounter: number;
  monitoredUploadTotal: number;
  monitoredDownloadTotal: number;
}

export interface ConnectionsListResponse {
  items: ConnectionRecord[];
  limit: number;
  offset: number;
  hasMore: boolean;
}

/**
 * Precision values the backend is known to emit.
 * `exact_snapshot` / `estimated` are accepted alongside the documented
 * `exact` / `interval_derived`; any unrecognised value is surfaced as
 * "Unrecognised precision" rather than silently treated as exact.
 */
export type PrecisionValue =
  | 'exact'
  | 'exact_snapshot'
  | 'interval_derived'
  | 'estimated'
  | (string & {});

export interface AccountedTrafficRecord {
  runId: string;
  sourceEventId: string;
  sessionId: string;
  epochId: number;
  connectionId: string;
  observedAt: string;
  intervalStart?: string;
  intervalEnd?: string;
  precision: PrecisionValue;
  route: string;
  rawUpload: number;
  rawDownload: number;
  accountedUpload: number;
  accountedDownload: number;
  accountingClass: string;
  process?: string;
  processPath?: string;
  host?: string;
  sniffHost?: string;
  destinationIp?: string;
  network?: string;
  rule?: string;
  rulePayload?: string;
  finalProxy?: string;
  topPolicyGroup?: string;
  dimensionDerivationVersion?: string;
}

export interface ConnectionAccountingSummary {
  runId: string;
  accountingClass: string;
  route: string;
  rawUploadTotal: number;
  rawDownloadTotal: number;
  accountedUploadTotal: number;
  accountedDownloadTotal: number;
  latestProcess?: string;
  latestProcessPath?: string;
  latestHost?: string;
  latestSniffHost?: string;
  latestDestinationIp?: string;
  latestNetwork?: string;
  latestRule?: string;
  latestRulePayload?: string;
  latestFinalProxy?: string;
  latestTopPolicyGroup?: string;
}

export interface ConnectionDetailResponse {
  connection: ConnectionRecord;
  accountingEvents: AccountedTrafficRecord[];
  accountingSummary?: ConnectionAccountingSummary;
}

/** Raw traffic sampling frame: `GET /api/v1/connections/{s}/{e}/{c}/traffic` */
export interface ConnectionTrafficFrame {
  eventId: string;
  sessionId: string;
  epochId: number;
  frameSequence: number;
  eventSequence: number;
  connectionId: string;
  observedAt: string;
  intervalStart?: string;
  intervalEnd?: string;
  precision: PrecisionValue;
  deltaUpload: number;
  deltaDownload: number;
  observedUploadCounter: number;
  observedDownloadCounter: number;
  monitoredUploadTotal: number;
  monitoredDownloadTotal: number;
}

export interface ConnectionTrafficResponse {
  connectionId: string;
  traffic: ConnectionTrafficFrame[];
}


