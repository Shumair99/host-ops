package config

import (
	"fmt"
)

// SaveBilling edits the latest on-disk manifest, preserving unrelated settings.
// The caller receives the validated saved configuration for immediate use.
func SaveBilling(file, hostID string, billing *Billing, currency string) (Config, error) {
	c, err := Load(file)
	if err != nil {
		return Config{}, err
	}
	if c.Host.ID != hostID {
		return Config{}, fmt.Errorf("settings now belong to another host; reopen its dashboard")
	}
	c.Host.Billing, c.Host.Currency = billing, currency
	if err = c.Validate(); err != nil {
		return Config{}, err
	}
	if err = Save(file, c); err != nil {
		return Config{}, fmt.Errorf("cannot save billing settings: %w", err)
	}
	return c, nil
}
