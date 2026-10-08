package api

import (
	"encoding/json"
	"github.com/Shumair99/host-ops/internal/recovery"
	"io"
	"net/http"
	"time"
)

func (s *Server) recovery(w http.ResponseWriter, r *http.Request) {
	sites, err := s.Config.Select(r.PathValue("site"))
	if err != nil {
		Error(w, 404, "Site not registered")
		return
	}
	out := []recovery.Assessment{}
	for _, site := range sites {
		a, err := recovery.Assess(r.Context(), s.Config, site, s.Store)
		if err != nil {
			Error(w, 500, "Recovery evidence unavailable")
			return
		}
		out = append(out, a)
	}
	JSON(w, 200, out)
}
func decodeRecord(w http.ResponseWriter, r *http.Request, value any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 16*1024)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if decoder.Decode(value) != nil {
		Error(w, 400, "Invalid evidence JSON")
		return false
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		Error(w, 400, "One JSON object required")
		return false
	}
	return true
}
func (s *Server) backup(w http.ResponseWriter, r *http.Request) {
	var b recovery.Backup
	if !decodeRecord(w, r, &b) {
		return
	}
	if err := b.Validate(time.Now()); err != nil {
		Error(w, 400, err.Error())
		return
	}
	if _, err := s.Config.Select(b.SiteID); err != nil {
		Error(w, 400, "Site not registered")
		return
	}
	b.HostID = s.Config.Host.ID
	b.Origin = "operator_report"
	b.ObservedAt = time.Now().UTC()
	created, err := s.Store.AddOnce(r.Context(), b.HostID, b.SiteID, "backup", b.SiteID+":"+b.ID+":"+b.Status, b.ObservedAt, b)
	if err != nil {
		Error(w, 500, "Backup evidence store unavailable")
		return
	}
	status := 200
	if created {
		status = 201
	}
	JSON(w, status, b)
}
func (s *Server) restore(w http.ResponseWriter, r *http.Request) {
	var v recovery.Restore
	if !decodeRecord(w, r, &v) {
		return
	}
	if err := v.Validate(time.Now()); err != nil {
		Error(w, 400, err.Error())
		return
	}
	if _, err := s.Config.Select(v.SiteID); err != nil {
		Error(w, 400, "Site not registered")
		return
	}
	v.HostID = s.Config.Host.ID
	v.Origin = "operator_report"
	v.ObservedAt = time.Now().UTC()
	v.MeasuredRTOseconds = v.CompletedAt.Sub(v.StartedAt).Seconds()
	created, err := s.Store.AddOnce(r.Context(), v.HostID, v.SiteID, "restore", v.SiteID+":"+v.ID+":"+v.Status, v.ObservedAt, v)
	if err != nil {
		Error(w, 500, "Restore evidence store unavailable")
		return
	}
	status := 200
	if created {
		status = 201
	}
	JSON(w, status, v)
}
