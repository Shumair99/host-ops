package readiness

import (
	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/model"
	"testing"
	"time"
)

func TestUnknownEvidenceCannotEarnReadinessPoints(t *testing.T) {
	now := time.Now()
	site := config.Site{ID: "s", HostID: "h", Runtime: config.Runtime{Type: "docker"}}
	a := Evaluate(site, model.Report{}, Evidence{}, now)
	if a.Score != 0 || !a.Provisional {
		t.Fatal(a)
	}
	r := model.Report{ObservedAt: now, Runtime: model.Runtime{PostureObserved: true, Status: "running", User: "1000", MemoryLimit: 1024, RestartPolicy: "unless-stopped", ImageID: "id", Image: "site:v1", Commit: "abcdef1"}, Checks: []model.Check{{Layer: "runtime", State: model.Pass}, {Layer: "local_http", State: model.Pass}, {Layer: "proxy_route", State: model.Pass}, {Layer: "public_http", State: model.Pass}, {Layer: "tls", State: model.Pass}}}
	r.Runtime.Processes = model.ProcessSnapshot{Observed: true, Complete: true, Items: []model.ProcessIdentity{{PID: 1, UID: 1000, Name: "app"}}}
	a = Evaluate(site, r, Evidence{External: true, Metrics: true, History: true, Backup: true}, now)
	if a.Score != 85 {
		t.Fatal("restore should remain unverified", a.Score)
	}
	r.ObservedAt = now.Add(-time.Hour)
	if Evaluate(site, r, Evidence{}, now).Score != 0 {
		t.Fatal("stale runtime earned points")
	}
}

func TestRootWithGroupCannotPassUserRule(t *testing.T) {
	now := time.Now()
	site := config.Site{ID: "s", HostID: "h", Runtime: config.Runtime{Type: "docker"}}
	for _, user := range []string{"root:root", "0:1000"} {
		a := Evaluate(site, model.Report{ObservedAt: now, Runtime: model.Runtime{PostureObserved: true, Status: "running", User: user}}, Evidence{}, now)
		for _, rule := range a.Rules {
			if rule.ID == "user" && rule.State != model.Unknown {
				t.Fatal(rule)
			}
		}
	}
}

func TestEveryRuntimeKeepsTheSameNonRootReadinessCriterion(t *testing.T) {
	now := time.Now()
	site := config.Site{ID: "s", HostID: "h", Runtime: config.Runtime{Type: "docker"}}
	cases := []struct {
		name      string
		user      string
		complete  bool
		processes []model.ProcessIdentity
		state     model.State
		points    int
	}{
		{"root nginx supervisor", "", true, []model.ProcessIdentity{{PID: 1, UID: 0, Name: "nginx", Role: "nginx-master"}, {PID: 2, ParentPID: 1, UID: 101, Name: "nginx", Role: "nginx-worker"}}, model.Warn, 0},
		{"non-root nginx", "101", true, []model.ProcessIdentity{{PID: 1, UID: 101, Name: "nginx", Role: "nginx-master"}, {PID: 2, ParentPID: 1, UID: 101, Name: "nginx", Role: "nginx-worker"}}, model.Pass, 5},
		{"database dropped startup permissions", "", true, []model.ProcessIdentity{{PID: 1, UID: 999, Name: "postgres"}}, model.Pass, 5},
		{"root application", "", true, []model.ProcessIdentity{{PID: 1, UID: 0, Name: "java"}}, model.Warn, 0},
		{"incomplete observation", "1000", false, []model.ProcessIdentity{{PID: 1, UID: 1000, Name: "node"}}, model.Unknown, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := model.Report{ObservedAt: now, Runtime: model.Runtime{PostureObserved: true, Status: "running", User: tc.user, Processes: model.ProcessSnapshot{Observed: true, Complete: tc.complete, Items: tc.processes}}}
			a := Evaluate(site, r, Evidence{}, now)
			if a.MaxScore != 95 {
				t.Fatalf("runtime pattern changed applicable score: %d", a.MaxScore)
			}
			for _, rule := range a.Rules {
				if rule.ID == "user" {
					if rule.State != tc.state || rule.Points != tc.points || rule.MaxPoints != 5 {
						t.Fatalf("same criterion must apply to every runtime: %+v", rule)
					}
					return
				}
			}
			t.Fatal("non-root criterion missing")
		})
	}
}

func TestProxyServedEndpointHasNoInventedContainerRequirements(t *testing.T) {
	now := time.Now()
	r := model.Report{ObservedAt: now, Checks: []model.Check{
		{Layer: "runtime", State: model.Skip}, {Layer: "local_http", State: model.Skip},
		{Layer: "public_http", State: model.Pass}, {Layer: "proxy_route", State: model.Pass}, {Layer: "tls", State: model.Pass},
	}}
	a := Evaluate(config.Site{Runtime: config.Runtime{Type: "unresolved"}}, r, Evidence{History: true}, now)
	if a.Score != 25 || a.MaxScore != 50 || !a.Provisional {
		t.Fatal("redirect lost availability evidence or was penalized for a nonexistent container", a)
	}
	for _, rule := range a.Rules {
		if rule.ID == "runtime" || rule.ID == "revision" || rule.ID == "image" || rule.ID == "restart" || rule.ID == "user" || rule.ID == "privileged" || rule.ID == "memory_limit" || rule.ID == "metrics" {
			if rule.State != model.Skip || rule.Points != 0 {
				t.Fatal("proxy-only endpoint earned or lost container points", rule)
			}
		}
	}
}

func TestFreshStatusWithoutPostureCannotPassContainerHardening(t *testing.T) {
	now := time.Now()
	a := Evaluate(config.Site{Runtime: config.Runtime{Type: "docker"}}, model.Report{ObservedAt: now, Runtime: model.Runtime{Status: "running"}}, Evidence{}, now)
	for _, rule := range a.Rules {
		if rule.ID == "user" || rule.ID == "privileged" || rule.ID == "memory_limit" {
			if rule.State != model.Unknown || rule.Points != 0 {
				t.Fatal("unobserved hardening fields were evaluated", rule)
			}
		}
	}
}
