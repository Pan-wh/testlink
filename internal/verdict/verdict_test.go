package verdict

import (
	"strings"
	"testing"

	"testlink/internal/model"
)

func TestComputeDoesNotTreatMissingBusinessAsAllDown(t *testing.T) {
	targets := []model.Target{
		{ID: 1, Name: "Google", Role: "基线", PlayerVisible: 1, Enabled: 1},
		{ID: 2, Name: "SDK", Role: "业务", PlayerVisible: 1, Enabled: 1},
		{ID: 3, Name: "区服", Role: "业务", PlayerVisible: 1, Enabled: 1},
	}
	results := []model.ProbeResult{
		{TargetID: 1, TargetName: "Google", Role: "基线", AttemptNo: 1, Outcome: "reachable"},
		{TargetID: 2, TargetName: "SDK", Role: "业务", AttemptNo: 1, Outcome: "timeout"},
	}

	code, detail := Compute(results, targets)
	if code == "OUR_API_DOWN" {
		t.Fatalf("missing business target was misclassified as all down: %s", detail)
	}
	if code != "PARTIAL_FAIL" {
		t.Fatalf("code=%s, want PARTIAL_FAIL", code)
	}
	if !strings.Contains(detail, "业务未完成: 区服") {
		t.Fatalf("detail=%q, want missing target note", detail)
	}
}

func TestComputeAllBusinessDownOnlyWhenComplete(t *testing.T) {
	targets := []model.Target{
		{ID: 1, Name: "Google", Role: "基线", PlayerVisible: 1, Enabled: 1},
		{ID: 2, Name: "SDK", Role: "业务", PlayerVisible: 1, Enabled: 1},
		{ID: 3, Name: "区服", Role: "业务", PlayerVisible: 1, Enabled: 1},
	}
	results := []model.ProbeResult{
		{TargetID: 1, TargetName: "Google", Role: "基线", AttemptNo: 1, Outcome: "reachable"},
		{TargetID: 2, TargetName: "SDK", Role: "业务", AttemptNo: 1, Outcome: "timeout"},
		{TargetID: 3, TargetName: "区服", Role: "业务", AttemptNo: 1, Outcome: "fast_fail"},
	}

	code, _ := Compute(results, targets)
	if code != "OUR_API_DOWN" {
		t.Fatalf("code=%s, want OUR_API_DOWN", code)
	}
}

func TestComputeLatencyWarnUsesWarmAverage(t *testing.T) {
	targets := []model.Target{
		{ID: 1, Name: "Google", Role: "基线", PlayerVisible: 1, Enabled: 1},
		{ID: 2, Name: "SDK", Role: "业务", PlayerVisible: 1, Enabled: 1, LatencyWarnMS: 500},
	}
	results := []model.ProbeResult{
		{TargetID: 1, TargetName: "Google", Role: "基线", AttemptNo: 1, Outcome: "reachable", TotalMS: 50},
		{TargetID: 2, TargetName: "SDK", Role: "业务", AttemptNo: 1, Outcome: "reachable", TotalMS: 100},
		{TargetID: 2, TargetName: "SDK", Role: "业务", AttemptNo: 2, Outcome: "reachable", TotalMS: 700},
		{TargetID: 2, TargetName: "SDK", Role: "业务", AttemptNo: 3, Outcome: "reachable", TotalMS: 900},
	}

	code, detail := Compute(results, targets)
	if code != "PARTIAL_FAIL" {
		t.Fatalf("code=%s, want PARTIAL_FAIL", code)
	}
	if !strings.Contains(detail, "SDK 800ms>500ms") {
		t.Fatalf("detail=%q, want warm average slow detail", detail)
	}
}
