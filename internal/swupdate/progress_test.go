package swupdate

import "testing"

func TestParseProgressLine(t *testing.T) {
	var p Progress
	if changed, _ := parseProgressLine("[ 1 / 3 ] rootfs.ext4 [=====     ] 45 %", &p); !changed {
		t.Fatal("expected change")
	}
	if p.StepPercent != 45 || p.CurrentStep != 1 || p.TotalSteps != 3 || p.Status != StatusRun {
		t.Fatalf("unexpected progress: %+v", p)
	}
	if _, out := parseProgressLine("SWUpdate was successful !", &p); out != outcomeSuccess {
		t.Fatalf("outcome = %v, want success", out)
	}
	if _, out := parseProgressLine("Failure", &p); out != outcomeFailure || p.Status != StatusFailure {
		t.Fatalf("outcome = %v status=%v, want failure", out, p.Status)
	}
	if changed, _ := parseProgressLine("   ", &p); changed {
		t.Fatal("blank line must be ignored")
	}
}
