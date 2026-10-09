package deployments

import (
	"math"
	"testing"
)

func validDeployment() Deployment {
	return Deployment{
		ID:           "deploy-1",
		SiteID:       "site-1",
		Commit:       "abcdef1234567",
		Status:       "completed",
		HealthResult: "healthy",
	}
}

func TestValidateDeployment(t *testing.T) {
	t.Run("valid record", func(t *testing.T) {
		if err := validDeployment().Validate(); err != nil {
			t.Fatal(err)
		}
	})

	tests := []struct {
		name   string
		mutate func(*Deployment)
	}{
		{name: "missing id", mutate: func(d *Deployment) { d.ID = "" }},
		{name: "invalid site id", mutate: func(d *Deployment) { d.SiteID = "site/../../escape" }},
		{name: "short commit", mutate: func(d *Deployment) { d.Commit = "abc" }},
		{name: "invalid status", mutate: func(d *Deployment) { d.Status = "running" }},
		{name: "invalid image", mutate: func(d *Deployment) { d.Image = "bad image" }},
		{name: "invalid branch", mutate: func(d *Deployment) { d.Branch = "branch with spaces" }},
		{name: "negative duration", mutate: func(d *Deployment) { d.DurationSeconds = -1 }},
		{name: "duration over limit", mutate: func(d *Deployment) { d.DurationSeconds = 86401 }},
		{name: "NaN duration", mutate: func(d *Deployment) { d.DurationSeconds = math.NaN() }},
		{name: "invalid health result", mutate: func(d *Deployment) { d.HealthResult = "maybe" }},
		{name: "raw failure reason", mutate: func(d *Deployment) { d.FailureReason = "database password leaked" }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d := validDeployment()
			tc.mutate(&d)
			if err := d.Validate(); err == nil {
				t.Fatal("invalid deployment accepted")
			}
		})
	}
}

func TestValidateDeploymentDurationBoundaries(t *testing.T) {
	for _, duration := range []float64{0, 86400} {
		d := validDeployment()
		d.DurationSeconds = duration
		if err := d.Validate(); err != nil {
			t.Fatalf("duration %v rejected: %v", duration, err)
		}
	}
}
