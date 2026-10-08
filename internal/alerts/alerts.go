package alerts

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/Shumair99/host-ops/internal/incidents"
)

func Notify(ctx context.Context, endpoint string, incident incidents.Incident) error {
	client := &http.Client{Timeout: 5 * time.Second}
	return notify(ctx, endpoint, incident, client)
}

func notify(ctx context.Context, endpoint string, incident incidents.Incident, client *http.Client) error {
	if endpoint == "" {
		return nil
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil {
		return fmt.Errorf("webhook must be HTTPS without URL credentials")
	}
	data, err := json.Marshal(incident)
	if err != nil {
		return err
	}
	copyClient := *client
	copyClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	ctx, cancel := context.WithTimeout(ctx, 11*time.Second)
	defer cancel()
	key := fmt.Sprintf("%x", sha256.Sum256([]byte(incident.ID+":"+incident.Status)))
	var failure error
	for attempt := 0; attempt < 2; attempt++ {
		req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(data))
		if err != nil {
			return fmt.Errorf("invalid webhook")
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", key)
		resp, err := copyClient.Do(req)
		retry := err != nil
		failure = fmt.Errorf("webhook delivery failed")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				return nil
			}
			failure = fmt.Errorf("webhook returned HTTP %d", resp.StatusCode)
			retry = resp.StatusCode == 408 || resp.StatusCode == 429 || resp.StatusCode >= 500
		}
		if !retry || attempt == 1 || ctx.Err() != nil {
			return failure
		}
		timer := time.NewTimer(250 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return failure
		case <-timer.C:
		}
	}
	return failure
}
