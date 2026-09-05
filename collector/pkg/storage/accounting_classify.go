package storage

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Whatnamed/ProxyLens/collector/pkg/types"
)

// This file holds the shared accounting semantics used identically by the
// legacy full rebuild and incremental accounting v2:
//
//   - classifyConnectionGroup: the relay reconciliation contract applied per
//     (session, epoch) group;
//   - allocateAccountedRowBuckets / accountedDimensionPairs: the hourly bucket
//     allocation and dimension projection of one accounted row.
//
// Both algorithms must never drift: equivalence between legacy and v2 is a
// Phase 3S acceptance gate.

// classifyConnectionGroup classifies one (session, epoch) connection group and
// returns the accounting class per connection plus relay relation records.
// Returned relation records carry an empty RunID; callers stamp their own
// run/generation identifier before persisting them.
func classifyConnectionGroup(conns []*connInfo) (map[connKey]AccountingClass, []RelayRelationRecord) {
	resultClasses := make(map[connKey]AccountingClass)
	var relationsToInsert []RelayRelationRecord

	var candidates []*connInfo
	var logicals []*connInfo

	for _, c := range conns {
		hasProcess := strings.TrimSpace(c.process) != ""
		hasRule := strings.TrimSpace(c.rule) != ""
		hasProcessAndRule := hasProcess && hasRule

		// 核心原则：如果 metadata 明确具备 process+rule (或显式为 KnownApplication)，绝对不是 Candidate！
		isLogical := hasProcessAndRule || c.attributionClass == types.ClassKnownApplication

		isCandidate := false
		if !hasProcessAndRule {
			if c.attributionClass == types.ClassRelayCandidate ||
				c.attributionClass == types.ClassConfirmedRelayDuplicate ||
				(!hasProcess && !hasRule && len(c.chains) > 0 && c.route == types.RouteProxy) {
				isCandidate = true
			}
		}

		if isLogical {
			logicals = append(logicals, c)
			resultClasses[c.key] = ClassUnique
		} else if isCandidate {
			candidates = append(candidates, c)
		} else {
			resultClasses[c.key] = ClassMissingAttribution
		}
	}

	candidateMatches := make(map[connKey][]*connInfo)
	logicalMatches := make(map[connKey][]*connInfo)
	matchEvidenceMap := make(map[string]map[string]any)

	for _, cand := range candidates {
		for _, log := range logicals {
			candChains := cand.chains
			logChains := log.chains

			if len(candChains) == 0 || len(logChains) <= 1 {
				continue
			}
			if candChains[0] != logChains[0] {
				continue
			}

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
				continue
			}

			if cand.lastObs.Before(log.firstObs) || cand.firstObs.After(log.lastObs) {
				continue
			}
			overlapStart := cand.firstObs
			if log.firstObs.After(overlapStart) {
				overlapStart = log.firstObs
			}
			overlapEnd := cand.lastObs
			if log.lastObs.Before(overlapEnd) {
				overlapEnd = log.lastObs
			}
			overlapMs := overlapEnd.Sub(overlapStart).Milliseconds()
			if overlapMs < 0 {
				continue
			}

			hasTraffic := cand.monitoredUp > 500 || cand.monitoredDown > 500 || log.monitoredUp > 500 || log.monitoredDown > 500
			if !hasTraffic {
				continue
			}

			upDiff := cand.monitoredUp - log.monitoredUp
			if upDiff < 0 {
				upDiff = -upDiff
			}
			downDiff := cand.monitoredDown - log.monitoredDown
			if downDiff < 0 {
				downDiff = -downDiff
			}

			maxUp := log.monitoredUp
			if cand.monitoredUp > maxUp {
				maxUp = cand.monitoredUp
			}
			maxDown := log.monitoredDown
			if cand.monitoredDown > maxDown {
				maxDown = cand.monitoredDown
			}

			upRatio := 1.0
			if maxUp > 0 {
				upRatio = float64(upDiff) / float64(maxUp)
			}
			downRatio := 1.0
			if maxDown > 0 {
				downRatio = float64(downDiff) / float64(maxDown)
			}

			upMatch := (maxUp > 0 && upRatio <= 0.05) || (upDiff < 2000 && upRatio < 0.20)
			downMatch := (maxDown > 0 && downRatio <= 0.05) || (downDiff < 2000 && downRatio < 0.20)

			var trafficMatch bool
			if log.monitoredUp > 500 && log.monitoredDown > 500 {
				trafficMatch = upMatch && downMatch
			} else if log.monitoredDown > 2000 && log.monitoredUp <= 500 {
				trafficMatch = downMatch && (upDiff < 2000 && upRatio < 0.20)
			} else if log.monitoredUp > 2000 && log.monitoredDown <= 500 {
				trafficMatch = upMatch && (downDiff < 2000 && downRatio < 0.20)
			} else {
				trafficMatch = upMatch && downMatch
			}

			if trafficMatch {
				candidateMatches[cand.key] = append(candidateMatches[cand.key], log)
				logicalMatches[log.key] = append(logicalMatches[log.key], cand)

				pairKey := fmt.Sprintf("%s:%s", cand.key.connectionID, log.key.connectionID)
				matchEvidenceMap[pairKey] = map[string]any{
					"overlapMs":         overlapMs,
					"sharedHops":        sharedHops,
					"candidateChains":   cand.chains,
					"logicalChains":     log.chains,
					"candidateTotals":   map[string]int64{"upload": cand.monitoredUp, "download": cand.monitoredDown},
					"logicalTotals":     map[string]int64{"upload": log.monitoredUp, "download": log.monitoredDown},
					"uploadDiffRatio":   upRatio,
					"downloadDiffRatio": downRatio,
				}
			}
		}
	}

	stamp := func(r RelayRelationRecord, evidence map[string]any) RelayRelationRecord {
		evJSON, _ := json.Marshal(evidence)
		r.EvidenceJSON = string(evJSON)
		r.DerivationVersion = AccountingAlgorithmVersion
		return r
	}

	for _, cand := range candidates {
		matchedLogicals := candidateMatches[cand.key]

		if len(matchedLogicals) == 0 {
			resultClasses[cand.key] = ClassMissingAttribution
			evidence := map[string]any{
				"decisionReason":  "unpaired_no_matching_logical_connection",
				"candidateChains": cand.chains,
				"candidateTotals": map[string]int64{"upload": cand.monitoredUp, "download": cand.monitoredDown},
				"candidateKey":    fmt.Sprintf("%s:%d:%s", cand.key.sessionID, cand.key.epochID, cand.key.connectionID),
			}
			relationsToInsert = append(relationsToInsert, stamp(RelayRelationRecord{
				CandidateSessionID:    cand.key.sessionID,
				CandidateEpochID:      cand.key.epochID,
				CandidateConnectionID: cand.key.connectionID,
				Status:                RelayUnpaired,
			}, evidence))
		} else if len(matchedLogicals) == 1 {
			singleLogical := matchedLogicals[0]
			pairKey := fmt.Sprintf("%s:%s", cand.key.connectionID, singleLogical.key.connectionID)
			if len(logicalMatches[singleLogical.key]) == 1 {
				resultClasses[cand.key] = ClassConfirmedRelayDuplicate
				evidence := matchEvidenceMap[pairKey]
				if evidence == nil {
					evidence = make(map[string]any)
				}
				evidence["decisionReason"] = "strict_1to1_structural_overlap_match"
				evidence["candidateKey"] = fmt.Sprintf("%s:%d:%s", cand.key.sessionID, cand.key.epochID, cand.key.connectionID)
				evidence["logicalKey"] = fmt.Sprintf("%s:%d:%s", singleLogical.key.sessionID, singleLogical.key.epochID, singleLogical.key.connectionID)
				relationsToInsert = append(relationsToInsert, stamp(RelayRelationRecord{
					CandidateSessionID:    cand.key.sessionID,
					CandidateEpochID:      cand.key.epochID,
					CandidateConnectionID: cand.key.connectionID,
					LogicalSessionID:      singleLogical.key.sessionID,
					LogicalEpochID:        singleLogical.key.epochID,
					LogicalConnectionID:   singleLogical.key.connectionID,
					Status:                RelayConfirmed,
				}, evidence))
			} else {
				resultClasses[cand.key] = ClassAmbiguousRelay
				evidence := matchEvidenceMap[pairKey]
				if evidence == nil {
					evidence = make(map[string]any)
				}
				evidence["decisionReason"] = "n_to_1_logical_ambiguity"
				evidence["candidateKey"] = fmt.Sprintf("%s:%d:%s", cand.key.sessionID, cand.key.epochID, cand.key.connectionID)
				evidence["logicalKey"] = fmt.Sprintf("%s:%d:%s", singleLogical.key.sessionID, singleLogical.key.epochID, singleLogical.key.connectionID)
				relationsToInsert = append(relationsToInsert, stamp(RelayRelationRecord{
					CandidateSessionID:    cand.key.sessionID,
					CandidateEpochID:      cand.key.epochID,
					CandidateConnectionID: cand.key.connectionID,
					LogicalSessionID:      singleLogical.key.sessionID,
					LogicalEpochID:        singleLogical.key.epochID,
					LogicalConnectionID:   singleLogical.key.connectionID,
					Status:                RelayAmbiguous,
				}, evidence))
			}
		} else {
			resultClasses[cand.key] = ClassAmbiguousRelay
			evidence := map[string]any{
				"decisionReason":  "1_to_n_candidate_ambiguity",
				"candidateChains": cand.chains,
				"candidateTotals": map[string]int64{"upload": cand.monitoredUp, "download": cand.monitoredDown},
				"matchedCount":    len(matchedLogicals),
				"candidateKey":    fmt.Sprintf("%s:%d:%s", cand.key.sessionID, cand.key.epochID, cand.key.connectionID),
			}
			relationsToInsert = append(relationsToInsert, stamp(RelayRelationRecord{
				CandidateSessionID:    cand.key.sessionID,
				CandidateEpochID:      cand.key.epochID,
				CandidateConnectionID: cand.key.connectionID,
				Status:                RelayAmbiguous,
			}, evidence))
		}
	}

	return resultClasses, relationsToInsert
}

// accountedAllocation is one hourly-bucket share of an accounted row.
type accountedAllocation struct {
	bucketStart time.Time
	upBytes     int64
	downBytes   int64
	isExact     bool
}

// accountedDimPair is one (dimension_type, dimension_key) projection of an
// accounted row.
type accountedDimPair struct {
	dimType string
	dimKey  string
}

// allocateAccountedRowBuckets mirrors the legacy hourly allocation semantics
// exactly: exact_snapshot rows (and rows without a usable interval) land
// entirely in the observation bucket; interval rows are split proportionally
// across the crossed hour buckets.
func allocateAccountedRowBuckets(obsTime time.Time, intStart, intEnd *time.Time, prec string, accUp, accDown int64) []accountedAllocation {
	if prec == "exact_snapshot" || intStart == nil || intEnd == nil {
		return []accountedAllocation{{
			bucketStart: obsTime.Truncate(time.Hour),
			upBytes:     accUp,
			downBytes:   accDown,
			isExact:     true,
		}}
	}

	startTime := *intStart
	endTime := *intEnd
	if !endTime.After(startTime) {
		return []accountedAllocation{{
			bucketStart: obsTime.Truncate(time.Hour),
			upBytes:     accUp,
			downBytes:   accDown,
			isExact:     false,
		}}
	}

	allocUp := NewIntervalAllocator(startTime, endTime, accUp)
	allocDown := NewIntervalAllocator(startTime, endTime, accDown)

	var allocations []accountedAllocation
	curr := startTime
	for curr.Before(endTime) {
		bucket := curr.Truncate(time.Hour)
		nextBucket := bucket.Add(time.Hour)
		bucketEnd := nextBucket
		if bucketEnd.After(endTime) {
			bucketEnd = endTime
		}

		allocations = append(allocations, accountedAllocation{
			bucketStart: bucket,
			upBytes:     allocUp.Allocate(curr, bucketEnd),
			downBytes:   allocDown.Allocate(curr, bucketEnd),
			isExact:     false,
		})
		curr = bucketEnd
	}
	return allocations
}

// accountedDimensionPairs projects one accounted row onto its non-empty
// dimension keys, matching the legacy dimension list exactly.
func accountedDimensionPairs(proc, host, destIP, network, rule, rulePayload, finalProxy, topGroup sql.NullString) []accountedDimPair {
	dims := []accountedDimPair{{dimType: "total", dimKey: "total"}}
	if proc.Valid && proc.String != "" {
		dims = append(dims, accountedDimPair{dimType: "process", dimKey: proc.String})
	}
	if host.Valid && host.String != "" {
		dims = append(dims, accountedDimPair{dimType: "host", dimKey: host.String})
	}
	if destIP.Valid && destIP.String != "" {
		dims = append(dims, accountedDimPair{dimType: "destination_ip", dimKey: destIP.String})
	}
	if rule.Valid && rule.String != "" {
		dims = append(dims, accountedDimPair{dimType: "rule", dimKey: rule.String})
	}
	if rulePayload.Valid && rulePayload.String != "" {
		dims = append(dims, accountedDimPair{dimType: "rule_payload", dimKey: rulePayload.String})
	}
	if finalProxy.Valid && finalProxy.String != "" {
		dims = append(dims, accountedDimPair{dimType: "final_proxy", dimKey: finalProxy.String})
	}
	if topGroup.Valid && topGroup.String != "" {
		dims = append(dims, accountedDimPair{dimType: "top_policy_group", dimKey: topGroup.String})
	}
	if network.Valid && network.String != "" {
		dims = append(dims, accountedDimPair{dimType: "network", dimKey: network.String})
	}
	return dims
}
