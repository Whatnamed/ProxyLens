import {
  QueryApiSession,
  ApiErrorResponse,
  MetaResponse,
  UsageSummary,
  TopDimensionResponse,
  TopRulesResponse,
  CoverageSummary,
  ConnectionsListResponse,
  ConnectionDetailResponse,
  ConnectionTrafficResponse,
  AuditFindingResult,
  ProcessChangeResult,
  TemporalFindingsResult,
} from './types';

export class ApiClientError extends Error {
  code: string;
  status: number;
  details?: Record<string, unknown>;

  constructor(status: number, code: string, message: string, details?: Record<string, unknown>) {
    super(message);
    this.name = 'ApiClientError';
    this.status = status;
    this.code = code;
    this.details = details;
  }
}

export class QueryApiClient {
  private session: QueryApiSession;

  constructor(session: QueryApiSession) {
    this.session = session;
  }

  private async request<T>(path: string, params?: Record<string, string | number | undefined>): Promise<T> {
    const url = new URL(path, this.session.baseUrl);
    if (params) {
      Object.entries(params).forEach(([k, v]) => {
        if (v !== undefined && v !== '') {
          url.searchParams.set(k, String(v));
        }
      });
    }

    let res: Response;
    try {
      res = await fetch(url.toString(), {
        method: 'GET',
        headers: {
          'Authorization': `Bearer ${this.session.token}`,
          'Accept': 'application/json'
        }
      });
    } catch (networkErr: unknown) {
      const msg = networkErr instanceof Error ? networkErr.message : String(networkErr);
      throw new ApiClientError(0, 'API_UNAVAILABLE', `Network error connecting to Query API: ${msg}`);
    }

    if (!res.ok) {
      let errPayload: ApiErrorResponse | null = null;
      try {
        errPayload = await res.json();
      } catch {}

      const code = errPayload?.error?.code || (res.status === 401 ? 'UNAUTHORIZED' : 'HTTP_ERROR');
      const message = errPayload?.error?.message || `Request failed with status ${res.status}`;
      throw new ApiClientError(res.status, code, message, errPayload?.error?.details);
    }

    return res.json() as Promise<T>;
  }

  async getMeta(): Promise<MetaResponse> {
    return this.request<MetaResponse>('/api/v1/meta');
  }

  async getSummary(from?: string, to?: string, route?: string): Promise<UsageSummary> {
    return this.request<UsageSummary>('/api/v1/analytics/summary', { from, to, route });
  }

  async getTopProcesses(from?: string, to?: string, route?: string, limit?: number): Promise<TopDimensionResponse> {
    return this.request<TopDimensionResponse>('/api/v1/analytics/top/processes', { from, to, route, limit });
  }

  async getTopHosts(from?: string, to?: string, route?: string, limit?: number): Promise<TopDimensionResponse> {
    return this.request<TopDimensionResponse>('/api/v1/analytics/top/hosts', { from, to, route, limit });
  }

  async getTopRules(from?: string, to?: string, route?: string, limit?: number): Promise<TopRulesResponse> {
    return this.request<TopRulesResponse>('/api/v1/analytics/top/rules', { from, to, route, limit });
  }

  async getTopFinalProxies(from?: string, to?: string, route?: string, limit?: number): Promise<TopDimensionResponse> {
    return this.request<TopDimensionResponse>('/api/v1/analytics/top/final-proxies', { from, to, route, limit });
  }

  async getProtocols(from?: string, to?: string, route?: string, limit?: number): Promise<TopDimensionResponse> {
    return this.request<TopDimensionResponse>('/api/v1/analytics/protocols', { from, to, route, limit });
  }

  async getCoverage(from?: string, to?: string): Promise<CoverageSummary> {
    return this.request<CoverageSummary>('/api/v1/coverage', { from, to });
  }

  async getConnections(params?: {
    from?: string;
    to?: string;
    route?: string;
    process?: string;
    host?: string;
    destinationIp?: string;
    network?: string;
    rule?: string;
    limit?: number;
    offset?: number;
  }): Promise<ConnectionsListResponse> {
    return this.request<ConnectionsListResponse>('/api/v1/connections', params);
  }

  async getAuditFindings(from: string, to: string, limitPerKind = 20): Promise<AuditFindingResult> {
    return this.request<AuditFindingResult>('/api/v1/intelligence/findings', { from, to, limitPerKind });
  }

  async getProcessChanges(
    baselineFrom: string,
    baselineTo: string,
    recentFrom: string,
    recentTo: string,
    limitPerKind = 20,
  ): Promise<ProcessChangeResult> {
    return this.request<ProcessChangeResult>('/api/v1/intelligence/process-changes', {
      baselineFrom,
      baselineTo,
      recentFrom,
      recentTo,
      limitPerKind,
    });
  }

  async getTemporalFindings(
    baselineFrom: string,
    baselineTo: string,
    recentFrom: string,
    recentTo: string,
    limitPerKind = 20,
  ): Promise<TemporalFindingsResult> {
    return this.request<TemporalFindingsResult>('/api/v1/intelligence/temporal-findings', {
      baselineFrom,
      baselineTo,
      recentFrom,
      recentTo,
      limitPerKind,
    });
  }

  async getConnectionDetail(sessionId: string, epochId: number, connectionId: string): Promise<ConnectionDetailResponse> {
    return this.request<ConnectionDetailResponse>(`/api/v1/connections/${sessionId}/${epochId}/${connectionId}`);
  }

  async getConnectionTraffic(sessionId: string, epochId: number, connectionId: string): Promise<ConnectionTrafficResponse> {
    return this.request<ConnectionTrafficResponse>(`/api/v1/connections/${sessionId}/${epochId}/${connectionId}/traffic`);
  }
}
