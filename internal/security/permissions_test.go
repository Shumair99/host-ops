package security

import (
	"github.com/Shumair99/host-ops/internal/model"
	"testing"
)

func TestPermissionsUsesObservedWorkersRatherThanConfiguredDefaults(t *testing.T) {
	tests := []struct {
		name     string
		user     string
		complete bool
		items    []model.ProcessIdentity
		want     model.State
	}{
		{"postgres drops root", "", true, []model.ProcessIdentity{{PID: 10, UID: 70, Name: "postgres"}}, model.Pass},
		{"nginx separation retains root warning", "", true, []model.ProcessIdentity{{PID: 10, UID: 0, Name: "nginx", Role: "nginx-master"}, {PID: 11, ParentPID: 10, UID: 101, Name: "nginx", Role: "nginx-worker"}}, model.Warn},
		{"non-root nginx supervisor and workers", "101", true, []model.ProcessIdentity{{PID: 10, UID: 101, Name: "nginx", Role: "nginx-master"}, {PID: 11, ParentPID: 10, UID: 101, Name: "nginx", Role: "nginx-worker"}}, model.Pass},
		{"root worker", "", true, []model.ProcessIdentity{{PID: 10, UID: 0, Name: "nginx", Role: "nginx-master"}, {PID: 11, ParentPID: 10, UID: 0, Name: "nginx", Role: "nginx-worker"}}, model.Warn},
		{"root application", "", true, []model.ProcessIdentity{{PID: 10, UID: 0, Name: "node"}}, model.Warn},
		{"incomplete root worker", "1000", false, []model.ProcessIdentity{{PID: 10, UID: 0, Name: "node"}}, model.Unknown},
		{"configured root without processes", "root:root", false, nil, model.Unknown},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			state, evidence := Permissions(model.Runtime{User: tc.user, Processes: model.ProcessSnapshot{Observed: len(tc.items) > 0, Complete: tc.complete, Items: tc.items}})
			if state != tc.want || evidence == "" {
				t.Fatal(state, evidence)
			}
		})
	}
}
