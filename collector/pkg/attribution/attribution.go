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

// PerformFrameRelayDeduplication 在当前帧的增量中自动执行 Relay Candidate 结构去重配对
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

	// 贪心匹配具有确凿结构证据与流量证据的 Pair
	for _, cand := range candidates {
		candDelta := deltas[cand.ID]
		for _, log := range logicals {
			logDelta := deltas[log.ID]
			matched, evidence := CheckStructuralRelayPair(cand, log, candDelta[0], candDelta[1], logDelta[0], logDelta[1])
			if matched {
				confirmedRelays[cand.ID] = evidence
				break // 已确认为 confirmed duplicate，避免重复匹配
			}
		}
	}

	return confirmedRelays
}
