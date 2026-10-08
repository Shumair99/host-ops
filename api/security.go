package api

import (
	"github.com/Shumair99/host-ops/internal/platform"
	"github.com/Shumair99/host-ops/internal/security"
	"net/http"
)

func (s *Server) security(w http.ResponseWriter, r *http.Request) {
	reports, err := s.reports(r)
	if err != nil {
		Error(w, 500, "Runtime evidence unavailable")
		return
	}
	JSON(w, 200, security.Inspect(r.Context(), s.Config, reports, platform.Command{Host: s.Config.Host}))
}
