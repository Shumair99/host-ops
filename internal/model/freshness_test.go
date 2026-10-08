package model

import (
	"testing"
	"time"
)

func TestCurrentRejectsMissingOldAndFutureReportsWithoutMutatingEvidence(t *testing.T) {
	now := time.Now()
	for _, at := range []time.Time{{}, now.Add(-time.Hour), now.Add(time.Hour)} {
		r := Report{ObservedAt: at, Overall: "healthy", Checks: []Check{{Layer: "public_http", State: Pass}, {Layer: "local_http", State: Skip}}}
		got := Current(r, now, DefaultMaxAge)
		if got.Overall != "unknown" || got.Checks[0].State != Unknown || got.Checks[1].State != Skip || r.Checks[0].State != Pass || !got.ObservedAt.Equal(at) {
			t.Fatal(got, r)
		}
	}
}
