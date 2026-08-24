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

// ClassifyInitialAttribution 进行初阶归因分类
func ClassifyInitialAttribution(conn *types.ConnectionSnapshot) types.AttributionClass {
	hasProcess := strings.TrimSpace(conn.Metadata.Process) != ""
	hasRule := strings.TrimSpace(conn.Rule) != ""

	if hasProcess && hasRule {
		return types.ClassKnownApplication
	}

	if len(conn.Chains) > 0 {
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
		"candidateId":         candidate.ID,
		"logicalId":           logical.ID,
		"candidateChains":     candChains,
		"logicalChains":       logChains,
		"sharedHops":          sharedHops,
		"structuralRelation":  true,
		"uploadMatch":         upMatch,
		"downloadMatch":       downMatch,
		"candidateUploadDelta": candUpDelta,
		"logicalUploadDelta":   logUpDelta,
		"candidateDownloadDelta": candDownDelta,
		"logicalDownloadDelta":   logDownDelta,
	}

	return true, evidence
}
