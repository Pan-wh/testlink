package verdict

import "testlink/internal/model"

func Compute(results []model.ProbeResult) (code, detail string) {
	var baseline, business []model.ProbeResult
	for _, r := range results {
		if r.Role == "基线" || r.Role == "baseline" {
			baseline = append(baseline, r)
		} else {
			business = append(business, r)
		}
	}

	hasBaseline := len(baseline) > 0
	baselineOK := anyReachable(baseline)
	baselineFailed := failedTargets(baseline)
	bizFailed := failedTargets(business)

	// Count distinct business targets
	seen := make(map[uint64]bool)
	for _, r := range business {
		seen[r.TargetID] = true
	}
	bizTotal := len(seen)

	suffix := ""
	if len(baselineFailed) > 0 {
		suffix += " 基线异常: " + joinNames(baselineFailed)
	}

	switch {
	case hasBaseline && !baselineOK:
		return "PLAYER_NET_DOWN", "玩家自身网络异常，无法访问外网"

	case len(bizFailed) == bizTotal && bizTotal > 0:
		return "OUR_API_DOWN", "我方 API 全部不可达，疑似服务端或 IDC 路由故障"

	case len(bizFailed) > 0:
		return "PARTIAL_FAIL", "部分模块异常: " + joinNames(bizFailed) + suffix

	case len(bizFailed) == 0:
		d := "网络层正常，进不去大概率非网络问题"
		if suffix != "" {
			d += suffix
		}
		return "ALL_OK", d

	default:
		return "INCONCLUSIVE", "数据不足，无法判定"
	}
}

func anyReachable(results []model.ProbeResult) bool {
	for _, r := range results {
		if r.Outcome == "reachable" {
			return true
		}
	}
	return false
}

func failedTargets(results []model.ProbeResult) []model.ProbeResult {
	byTarget := make(map[uint64][]model.ProbeResult)
	for _, r := range results {
		byTarget[r.TargetID] = append(byTarget[r.TargetID], r)
	}
	var failed []model.ProbeResult
	for _, attempts := range byTarget {
		anyOK := false
		for _, a := range attempts {
			if a.Outcome == "reachable" {
				anyOK = true
				break
			}
		}
		if !anyOK && len(attempts) > 0 {
			failed = append(failed, attempts[0])
		}
	}
	return failed
}

func joinNames(failed []model.ProbeResult) string {
	s := ""
	for i, f := range failed {
		if i > 0 {
			s += ", "
		}
		s += f.TargetName
	}
	return s
}
