package api

import (
	"crypto/subtle"
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/model"
	"github.com/Shumair99/host-ops/internal/storage"
)

type Server struct {
	Config config.Config
	Store  *storage.Store
	Token  string
	MaxAge time.Duration
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
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { JSON(w, 200, map[string]string{"status": "alive"}) })
	mux.HandleFunc("GET /api/sites", func(w http.ResponseWriter, r *http.Request) { JSON(w, 200, s.Config.Sites) })
	mux.HandleFunc("GET /api/status", s.status)
	mux.HandleFunc("GET /api/sites/{site}", s.site)
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
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if r.URL.Path != "/healthz" {
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
