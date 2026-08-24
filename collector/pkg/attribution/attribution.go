package attribution

import (
	"math"
	"strings"

	"github.com/Whatnamed/ProxyLens/collector/pkg/types"
)

// ClassifyRoute 派生路由分类
func ClassifyRoute(chains []string) types.RouteType {
	if len(chains) == 0 {
		return types.RouteUnknown
	}
	topHop := strings.ToUpper(strings.TrimSpace(chains[0]))
	if topHop == "DIRECT" {
		return types.RouteDirect
	}
	if topHop == "REJECT" {
		return types.RouteReject
	}
	return types.RouteProxy
}

// ClassifyInitialAttribution 进行初阶归因分类 (严格遵守 Phase 0 边界)
func ClassifyInitialAttribution(conn *types.ConnectionSnapshot) types.AttributionClass {
	hasProcess := strings.TrimSpace(conn.Metadata.Process) != ""
	hasRule := strings.TrimSpace(conn.Rule) != ""

	if hasProcess && hasRule {
		return types.ClassKnownApplication
	}

	// 仅在进程与规则均缺失且具有代理链路时标记为 relay_candidate
	if !hasProcess && !hasRule && len(conn.Chains) > 0 {
		return types.ClassRelayCandidate
	}

	return types.ClassUnpairedMissingAttr
}

// CheckStructuralRelayPair 严格验证底层中继候选连接与应用连接的配对关系
func CheckStructuralRelayPair(
	candidate *types.ConnectionSnapshot,
	logical *types.ConnectionSnapshot,
	candUpDelta, candDownDelta int64,
	logUpDelta, logDownDelta int64,
) (bool, map[string]any) {
	candChains := candidate.Chains
	logChains := logical.Chains

	if len(candChains) == 0 || len(logChains) <= 1 {
		return false, nil
	}

	// 1. 结构关系判定：物理出站节点必须一致 (chains[0])
	if candChains[0] != logChains[0] {
		return false, nil
	}

	// 2. 检查 shared structural hops
	var sharedHops []string
	logHopMap := make(map[string]bool, len(logChains))
	for _, hop := range logChains {
		logHopMap[hop] = true
	}
	for _, hop := range candChains {
		if logHopMap[hop] {
			sharedHops = append(sharedHops, hop)
		}
	}
	if len(sharedHops) == 0 {
		return false, nil
	}

	// 3. 流量保守吻合判定
	upDiff := math.Abs(float64(candUpDelta - logUpDelta))
	downDiff := math.Abs(float64(candDownDelta - logDownDelta))

	upMatch := upDiff < 2000 || (logUpDelta > 0 && (upDiff/float64(logUpDelta)) < 0.05)
	downMatch := downDiff < 2000 || (logDownDelta > 0 && (downDiff/float64(logDownDelta)) < 0.05)

	hasTraffic := candUpDelta > 500 || candDownDelta > 500 || logUpDelta > 500 || logDownDelta > 500
	if !hasTraffic {
		return false, nil
	}

	var trafficMatch bool
	if logUpDelta > 500 && logDownDelta > 500 {
		trafficMatch = upMatch && downMatch
	} else if logDownDelta > 2000 && logUpDelta <= 500 {
		trafficMatch = downMatch && (upDiff < 2000)
	} else if logUpDelta > 2000 && logDownDelta <= 500 {
		trafficMatch = upMatch && (downDiff < 2000)
	} else {
		trafficMatch = upMatch && downMatch
	}

	if !trafficMatch {
		return false, nil
	}

	evidence := map[string]any{
		"candidateId":            candidate.ID,
		"logicalId":              logical.ID,
		"candidateChains":        candChains,
		"logicalChains":          logChains,
		"sharedHops":             sharedHops,
		"structuralRelation":     true,
		"uploadMatch":            upMatch,
		"downloadMatch":          downMatch,
		"candidateUploadDelta":   candUpDelta,
		"logicalUploadDelta":     logUpDelta,
		"candidateDownloadDelta": candDownDelta,
		"logicalDownloadDelta":   logDownDelta,
	}

	return true, evidence
}

// PerformFrameRelayDeduplication 在当前帧的增量中执行保守 Relay 配对（处理 1-to-1 确凿匹配，排除歧义）
func PerformFrameRelayDeduplication(
	connections []types.ConnectionSnapshot,
	deltas map[string][2]int64,
) map[string]map[string]any {
	confirmedRelays := make(map[string]map[string]any)

	var candidates []*types.ConnectionSnapshot
	var logicals []*types.ConnectionSnapshot

	for i := range connections {
		c := &connections[i]
		initialClass := ClassifyInitialAttribution(c)
		if initialClass == types.ClassRelayCandidate {
			candidates = append(candidates, c)
		} else if initialClass == types.ClassKnownApplication {
			logicals = append(logicals, c)
		}
	}

	if len(candidates) == 0 || len(logicals) == 0 {
		return confirmedRelays
	}

	// 记录每个 candidate 匹配到的 logicals 列表
	candidateMatches := make(map[string][]struct {
		logicalID string
		evidence  map[string]any
	})
	// 记录每个 logical 匹配到的 candidate IDs
	logicalMatches := make(map[string][]string)

	for _, cand := range candidates {
		candDelta := deltas[cand.ID]
		for _, log := range logicals {
			logDelta := deltas[log.ID]
			matched, evidence := CheckStructuralRelayPair(cand, log, candDelta[0], candDelta[1], logDelta[0], logDelta[1])
			if matched {
				candidateMatches[cand.ID] = append(candidateMatches[cand.ID], struct {
					logicalID string
					evidence  map[string]any
				}{
					logicalID: log.ID,
					evidence:  evidence,
				})
				logicalMatches[log.ID] = append(logicalMatches[log.ID], cand.ID)
			}
		}
	}

	// 严格 1-to-1 判定：只有当 candidate 仅匹配唯一 1 个 logical，且该 logical 仅匹配唯一 1 个 candidate 时才确认为 confirmed duplicate
	for candID, matches := range candidateMatches {
		if len(matches) == 1 {
			targetLogicalID := matches[0].logicalID
			if len(logicalMatches[targetLogicalID]) == 1 {
				confirmedRelays[candID] = matches[0].evidence
			}
		}
	}

	return confirmedRelays
}
