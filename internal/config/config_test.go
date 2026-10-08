package config

import "testing"

func TestUnsafeManifestIsRejected(t *testing.T) {
	base := Config{Host: Host{ID: "h"}, Sites: []Site{{ID: "s", HostID: "h", Name: "S", Runtime: Runtime{Type: "docker", Container: "s"}}}}
	for _, mutate := range []func(*Config){
		func(c *Config) { c.Host.SSH = "-oProxyCommand=evil" },
		func(c *Config) { c.Host.CaddyAdminURL = "http://user:secret@127.0.0.1:2019" },
		func(c *Config) { c.Sites[0].Recovery.RPOhours = -1 },
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

func TestConnectionOnlyConfigurationAndSSHPorts(t *testing.T) {
	for _, target := range []string{"vps-alias", "ops@example.com", "ops@2001:db8::1", "ops@[2001:db8::1]"} {
		c := Config{Host: Host{ID: "h", SSH: target, SSHPort: 2222}}
		if err := c.Validate(); err != nil {
			t.Fatalf("valid connection rejected: %s: %v", target, err)
		}
	}
	for _, port := range []int{-1, 65536} {
		if err := (&Config{Host: Host{ID: "h", SSHPort: port}}).Validate(); err == nil {
			t.Fatal("invalid port accepted")
		}
	}
	if err := (&Config{Host: Host{ID: "h"}, Discovery: Discovery{Disabled: true}}).Validate(); err == nil {
		t.Fatal("manual mode accepted no sites")
	}
}
