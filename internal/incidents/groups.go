package incidents

import (
	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/model"
	"sort"
)

// GroupReports correlates only observations of the same dependency. Unreadable
// configuration is unknown; confirmed proxy runtime failure can suppress site incidents.
func GroupReports(c config.Config, reports []model.Report, source string) []model.Report {
	type observation struct {
		report model.Report
		state  model.State
		layer  string
	}
	groups := map[string][]observation{}
	for _, r := range reports {
		seen := map[string]bool{}
		for _, check := range r.Checks {
			dependency := ""
			if source == "watcher" && check.Layer == "tcp" {
				dependency = "shared_public_path"
			}
			if source != "watcher" {
				if check.Layer == "proxy" {
					dependency = check.Dependency
				}
				if check.Layer == "caddy" {
					dependency = "caddy"
				}
			}
			if dependency == "" || seen[dependency] {
				continue
			}
			seen[dependency] = true
			state := check.State
			if source == "watcher" && state == model.Fail {
				dns := false
				for _, x := range r.Checks {
					dns = dns || x.Layer == "dns" && x.State == model.Pass
				}
				if !dns {
					state = model.Unknown
				}
			}
			groups[dependency] = append(groups[dependency], observation{r, state, check.Layer})
		}
	}
	keys := []string{}
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := []model.Report{}
	suppressed := map[string]bool{}
	for _, dependency := range keys {
		group := groups[dependency]
		if len(group) < 2 {
			continue
		}
		allFail, allPass := true, true
		ids := []string{}
		latest := group[0].report.ObservedAt
		for _, x := range group {
			ids = append(ids, x.report.SiteID)
			allFail = allFail && x.state == model.Fail
			allPass = allPass && x.state == model.Pass
			if x.report.ObservedAt.After(latest) {
				latest = x.report.ObservedAt
			}
		}
		if source == "watcher" && len(group) != len(c.Sites) {
			allFail, allPass = false, false
		}
		state := model.Unknown
		if allFail {
			state = model.Fail
		} else if allPass {
			state = model.Pass
		}
		sort.Strings(ids)
		key := "dependency:" + dependency
		joint := model.Report{HostID: c.Host.ID, SiteID: key, Name: "Shared " + dependency, ObservedAt: latest, Vantage: source, Runtime: model.Runtime{HostID: c.Host.ID, SiteID: key, Type: "dependency", Status: string(state)}, Dependency: dependency, AffectedSiteIDs: ids, Checks: []model.Check{{HostID: c.Host.ID, SiteID: key, Layer: group[0].layer, Dependency: dependency, State: state, Summary: "Shared dependency observed across properties"}}}
		joint.Overall = model.Overall(joint.Checks)
		if allFail {
			confidence, finding := "high", "Shared reverse proxy failure"
			if source == "watcher" {
				confidence, finding = "medium", "Multiple properties fail on the shared public path"
			}
			joint.Diagnoses = []model.Diagnosis{{HostID: c.Host.ID, SiteID: key, Finding: finding, Confidence: confidence, Layer: dependency, Evidence: ids, Investigation: "Inspect the shared dependency and observer network; correlation alone does not prove the root cause."}}
			for _, id := range ids {
				suppressed[id] = true
			}
		}
		out = append(out, joint)
	}
	for _, r := range reports {
		if suppressed[r.SiteID] {
			r.Overall = "unknown"
		}
		out = append(out, r)
	}
	return out
}
