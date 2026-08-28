import { QueryApiClient } from '../../api/client';
import { useAudit } from '../../state/AuditContext';
import { NavRail } from './NavRail';
import { AuditContextBar } from './AuditContextBar';
import { OverviewView } from '../../features/overview/OverviewView';
import { HistoryView } from '../../features/history/HistoryView';
import { CoverageView } from '../../features/coverage/CoverageView';

/**
 * Three top-level destinations only.
 * Connection Detail is reachable exclusively from History as an Inspector.
 */
export const AppShell: React.FC<{ client: QueryApiClient | null }> = ({ client }) => {
  const { surface, navigate } = useAudit();

  return (
    <div className="shell">
      <NavRail surface={surface} onNavigate={navigate} client={client} />
      <div className="main">
        <AuditContextBar />
        <div className="surface-host">
          {surface === 'overview' && <OverviewView client={client} />}
          {surface === 'history' && <HistoryView client={client} />}
          {surface === 'coverage' && <CoverageView client={client} />}
        </div>
      </div>
    </div>
  );
};
