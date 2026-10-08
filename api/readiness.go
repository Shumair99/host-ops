package api

import (
	"github.com/Shumair99/host-ops/internal/model"
	"github.com/Shumair99/host-ops/internal/readiness"
	"net/http"
)

func (s *Server) readiness(w http.ResponseWriter, r *http.Request) {
	selected, err := s.Config.Select(r.PathValue("site"))
	if err != nil {
		Error(w, 404, "Site not registered")
		return
	}
	reports, err := s.reports(r)
	if err != nil {
		Error(w, 500, "Health history unavailable")
		return
	}
	byID := map[string]model.Report{}
	for _, report := range reports {
		byID[report.SiteID] = report
	}
	assessments := []readiness.Assessment{}
	for _, site := range selected {
		a, err := readiness.Assess(r.Context(), s.Config, site, byID[site.ID], s.Store)
		if err != nil {
			Error(w, 500, "Readiness evidence unavailable")
			return
		}
		assessments = append(assessments, a)
	}
	if r.PathValue("site") != "" {
		JSON(w, 200, assessments[0])
	} else {
		JSON(w, 200, assessments)
	}
}
