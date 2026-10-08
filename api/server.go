package api

import (
	"crypto/subtle"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/deployments"
	"github.com/Shumair99/host-ops/internal/diagnostics"
	"github.com/Shumair99/host-ops/internal/incidents"
	"github.com/Shumair99/host-ops/internal/logs"
	"github.com/Shumair99/host-ops/internal/model"
	"github.com/Shumair99/host-ops/internal/platform"
	"github.com/Shumair99/host-ops/internal/security"
	"github.com/Shumair99/host-ops/internal/storage"
)

type Server struct {
	Config config.Config
	Store  *storage.Store
	Token  string
	MaxAge time.Duration
	WebDir string
}

func Loopback(address string) bool {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func JSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(value)
}
func Error(w http.ResponseWriter, status int, message string) {
	JSON(w, status, map[string]string{"error": message})
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	if s.WebDir != "" {
		mux.Handle("GET /", http.FileServer(http.Dir(s.WebDir)))
	}
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { JSON(w, 200, map[string]string{"status": "alive"}) })
	mux.HandleFunc("GET /api/sites", func(w http.ResponseWriter, r *http.Request) { JSON(w, 200, s.Config.Sites) })
	mux.HandleFunc("GET /api/status", s.status)
	mux.HandleFunc("GET /api/sites/{site}", s.site)
	mux.HandleFunc("GET /api/sites/{site}/readiness", s.readiness)
	mux.HandleFunc("GET /api/readiness", s.readiness)
	mux.HandleFunc("GET /api/security", s.security)
	mux.HandleFunc("GET /api/costs", s.costs)
	mux.HandleFunc("GET /api/capacity", s.capacity)
	mux.HandleFunc("GET /api/ownership", func(w http.ResponseWriter, r *http.Request) {
		JSON(w, 200, security.Ownership(s.Config, r.URL.Query().Get("resource")))
	})
	mux.HandleFunc("GET /api/history", func(w http.ResponseWriter, r *http.Request) { s.records(w, r, "health") })
	mux.HandleFunc("GET /api/metrics", func(w http.ResponseWriter, r *http.Request) {
		kind := "metrics"
		if r.URL.Query().Get("site") == "" {
			kind = "host_metrics"
		}
		s.records(w, r, kind)
	})
	mux.HandleFunc("GET /api/host", func(w http.ResponseWriter, r *http.Request) {
		records, err := s.Store.Query(r.Context(), s.Config.Host.ID, "", "host_metrics", time.Time{}, 1)
		if err != nil {
			Error(w, 500, "Host metrics unavailable")
			return
		}
		if len(records) == 0 {
			JSON(w, 200, nil)
			return
		}
		JSON(w, 200, records[0].Data)
	})
	mux.HandleFunc("GET /api/events", func(w http.ResponseWriter, r *http.Request) { s.records(w, r, "event") })
	mux.HandleFunc("GET /api/changes", func(w http.ResponseWriter, r *http.Request) { s.records(w, r, "change") })
	mux.HandleFunc("GET /api/deployments", func(w http.ResponseWriter, r *http.Request) { s.records(w, r, "deployment") })
	mux.HandleFunc("POST /api/deployments", s.deployment)
	mux.HandleFunc("GET /api/incidents", func(w http.ResponseWriter, r *http.Request) { s.records(w, r, "incident") })
	mux.HandleFunc("GET /api/external", func(w http.ResponseWriter, r *http.Request) { s.records(w, r, "external_health") })
	mux.HandleFunc("POST /api/observations", s.observations)
	mux.HandleFunc("GET /api/sites/{site}/logs", func(w http.ResponseWriter, r *http.Request) {
		sites, err := s.Config.Select(r.PathValue("site"))
		if err != nil {
			Error(w, 404, "Site not registered")
			return
		}
		text, err := logs.Read(r.Context(), platform.Command{Host: s.Config.Host}, sites[0])
		if err != nil {
			Error(w, 502, "Log stream unavailable")
			return
		}
		JSON(w, 200, map[string]string{"host_id": s.Config.Host.ID, "site_id": sites[0].ID, "text": text})
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; font-src 'self'; connect-src 'self'; img-src 'self' data:; frame-ancestors 'none'; base-uri 'self'; form-action 'self'")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			if len(s.Token) < 32 || subtle.ConstantTimeCompare([]byte(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")), []byte(s.Token)) != 1 {
				Error(w, 401, "Authentication required")
				return
			}
			if origin := r.Header.Get("Origin"); origin != "" {
				u, err := url.Parse(origin)
				if err != nil || u.Host != r.Host {
					Error(w, 403, "Origin rejected")
					return
				}
			}
		}
		mux.ServeHTTP(w, r)
	})
}

func (s *Server) reports(r *http.Request) ([]model.Report, error) {
	reports, err := s.Store.Latest(r.Context(), s.Config.Host.ID)
	maxAge := s.MaxAge
	if maxAge == 0 {
		maxAge = 3 * time.Minute
	}
	for i := range reports {
		if time.Since(reports[i].ObservedAt) > maxAge {
			reports[i].Checks = append(reports[i].Checks, model.Check{HostID: reports[i].HostID, SiteID: reports[i].SiteID, Layer: "collector", State: model.Unknown, Summary: "Observations are stale"})
			reports[i].Overall = model.Overall(reports[i].Checks)
		}
	}
	return reports, err
}
func (s *Server) status(w http.ResponseWriter, r *http.Request) {
	reports, err := s.reports(r)
	if err != nil {
		Error(w, 500, "History unavailable")
		return
	}
	JSON(w, 200, reports)
}
func (s *Server) site(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("site")
	sites, err := s.Config.Select(id)
	if err != nil {
		Error(w, 404, "Site not registered")
		return
	}
	reports, err := s.reports(r)
	if err != nil {
		Error(w, 500, "History unavailable")
		return
	}
	var report *model.Report
	for i := range reports {
		if reports[i].SiteID == id {
			report = &reports[i]
		}
	}
	JSON(w, 200, map[string]any{"site": sites[0], "report": report})
}
func (s *Server) records(w http.ResponseWriter, r *http.Request, kind string) {
	since := time.Now().Add(-24 * time.Hour)
	if raw := r.URL.Query().Get("since"); raw != "" {
		parsed, err := time.ParseDuration(raw)
		if err != nil || parsed <= 0 || parsed > 365*24*time.Hour {
			Error(w, 400, "since must be a duration between 0 and 8760h")
			return
		}
		since = time.Now().Add(-parsed)
	}
	records, err := s.Store.Query(r.Context(), s.Config.Host.ID, r.URL.Query().Get("site"), kind, since, 1000)
	if err != nil {
		Error(w, 500, "History unavailable")
		return
	}
	JSON(w, 200, records)
}

func (s *Server) deployment(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 16*1024)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var d deployments.Deployment
	if err := decoder.Decode(&d); err != nil {
		Error(w, 400, "Invalid deployment JSON")
		return
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		Error(w, 400, "Expected one JSON object")
		return
	}
	if _, err := s.Config.Select(d.SiteID); err != nil {
		Error(w, 400, "Site not registered")
		return
	}
	if d.HealthResult == "" {
		d.HealthResult = "unknown"
	}
	if err := d.Validate(); err != nil {
		Error(w, 400, err.Error())
		return
	}
	d.HostID = s.Config.Host.ID
	d.Origin = "deployment_notification"
	d.ObservedAt = time.Now().UTC()
	created, err := s.Store.AddOnce(r.Context(), d.HostID, d.SiteID, "deployment", d.SiteID+":"+d.ID+":"+d.Status, d.ObservedAt, d)
	if err != nil {
		Error(w, 500, "Deployment history unavailable")
		return
	}
	status := 200
	if created {
		status = 201
	}
	JSON(w, status, d)
}

func (s *Server) observations(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 256*1024)
	decoder := json.NewDecoder(r.Body)
	var reports []model.Report
	if decoder.Decode(&reports) != nil || len(reports) == 0 || len(reports) > len(s.Config.Sites) {
		Error(w, 400, "Invalid observation batch")
		return
	}
	seen := map[string]bool{}
	for i := range reports {
		report := &reports[i]
		sites, err := s.Config.Select(report.SiteID)
		if err != nil || seen[report.SiteID] || report.ObservedAt.After(time.Now().Add(time.Minute)) || report.ObservedAt.Before(time.Now().Add(-15*time.Minute)) {
			Error(w, 400, "Unknown duplicate or stale site observation")
			return
		}
		seen[report.SiteID] = true
		report.HostID = s.Config.Host.ID
		report.Name = sites[0].Name
		report.Vantage = "external watcher report"
		report.Runtime = model.Runtime{}
		for j := range report.Checks {
			check := &report.Checks[j]
			if check.Layer != "dns" && check.Layer != "tcp" && check.Layer != "tls" && check.Layer != "public_http" {
				Error(w, 400, "Invalid external check layer")
				return
			}
			if check.State != model.Pass && check.State != model.Fail && check.State != model.Warn && check.State != model.Unknown && check.State != model.Skip {
				Error(w, 400, "Invalid check state")
				return
			}
			if len(check.Summary) > 512 {
				Error(w, 400, "Check summary too long")
				return
			}
			check.Summary = logs.Redact(check.Summary)
			check.HostID = report.HostID
			check.SiteID = report.SiteID
		}
		report.Overall = model.Overall(report.Checks)
		report.Diagnoses = diagnostics.Diagnose(*report)
	}
	for _, report := range reports {
		if _, err := s.Store.Add(r.Context(), report.HostID, report.SiteID, "external_health", report.ObservedAt, report); err != nil {
			Error(w, 500, "Observation store unavailable")
			return
		}
		if _, err := incidents.Observe(r.Context(), s.Store, report, "watcher", 3, 2); err != nil {
			Error(w, 500, "Incident store unavailable")
			return
		}
	}
	JSON(w, 202, map[string]int{"accepted": len(reports)})
}
