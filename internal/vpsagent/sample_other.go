//go:build !linux

package vpsagent

import (
	"context"
	"fmt"

	"github.com/Shumair99/host-ops/internal/metrics"
)

type Sampler struct{}

func NewSampler(string) (*Sampler, error)              { return nil, fmt.Errorf("VPS collector requires Linux") }
func (*Sampler) Sample(context.Context) metrics.Sample { return metrics.Sample{} }
