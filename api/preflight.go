package api

import (
	"github.com/Shumair99/host-ops/internal/platform"
	"github.com/Shumair99/host-ops/internal/preflight"
	"net/http"
)

func (s *Server) preflight(w http.ResponseWriter, r *http.Request) {
	reports, err := s.reports(r)
	if err != nil {
		Error(w, 500, "Preflight evidence unavailable")
		return
	}
	report, err := preflight.Assess(r.Context(), s.Config, r.PathValue("target"), reports, s.Store, platform.Command{Host: s.Config.Host})
	if err != nil {
		Error(w, 400, "Target or preflight evidence unavailable")
		return
	}
	JSON(w, 200, report)
}
func (s *Server) impact(w http.ResponseWriter, r *http.Request) {
	report, err := preflight.Dependency(s.Config, r.PathValue("resource"))
	if err != nil {
		Error(w, 404, err.Error())
		return
	}
	JSON(w, 200, report)
}
