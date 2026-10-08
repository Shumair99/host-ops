package tui

import (
	"strings"
	"testing"

	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/model"
)

func TestIssuesConsolidatesRepeatedEvidenceAndDoesNotPassEmptyInventory(t *testing.T) {
	text := strings.Join(issueLines(Snapshot{}), "\n")
	if strings.Contains(text, "No observed exceptions") || !strings.Contains(text, "UNKNOWN") {
		t.Fatal("empty inventory appeared healthy", text)
	}
	s := Snapshot{Sites: []config.Site{{ID: "a", Proxy: config.Proxy{Domain: "example.com"}}, {ID: "b", Proxy: config.Proxy{Domain: "www.example.com"}}}}
	for _, site := range s.Sites {
		s.Reports = append(s.Reports, model.Report{SiteID: site.ID, Name: site.Proxy.Domain, Checks: []model.Check{{Layer: "runtime", State: model.Unknown, Summary: "Shared missing evidence"}, {Layer: "tls", State: model.Fail, Summary: "Certificate failure"}}})
	}
	text = strings.Join(issueLines(s), "\n")
	if strings.Count(text, "Shared missing evidence") != 1 || !strings.Contains(text, "2 domains") || strings.Index(text, "FAIL") > strings.Index(text, "UNKNOWN") {
		t.Fatal("issues were duplicated or important failures were buried", text)
	}
}
