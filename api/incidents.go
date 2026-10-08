package api

import (
	"github.com/Shumair99/host-ops/internal/incidents"
	"net/http"
)

func (s *Server) incidentList(w http.ResponseWriter, r *http.Request) {
	records, err := incidents.List(r.Context(), s.Store, s.Config.Host.ID, r.URL.Query().Get("site"))
	if err != nil {
		Error(w, 500, "Incident history unavailable")
		return
	}
	JSON(w, 200, records)
}
func (s *Server) capsule(w http.ResponseWriter, r *http.Request) {
	value, err := incidents.Export(r.Context(), s.Store, s.Config.Host.ID, r.PathValue("incident"))
	if err != nil {
		Error(w, 404, "Incident evidence unavailable")
		return
	}
	JSON(w, 200, value)
}
