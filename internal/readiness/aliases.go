package readiness

import (
	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/model"
	"strings"
)

// Combine confirmed www/apex redirects or addresses mapped to the same
// application. Related applications such as analytics remain independent.
func CombineAliases(sites []config.Site, assessments []Assessment, reports []model.Report) []Assessment {
	byDomain := map[string]config.Site{}
	byID := map[string]Assessment{}
	health := map[string]model.Report{}
	for _, s := range sites {
		byDomain[strings.ToLower(s.Proxy.Domain)] = s
	}
	for _, a := range assessments {
		byID[a.SiteID] = a
	}
	for _, r := range reports {
		health[r.SiteID] = r
	}
	merged := map[string]bool{}
	for _, s := range sites {
		target, ok := byDomain[strings.ToLower(s.Proxy.RedirectTo)]
		if !ok && strings.HasPrefix(s.Proxy.Domain, "www.") {
			candidate, found := byDomain[strings.TrimPrefix(s.Proxy.Domain, "www.")]
			if found && s.Runtime.Container != "" && s.Runtime.Type == candidate.Runtime.Type && s.Runtime.Container == candidate.Runtime.Container {
				target, ok = candidate, true
			}
		}
		if !ok || s.Proxy.Domain == target.Proxy.Domain || strings.TrimPrefix(s.Proxy.Domain, "www.") != strings.TrimPrefix(target.Proxy.Domain, "www.") {
			continue
		}
		a, exists := byID[target.ID]
		if !exists {
			continue
		}
		a.Name = strings.TrimPrefix(target.Proxy.Domain, "www.")
		alias := s.Proxy.Domain
		if alias == a.Name {
			alias = target.Proxy.Domain
		}
		a.Aliases = append(a.Aliases, alias)
		state := model.Pass
		evidence := "Additional public address responds"
		r, observed := health[s.ID]
		if !observed || !model.Fresh(r.ObservedAt, a.ObservedAt, model.DefaultMaxAge) {
			state = model.Unknown
			evidence = "Fresh redirect observation unavailable"
		} else {
			found := false
			for _, c := range r.Checks {
				if c.Layer == "public_http" || c.Layer == "tls" {
					found = true
					if c.State != model.Pass && c.State != model.Skip && addressSeverity(c.State) > addressSeverity(state) {
						state = c.State
						evidence = c.Summary
					}
				}
			}
			if !found {
				state = model.Unknown
				evidence = "Redirect checks unavailable"
			}
		}
		a.Rules = append(a.Rules, Rule{ID: "address:" + s.ID, Category: "Availability", Title: "Address check: " + s.Proxy.Domain, State: state, Evidence: evidence, Investigation: "Check this address independently in Health log."})
		if state == model.Unknown {
			a.Provisional = true
		}
		byID[target.ID] = a
		merged[s.ID] = true
	}
	out := []Assessment{}
	for _, a := range assessments {
		if !merged[a.SiteID] {
			out = append(out, byID[a.SiteID])
		}
	}
	return out
}

func addressSeverity(state model.State) int {
	switch state {
	case model.Fail:
		return 3
	case model.Warn:
		return 2
	case model.Unknown:
		return 1
	}
	return 0
}
