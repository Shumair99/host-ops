package changes

import "testing"

func TestSnapshotDistinguishesMissingObservations(t *testing.T) {
	before := Snapshot{Values: map[string]string{"runtime.image": "a", "dns": "1.2.3.4"}}
	after := Snapshot{HostID: "h", SiteID: "s", Values: map[string]string{"runtime.image": "b"}}
	diff := Between(before, after)
	if len(diff) != 2 || diff[0].Field != "dns" || diff[0].Type != "observation_availability" || diff[1].Type != "observed_change" {
		t.Fatal(diff)
	}
}
