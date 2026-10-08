package api

import (
	"github.com/Shumair99/host-ops/internal/capacity"
	"github.com/Shumair99/host-ops/internal/costs"
	"net/http"
)

func (s *Server) costs(w http.ResponseWriter, r *http.Request) {
	value, err := costs.Assess(r.Context(), s.Config, s.Store)
	if err != nil {
		Error(w, 500, "Cost evidence unavailable")
		return
	}
	JSON(w, 200, value)
}
func (s *Server) capacity(w http.ResponseWriter, r *http.Request) {
	value, err := capacity.Assess(r.Context(), s.Config, s.Store)
	if err != nil {
		Error(w, 500, "Capacity evidence unavailable")
		return
	}
	JSON(w, 200, value)
}
