package incidents

import (
	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/model"
	"testing"
	"time"
)

func TestIndependentProxiesDoNotShareOneIncident(t *testing.T) {
	reports := []model.Report{}
	for _, id := range []string{"a", "b", "c", "d"} {
		dependency := "proxy-one"
		if id == "c" || id == "d" {
			dependency = "proxy-two"
		}
		reports = append(reports, model.Report{HostID: "h", SiteID: id, ObservedAt: time.Now(), Overall: "critical", Checks: []model.Check{{Layer: "proxy", Dependency: dependency, State: model.Fail}}})
	}
	got := GroupReports(config.Config{Host: config.Host{ID: "h"}}, reports, "agent")
	if len(got) != 6 || got[0].Dependency == got[1].Dependency || len(got[0].AffectedSiteIDs) != 2 || len(got[1].AffectedSiteIDs) != 2 {
		t.Fatal(got)
	}
}
