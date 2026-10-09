package config

import "testing"

func TestServiceUnitsSupportInstancesWithoutAcceptingShellSyntax(t *testing.T) {
	for _, unit := range []string{"web.service", "getty@tty1.service", "worker@customer-1.service", `worker@tenant\x2d1.service`} {
		c := Config{Host: Host{ID: "h", ServiceUnits: []string{unit}}, Sites: []Site{{ID: "s", HostID: "h", Name: "Site", Runtime: Runtime{Type: "systemd", Unit: unit}}}}
		if err := c.Validate(); err != nil {
			t.Fatalf("unit %q rejected: %v", unit, err)
		}
	}
	for _, unit := range []string{"-bad.service", "web.service;id", "web\n.service", "../../web.service", "worker@$(id).service", `worker@bad\q.service`} {
		if (&Config{Host: Host{ID: "h", ServiceUnits: []string{unit}}}).Validate() == nil {
			t.Fatalf("unsafe unit accepted: %q", unit)
		}
	}
}

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

func TestHistoryModeHasOnlyExplicitSupportedValues(t *testing.T) {
	for _, mode := range []string{"", "session", "continuous"} {
		if err := (&Config{Host: Host{ID: "h", HistoryMode: mode}}).Validate(); err != nil {
			t.Fatalf("supported history mode %q rejected: %v", mode, err)
		}
	}
	if err := (&Config{Host: Host{ID: "h", HistoryMode: "always"}}).Validate(); err == nil {
		t.Fatal("unknown history mode accepted")
	}
}

func TestBillingRejectsMixedSourcesUnsafeResourcesAndTokenNames(t *testing.T) {
	amount := 12.0
	for _, b := range []*Billing{
		{Provider: "hetzner", ResourceID: "../bad"},
		{Provider: "hostinger", ResourceID: "valid", TokenEnv: "TOKEN=value"},
		{Provider: "hetzner", ResourceID: "42", MonthlyAmount: &amount},
		{Provider: "manual"},
		{Provider: "manual", ResourceID: "42", MonthlyAmount: &amount},
	} {
		c := Config{Host: Host{ID: "h", Currency: "GBP", Billing: b}}
		if c.Validate() == nil {
			t.Fatal("invalid or ambiguous billing source accepted", b)
		}
	}
}
