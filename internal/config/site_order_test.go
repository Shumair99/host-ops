package config

import "testing"

func TestDomainOrderPairsWWWWithBaseDespiteUnrelatedIDsAndCustomNames(t *testing.T) {
	sites := []Site{
		{ID: "first", Name: "www.z.example", Proxy: Proxy{Domain: "www.z.example"}},
		{ID: "second", Name: "www.a.example", Proxy: Proxy{Domain: "WWW.A.EXAMPLE."}},
		{ID: "third", Name: "Brand", Proxy: Proxy{Domain: "z.example"}},
		{ID: "fourth", Name: "a.example", Proxy: Proxy{Domain: "a.example"}},
	}
	got := OrderedSites(sites)
	for i, want := range []string{"fourth", "second", "third", "first"} {
		if got[i].ID != want {
			t.Fatalf("order %d: got %s want %s", i, got[i].ID, want)
		}
	}
	if sites[0].ID != "first" {
		t.Fatal("ordering mutated the original site settings")
	}
}
