package logs

import (
	"strings"
	"testing"
)

func TestLogRedaction(t *testing.T) {
	raw := `password=hunter2 token="abc123" Authorization: Bearer aaa.bbb https://user:pwd@example.com/?api_key=secret`
	redacted := Redact(raw)
	for _, secret := range []string{"hunter2", "abc123", "aaa.bbb", "user:pwd", "api_key=secret"} {
		if strings.Contains(redacted, secret) {
			t.Fatal(redacted)
		}
	}
}
