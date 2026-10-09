package config

import "testing"

func TestHistoryPolicyDefaultsAndLimits(t *testing.T) {
	d := (History{}).Defaults()
	if d.MetricsDays != 30 || d.MetricDetail != "tiered" || d.RetentionDays != 7 || d.EvidenceDays != 365 || d.MaxDBMB != 256 {
		t.Fatal(d)
	}
	if full := (History{MetricDetail: "full"}).Defaults(); full.MetricsDays != 2 {
		t.Fatal(full)
	}
	for _, h := range []History{{MetricDetail: "invalid"}, {MetricsDays: -1}, {RetentionDays: 3651}, {EvidenceDays: -1}, {MaxDBMB: 15}, {MaxDBMB: 65537}} {
		if h.Validate() == nil {
			t.Fatal("unsafe history policy accepted", h)
		}
	}
}
