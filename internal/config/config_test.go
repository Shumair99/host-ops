package config

import "testing"

func TestUnsafeManifestIsRejected(t *testing.T) {
	base := Config{Host: Host{ID: "h"}, Sites: []Site{{ID: "s", HostID: "h", Name: "S", Runtime: Runtime{Type: "docker", Container: "s"}}}}
	for _, mutate := range []func(*Config){
		func(c *Config) { c.Host.SSH = "-oProxyCommand=evil" },
		func(c *Config) { c.Sites[0].Runtime.Container = "s; touch /tmp/pwned" },
		func(c *Config) { c.Sites[0].Health.PublicURL = "https://user:secret@example.com/" },
		func(c *Config) {
			c.Sites[0].Runtime = Runtime{Type: "static", Directory: "/srv/site", ExpectedFiles: []string{"../../etc/shadow"}}
		},
		func(c *Config) { c.Sites[0].HostID = "another-host" },
	} {
		c := base
		c.Sites = append([]Site(nil), base.Sites...)
		mutate(&c)
		if c.Validate() == nil {
			t.Fatal("unsafe or mismatched manifest passed validation")
		}
	}
}
