package diagnostics

import (
	"encoding/json"
	"github.com/Shumair99/host-ops/internal/model"
	"github.com/Shumair99/host-ops/internal/storage"
	"time"
)

func Correlate(before, after model.Report, deployments []storage.Record) []model.Diagnosis {
	if after.Overall != "critical" || before.Overall != "healthy" {
		return nil
	}
	out := []model.Diagnosis{}
	if before.Runtime.ImageID != "" && after.Runtime.ImageID != "" && before.Runtime.ImageID != after.Runtime.ImageID {
		out = append(out, model.Diagnosis{HostID: after.HostID, SiteID: after.SiteID, Finding: "Health regression coincides with a runtime image change", Confidence: "medium", Layer: "deployment", Evidence: []string{"Previously healthy image: " + before.Runtime.Image, "Current image: " + after.Runtime.Image}, Investigation: "Compare deployment and application evidence. Temporal association does not prove causation."})
	}
	for _, record := range deployments {
		elapsed := after.ObservedAt.Sub(record.ObservedAt)
		if elapsed < 0 || elapsed > 5*time.Minute {
			continue
		}
		var d struct {
			Commit string `json:"commit"`
			Status string `json:"status"`
		}
		if json.Unmarshal(record.Data, &d) != nil || d.Status != "completed" {
			continue
		}
		out = append(out, model.Diagnosis{HostID: after.HostID, SiteID: after.SiteID, Finding: "Health regression follows a recent deployment notification", Confidence: "medium", Layer: "deployment", Evidence: []string{"Reported deployment commit: " + d.Commit, "Site was healthy before this failed observation", "Deployment occurred within five minutes of the regression"}, Investigation: "Verify the commit-to-image mapping, compare metrics and inspect failure logs. This is correlation, not proven causation."})
		break
	}
	return out
}
