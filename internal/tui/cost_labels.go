package tui

import "github.com/Shumair99/host-ops/internal/console"

func usageMoney(n *float64, currency string, partial bool) string {
	value := console.Money(n, currency)
	if n != nil && partial {
		return ">= " + value
	}
	return value
}
