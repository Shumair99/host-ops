package config

import (
	"sort"
	"strings"
)

// DomainLess keeps a www endpoint beside its normal endpoint, with the normal
// endpoint first. Names and persistent IDs never determine domain adjacency.
func DomainLess(a, b string) bool {
	a = strings.ToLower(strings.TrimSuffix(a, "."))
	b = strings.ToLower(strings.TrimSuffix(b, "."))
	ka, kb := strings.TrimPrefix(a, "www."), strings.TrimPrefix(b, "www.")
	if ka != kb {
		return ka < kb
	}
	if a != b {
		return a == ka
	}
	return false
}

func OrderedSites(sites []Site) []Site {
	out := append([]Site(nil), sites...)
	key := func(s Site) string {
		if s.Proxy.Domain != "" {
			return s.Proxy.Domain
		}
		if s.Name != "" {
			return s.Name
		}
		return s.ID
	}
	sort.SliceStable(out, func(i, j int) bool { return DomainLess(key(out[i]), key(out[j])) })
	return out
}
