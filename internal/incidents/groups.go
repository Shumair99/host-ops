package incidents

import (
	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/model"
	"sort"
)

func GroupReports(c config.Config, reports []model.Report, source string) []model.Report {
	if len(reports) < 2 {
		return reports
	}
	layer, dependency := "caddy", "caddy"
	if source == "watcher" {
		layer, dependency = "tcp", "shared_public_path"
	}
	eligible := []model.Report{}
	for _, r := range reports {
		for _, check := range r.Checks {
			if check.Layer == layer {
				eligible = append(eligible, r)
				break
			}
		}
	}
	if len(eligible) < 2 {
		return reports
	}
	allFail, allPass := true, true
	ids := []string{}
	latest := eligible[0].ObservedAt
	for _, r := range eligible {
		ids = append(ids, r.SiteID)
		if r.ObservedAt.After(latest) {
			latest = r.ObservedAt
		}
		for _, check := range r.Checks {
			if check.Layer == layer {
				allFail = allFail && check.State == model.Fail
				allPass = allPass && check.State == model.Pass
			}
		}
	}
	if source == "watcher" {
		if len(eligible) != len(c.Sites) {
			allFail, allPass = false, false
		}
		for _, r := range eligible {
			dnsPass := false
			for _, check := range r.Checks {
				if check.Layer == "dns" && check.State == model.Pass {
					dnsPass = true
				}
			}
			allFail = allFail && dnsPass
		}
	}
	sort.Strings(ids)
	state := model.Unknown
	if allFail {
		state = model.Fail
	} else if allPass {
		state = model.Pass
	}
	key := "dependency:" + dependency
	joint := model.Report{HostID: c.Host.ID, SiteID: key, Name: "Shared " + dependency, ObservedAt: latest, Vantage: source, Runtime: model.Runtime{HostID: c.Host.ID, SiteID: key, Type: "dependency", Status: string(state)}, Dependency: dependency, AffectedSiteIDs: ids, Checks: []model.Check{{HostID: c.Host.ID, SiteID: key, Layer: layer, State: state, Summary: "Shared dependency observed across registered properties"}}}
	joint.Overall = model.Overall(joint.Checks)
	if allFail {
		confidence := "high"
		finding := "Shared reverse proxy failure"
		if source == "watcher" {
			confidence = "medium"
			finding = "Multiple properties fail on the shared public path"
		}
		joint.Diagnoses = []model.Diagnosis{{HostID: c.Host.ID, SiteID: key, Finding: finding, Confidence: confidence, Layer: dependency, Evidence: ids, Investigation: "Inspect the shared dependency and the observer's network before treating each site as an independent outage."}}
	}
	out := []model.Report{joint}
	suppressed := map[string]bool{}
	if allFail {
		for _, id := range ids {
			suppressed[id] = true
		}
	}
	for _, r := range reports {
		if suppressed[r.SiteID] {
			r.Overall = "unknown"
		}
		out = append(out, r)
	}
	return out
}
