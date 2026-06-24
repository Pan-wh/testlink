package verdict

import (
	"fmt"
	"sort"
	"strings"

	"testlink/internal/model"
)

func Compute(results []model.ProbeResult, targets []model.Target) (code, detail string) {
	expected := expectedTargets(targets, results)
	if len(expected) == 0 {
		return "INCONCLUSIVE", "数据不足，无法判定"
	}

	byTarget := make(map[uint64][]model.ProbeResult)
	for _, r := range results {
		byTarget[r.TargetID] = append(byTarget[r.TargetID], r)
	}
	for id := range byTarget {
		sort.Slice(byTarget[id], func(i, j int) bool { return byTarget[id][i].AttemptNo < byTarget[id][j].AttemptNo })
	}

	var baseline, business []targetState
	for _, t := range expected {
		state := buildState(t, byTarget[t.ID])
		if normalizeRole(t.Role) == "基线" {
			baseline = append(baseline, state)
		} else {
			business = append(business, state)
		}
	}

	baselineOK := anyOK(baseline)
	baselineMissing := missingStates(baseline)
	baselineFailed := failedStates(baseline)
	businessMissing := missingStates(business)
	businessFailed := failedStates(business)
	businessSlow := slowStates(business)
	businessComplete := len(business) > 0 && len(businessMissing) == 0

	var notes []string
	if len(baselineFailed) > 0 {
		notes = append(notes, "基线异常: "+joinStateNames(baselineFailed))
	}
	if len(baselineMissing) > 0 {
		notes = append(notes, "基线未完成: "+joinStateNames(baselineMissing))
	}
	if len(businessMissing) > 0 {
		notes = append(notes, "业务未完成: "+joinStateNames(businessMissing))
	}

	switch {
	case len(baseline) > 0 && !baselineOK && len(baselineMissing) > 0:
		return "INCONCLUSIVE", "数据不完整，无法判定。" + strings.Join(notes, "；")

	case len(baseline) > 0 && !baselineOK:
		return "PLAYER_NET_DOWN", "玩家自身网络异常，无法访问外网"

	case businessComplete && len(businessFailed) == len(business) && len(business) > 0:
		return "OUR_API_DOWN", "我方 API 全部不可达，疑似服务端或 IDC 路由故障"

	case len(businessFailed) > 0:
		d := "部分模块异常: " + joinStateNames(businessFailed)
		if len(businessSlow) > 0 {
			d += "；延迟较高: " + joinSlowNames(businessSlow)
		}
		if len(notes) > 0 {
			d += "；" + strings.Join(notes, "；")
		}
		return "PARTIAL_FAIL", d

	case len(businessSlow) > 0:
		d := "部分模块延迟较高: " + joinSlowNames(businessSlow)
		if len(notes) > 0 {
			d += "；" + strings.Join(notes, "；")
		}
		return "PARTIAL_FAIL", d

	case len(businessMissing) > 0 || len(baselineMissing) > 0:
		return "INCONCLUSIVE", "数据不完整，无法判定。" + strings.Join(notes, "；")

	default:
		d := "网络层正常，进不去大概率非网络问题"
		if len(notes) > 0 {
			d += "；" + strings.Join(notes, "；")
		}
		return "ALL_OK", d
	}
}

type targetState struct {
	Target    model.Target
	Attempts  []model.ProbeResult
	Reachable bool
	Missing   bool
	Slow      bool
	AvgMS     uint32
}

func expectedTargets(targets []model.Target, results []model.ProbeResult) []model.Target {
	if len(targets) > 0 {
		out := make([]model.Target, 0, len(targets))
		for _, t := range targets {
			if t.Enabled == 0 || t.PlayerVisible == 0 {
				continue
			}
			t.Role = normalizeRole(t.Role)
			out = append(out, t)
		}
		return out
	}

	seen := make(map[uint64]bool)
	out := make([]model.Target, 0)
	for _, r := range results {
		if seen[r.TargetID] {
			continue
		}
		seen[r.TargetID] = true
		out = append(out, model.Target{
			ID:        r.TargetID,
			Name:      r.TargetName,
			GroupName: r.GroupName,
			Role:      normalizeRole(r.Role),
		})
	}
	return out
}

func buildState(t model.Target, attempts []model.ProbeResult) targetState {
	state := targetState{Target: t, Attempts: attempts, Missing: len(attempts) == 0}
	var warmSum uint32
	var warmCount uint32
	var firstOK *model.ProbeResult

	for i := range attempts {
		a := attempts[i]
		if a.Outcome != "reachable" {
			continue
		}
		state.Reachable = true
		if firstOK == nil {
			firstOK = &attempts[i]
		}
		if a.AttemptNo > 1 {
			warmSum += uint32(a.TotalMS)
			warmCount++
		}
	}
	if warmCount > 0 {
		state.AvgMS = warmSum / warmCount
	} else if firstOK != nil {
		state.AvgMS = uint32(firstOK.TotalMS)
	}
	if state.Reachable && t.LatencyWarnMS > 0 && state.AvgMS > t.LatencyWarnMS {
		state.Slow = true
	}
	return state
}

func anyOK(states []targetState) bool {
	for _, s := range states {
		if s.Reachable {
			return true
		}
	}
	return false
}

func missingStates(states []targetState) []targetState {
	var out []targetState
	for _, s := range states {
		if s.Missing {
			out = append(out, s)
		}
	}
	return out
}

func failedStates(states []targetState) []targetState {
	var out []targetState
	for _, s := range states {
		if !s.Missing && !s.Reachable {
			out = append(out, s)
		}
	}
	return out
}

func slowStates(states []targetState) []targetState {
	var out []targetState
	for _, s := range states {
		if s.Slow {
			out = append(out, s)
		}
	}
	return out
}

func joinStateNames(states []targetState) string {
	parts := make([]string, 0, len(states))
	for _, s := range states {
		parts = append(parts, targetName(s))
	}
	return strings.Join(parts, ", ")
}

func joinSlowNames(states []targetState) string {
	parts := make([]string, 0, len(states))
	for _, s := range states {
		parts = append(parts, fmt.Sprintf("%s %dms>%dms", targetName(s), s.AvgMS, s.Target.LatencyWarnMS))
	}
	return strings.Join(parts, ", ")
}

func targetName(s targetState) string {
	if s.Target.Name != "" {
		return s.Target.Name
	}
	if len(s.Attempts) > 0 && s.Attempts[0].TargetName != "" {
		return s.Attempts[0].TargetName
	}
	return fmt.Sprintf("target-%d", s.Target.ID)
}

func normalizeRole(r string) string {
	if r == "baseline" {
		return "基线"
	}
	if r == "business" || r == "" {
		return "业务"
	}
	return r
}
