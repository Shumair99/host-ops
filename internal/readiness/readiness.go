package readiness

import (
	"context"
	"encoding/json"
	"time"

	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/model"
	"github.com/Shumair99/host-ops/internal/recovery"
	securityposture "github.com/Shumair99/host-ops/internal/security"
	"github.com/Shumair99/host-ops/internal/storage"
)

type Rule struct {
	ID            string      `json:"id"`
	Category      string      `json:"category"`
	Title         string      `json:"title"`
	State         model.State `json:"state"`
	Points        int         `json:"points"`
	MaxPoints     int         `json:"max_points"`
	Evidence      string      `json:"evidence"`
	Investigation string      `json:"investigation"`
	Priority      int         `json:"priority"`
}
type Assessment struct {
	HostID      string    `json:"host_id"`
	SiteID      string    `json:"site_id"`
	Name        string    `json:"name"`
	ObservedAt  time.Time `json:"observed_at"`
	Score       int       `json:"score"`
	MaxScore    int       `json:"max_score"`
	Provisional bool      `json:"provisional"`
	Rules       []Rule    `json:"rules"`
	Aliases     []string  `json:"aliases,omitempty"`
}
type Evidence struct{ External, Metrics, History, Backup, Restore bool }

func Evaluate(site config.Site, r model.Report, ev Evidence, now time.Time) Assessment {
	a := Assessment{HostID: site.HostID, SiteID: site.ID, Name: site.Name, ObservedAt: now.UTC(), Rules: []Rule{}}
	states := map[string]model.Check{}
	fresh := model.Fresh(r.ObservedAt, now, model.DefaultMaxAge)
	for _, check := range r.Checks {
		states[check.Layer] = check
	}
	add := func(id, category, title string, state model.State, max int, evidence, investigation string, priority int) {
		points := 0
		if state == model.Pass {
			points = max
		}
		if state == model.Unknown {
			a.Provisional = true
		}
		a.Score += points
		if state != model.Skip {
			a.MaxScore += max
		}
		a.Rules = append(a.Rules, Rule{ID: id, Category: category, Title: title, State: state, Points: points, MaxPoints: max, Evidence: evidence, Investigation: investigation, Priority: priority})
	}
	check := func(id, category, title, layer string, max int) {
		c, ok := states[layer]
		if !ok || !fresh {
			c.State = model.Unknown
			c.Summary = "Fresh observation unavailable"
		}
		add(id, category, title, c.State, max, c.Summary, "Inspect this health layer and its supporting evidence.", 1)
	}
	check("public_http", "Availability", "Public website responds", "public_http", 10)
	check("proxy", "Availability", "Expected proxy route loaded", "proxy_route", 5)
	check("runtime", "Availability", "Application runtime available", "runtime", 5)
	known := func(value bool) model.State {
		if value {
			return model.Pass
		}
		return model.Unknown
	}
	proxyServed := states["runtime"].State == model.Skip
	runtimeRule := func(state model.State) model.State {
		if proxyServed {
			return model.Skip
		}
		return state
	}
	dockerRule := func(state model.State) model.State {
		if proxyServed || site.Runtime.Type != "docker" && site.Runtime.Type != "unresolved" && site.Runtime.Type != "external" && site.Runtime.Type != "" {
			return model.Skip
		}
		return state
	}
	add("revision", "Deployment safety", "Running revision labelled", runtimeRule(known(r.Runtime.Commit != "" && fresh)), 5, "OCI revision label: "+r.Runtime.Commit, "Label images with the source commit during existing builds.", 2)
	add("image", "Deployment safety", "Runtime image traceable", dockerRule(known(r.Runtime.ImageID != "" && fresh)), 5, r.Runtime.Image, "Keep versioned image references in the deployment workflow.", 2)
	restart := model.Unknown
	if fresh && r.Runtime.Status != "" {
		restart = model.Fail
		if r.Runtime.RestartPolicy == "always" || r.Runtime.RestartPolicy == "unless-stopped" || site.Runtime.Type == "systemd" && r.Runtime.RestartPolicy != "no" {
			restart = model.Pass
		}
	}
	add("restart", "Deployment safety", "Restart policy configured", runtimeRule(restart), 5, r.Runtime.RestartPolicy, "Review automatic startup and recovery behaviour.", 1)
	add("backup", "Recovery", "Recent backup evidence", known(ev.Backup), 10, "Backup freshness must be verified separately from restore capability", "Record a successful backup within the configured RPO.", 1)
	add("restore", "Recovery", "Recent restore verification", known(ev.Restore), 10, "A backup is not proof of a successful restore", "Record an isolated restoration and health verification.", 1)
	security := func(value bool) model.State {
		if !fresh || site.Runtime.Type != "docker" || r.Runtime.Status == "" || !r.Runtime.PostureObserved {
			return model.Unknown
		}
		if value {
			return model.Pass
		}
		return model.Fail
	}
	permissionState, permissionEvidence := securityposture.Permissions(r.Runtime)
	if !fresh {
		permissionState = model.Unknown
	}
	add("user", "Security", "Runtime processes run without container root", dockerRule(permissionState), 5, permissionEvidence, "Use a supported non-root runtime after checking required files and ports. Supervisor roles do not exempt root processes.", 2)
	add("privileged", "Security", "Container not privileged", dockerRule(security(!r.Runtime.Privileged)), 5, "Docker privileged flag observed", "Remove unnecessary privileged execution after reviewing dependencies.", 1)
	add("memory_limit", "Security", "Memory limit configured", dockerRule(security(r.Runtime.MemoryLimit > 0)), 5, "Docker memory limit observed", "Set a suitable memory limit through the existing Compose configuration.", 2)
	check("tls", "Security", "TLS verified", "tls", 5)
	check("local_http", "Observability", "Local application health", "local_http", 5)
	add("external", "Observability", "Independent watcher evidence", known(ev.External), 5, "Fresh externally reported health sample required", "Run the watcher on a separate machine and forward observations.", 2)
	add("metrics", "Observability", "Resource metrics retained", runtimeRule(known(ev.Metrics)), 5, "Stored resource samples required", "Run periodic collection and verify that metrics are available.", 2)
	add("history", "Observability", "Health history retained", known(ev.History), 5, "At least two stored observations required", "Retain periodic health history rather than only current state.", 2)
	return a
}

func Assess(ctx context.Context, c config.Config, site config.Site, r model.Report, store *storage.Store) (Assessment, error) {
	ev := Evidence{}
	query := func(kind string, since time.Time, limit int) ([]storage.Record, error) {
		return store.Query(ctx, c.Host.ID, site.ID, kind, since, limit)
	}
	now := time.Now()
	external, err := query("external_health", now.Add(-3*time.Minute), 1)
	if err != nil {
		return Assessment{}, err
	}
	ev.External = len(external) > 0
	m, err := query("metrics", now.Add(-3*time.Minute), 1)
	if err != nil {
		return Assessment{}, err
	}
	if len(m) > 0 {
		var sample struct {
			Values map[string]float64 `json:"values"`
		}
		if json.Unmarshal(m[0].Data, &sample) == nil {
			ev.Metrics = len(sample.Values) > 0
		}
	}
	h, err := query("health", time.Time{}, 2)
	if err != nil {
		return Assessment{}, err
	}
	ev.History = len(h) >= 2
	backupWindow := 24 * time.Hour
	if site.Recovery.RPOhours > 0 {
		backupWindow = time.Duration(site.Recovery.RPOhours) * time.Hour
	}
	b, err := query("backup", now.Add(-backupWindow), 1)
	if err != nil {
		return Assessment{}, err
	}
	if len(b) > 0 {
		var data recovery.Backup
		json.Unmarshal(b[0].Data, &data)
		ev.Backup = data.Status == "succeeded" && !data.CompletedAt.IsZero() && now.Sub(data.CompletedAt) <= backupWindow
	}
	restores, err := query("restore", now.Add(-30*24*time.Hour), 1)
	if err != nil {
		return Assessment{}, err
	}
	if len(restores) > 0 {
		var data recovery.Restore
		json.Unmarshal(restores[0].Data, &data)
		ev.Restore = data.Status == "verified" && data.HealthVerified && data.IsolationVerified && !data.CompletedAt.IsZero() && now.Sub(data.CompletedAt) <= 30*24*time.Hour
	}
	if recoveryEvidence, e := recovery.Assess(ctx, c, site, store); e != nil {
		return Assessment{}, e
	} else {
		ev.Backup = recoveryEvidence.RPOstate == model.Pass
		ev.Restore = recoveryEvidence.RestoreState == model.Pass
	}
	return Evaluate(site, r, ev, now), nil
}
