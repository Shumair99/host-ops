package security

import (
	"fmt"
	"github.com/Shumair99/host-ops/internal/model"
	"strings"
)

// Permissions describes current observed processes, never the security of the
// entire application. Supervisor roles explain observations without exempting
// a root process from the common least-privilege criterion.
func Permissions(r model.Runtime) (model.State, string) {
	p := r.Processes
	if !p.Observed || !p.Complete || len(p.Items) == 0 {
		configured := r.User
		if configured == "" {
			configured = "Docker default (root)"
		}
		return model.Unknown, "Configured user: " + configured + "; effective process permissions were not completely observed"
	}
	roots, workers, master := 0, 0, 0
	for _, item := range p.Items {
		if item.UID == 0 {
			roots++
			if item.Role == "nginx-master" {
				master = item.PID
			}
		}
	}
	if roots == 0 {
		return model.Pass, fmt.Sprintf("All %d observed processes run without container root", len(p.Items))
	}
	if roots == 1 && master != 0 {
		recognized := true
		for _, item := range p.Items {
			if item.PID == master {
				continue
			}
			if item.Role != "nginx-worker" || item.ParentPID != master || item.UID == 0 {
				recognized = false
				break
			}
			workers++
		}
		if recognized && workers > 0 {
			return model.Warn, fmt.Sprintf("Nginx root supervisor with %d non-root request workers. Request handling has fewer permissions, but the supervisor retains container root; a supported non-root runtime can reduce its privileges", workers)
		}
	}
	names := []string{}
	seen := map[string]bool{}
	for _, item := range p.Items {
		if item.UID == 0 && !seen[item.Name] {
			names = append(names, item.Name)
			seen[item.Name] = true
		}
	}
	return model.Warn, "Observed root processes: " + strings.Join(names, ", ") + ". This confirms elevated permissions, not a vulnerability or compromise"
}
