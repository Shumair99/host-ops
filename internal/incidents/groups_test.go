package incidents

import (
	"context"
	"encoding/json"
	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/model"
	"github.com/Shumair99/host-ops/internal/storage"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSharedProxyIncidentAndRecovery(t *testing.T) {
	store, err := storage.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	c := config.Config{Host: config.Host{ID: "h"}}
	reports := []model.Report{}
	for _, id := range []string{"a", "b", "c", "d"} {
		reports = append(reports, model.Report{HostID: "h", SiteID: id, ObservedAt: time.Now(), Overall: "critical", Checks: []model.Check{{Layer: "caddy", State: model.Fail}}})
	}
	for cycle := 0; cycle < 3; cycle++ {
		for _, r := range GroupReports(c, reports, "agent") {
			if _, err = Observe(ctx, store, r, "agent", 3, 2); err != nil {
				t.Fatal(err)
			}
		}
	}
	list, err := List(ctx, store, "h", "b")
	if err != nil || len(list) != 1 {
		t.Fatal(list, err)
	}
	var i Incident
	json.Unmarshal(list[0].Data, &i)
	if i.Dependency != "caddy" || len(i.SiteIDs) != 4 || i.Status != "open" {
		t.Fatal(i)
	}
	for j := range reports {
		reports[j].Overall = "healthy"
		reports[j].Checks[0].State = model.Pass
	}
	for cycle := 0; cycle < 2; cycle++ {
		for _, r := range GroupReports(c, reports, "agent") {
			Observe(ctx, store, r, "agent", 3, 2)
		}
	}
	list, _ = List(ctx, store, "h", "")
	if len(list) != 1 {
		t.Fatal(list)
	}
	json.Unmarshal(list[0].Data, &i)
	if i.Status != "resolved" {
		t.Fatal(i)
	}
}

func TestConcurrentObserveOpensOnlyOnce(t *testing.T) {
	store, err := storage.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var wg sync.WaitGroup
	for j := 0; j < 10; j++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := Observe(context.Background(), store, model.Report{HostID: "h", SiteID: "s", Overall: "critical", ObservedAt: time.Now()}, "agent", 3, 2); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	items, err := store.Query(context.Background(), "h", "", "incident", time.Time{}, 100)
	if err != nil || len(items) != 1 {
		t.Fatal(items, err)
	}
}

func TestCapsuleRetainsOldOpenIncidentAndRedacts(t *testing.T) {
	store, err := storage.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	at := time.Now().Add(-48 * time.Hour)
	i := Incident{ID: "INC-one", HostID: "h", SiteIDs: []string{"s"}, Status: "open", StartedAt: at, Symptoms: []model.Check{{Summary: "token=secret-value at 192.168.1.2 /home/deploy/private"}}}
	store.Add(ctx, "h", "s", "incident", at, i)
	store.Add(ctx, "h", "s", "event", at, map[string]string{"summary": "password=another-secret"})
	store.Add(ctx, "h", "other", "event", at, map[string]string{"summary": "unrelated"})
	store.Add(ctx, "h", "s", "logs", at, map[string]string{"raw": "must-not-export"})
	c, err := Export(ctx, store, "h", i.ID)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(c)
	for _, secret := range []string{"secret-value", "another-secret", "192.168.1.2", "/home/deploy/private", "unrelated", "must-not-export"} {
		if strings.Contains(string(data), secret) {
			t.Fatal("leaked", secret)
		}
	}
	if len(c.Timeline) != 1 {
		t.Fatal(c.Timeline)
	}
}

func TestOutsideObserverDoesNotClaimHostFailure(t *testing.T) {
	reports := []model.Report{}
	for _, id := range []string{"a", "b"} {
		reports = append(reports, model.Report{HostID: "h", SiteID: id, ObservedAt: time.Now(), Overall: "critical", Checks: []model.Check{{Layer: "dns", State: model.Pass}, {Layer: "tcp", State: model.Fail}}})
	}
	grouped := GroupReports(config.Config{Host: config.Host{ID: "h"}, Sites: []config.Site{{ID: "a"}, {ID: "b"}}}, reports, "watcher")
	if grouped[0].Dependency != "shared_public_path" || grouped[0].Diagnoses[0].Confidence != "medium" {
		t.Fatal(grouped)
	}
	reports[0].Checks[0].State = model.Fail
	grouped = GroupReports(config.Config{Host: config.Host{ID: "h"}, Sites: []config.Site{{ID: "a"}, {ID: "b"}}}, reports, "watcher")
	if grouped[0].Overall == "critical" {
		t.Fatal("DNS failure incorrectly grouped", grouped)
	}
}

func TestPartialWatcherBatchCannotProveSharedRecovery(t *testing.T) {
	reports := []model.Report{}
	for _, id := range []string{"a", "b"} {
		reports = append(reports, model.Report{SiteID: id, ObservedAt: time.Now(), Checks: []model.Check{{Layer: "dns", State: model.Pass}, {Layer: "tcp", State: model.Pass}}})
	}
	grouped := GroupReports(config.Config{Host: config.Host{ID: "h"}, Sites: []config.Site{{ID: "a"}, {ID: "b"}, {ID: "c"}}}, reports, "watcher")
	if grouped[0].Overall != "unknown" {
		t.Fatal("partial batch established recovery", grouped)
	}
}
