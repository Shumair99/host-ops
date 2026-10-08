package logs

import (
	"context"
	"regexp"
	"strings"
	"time"

	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/platform"
)

var assignment = regexp.MustCompile(`(?i)(password|passwd|secret|token|api[_-]?key|authorization)(["']?\s*[:=]\s*["']?)[^\s,"'<>]+`)
var bearer = regexp.MustCompile(`(?i)bearer\s+[a-z0-9._~+/=-]+`)
var urlCredential = regexp.MustCompile(`(https?://)[^\s/@]+:[^\s/@]+@`)
var querySecret = regexp.MustCompile(`(?i)([?&](?:token|key|secret|password|api_key)=)[^&\s]+`)
var privateKey = regexp.MustCompile(`(?s)-----BEGIN [A-Z ]*PRIVATE KEY-----.*?-----END [A-Z ]*PRIVATE KEY-----`)

func Redact(raw string) string {
	raw = privateKey.ReplaceAllString(raw, "[PRIVATE KEY REDACTED]")
	raw = bearer.ReplaceAllString(raw, "Bearer [REDACTED]")
	raw = assignment.ReplaceAllString(raw, "$1$2[REDACTED]")
	raw = urlCredential.ReplaceAllString(raw, "${1}[REDACTED]@")
	raw = querySecret.ReplaceAllString(raw, "${1}[REDACTED]")
	return strings.Map(func(r rune) rune {
		if r < 32 && r != '\n' && r != '\t' && r != '\r' {
			return -1
		}
		return r
	}, raw)
}

func Read(ctx context.Context, runner platform.Runner, s config.Site) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var data []byte
	var err error
	switch s.Runtime.Type {
	case "docker":
		data, err = runner.Run(ctx, "docker", "logs", "--tail", "200", "--timestamps", "--", s.Runtime.Container)
	case "systemd":
		data, err = runner.Run(ctx, "journalctl", "--no-pager", "--lines=200", "--unit", s.Runtime.Unit)
	default:
		return "Static sites do not have an application log stream.", nil
	}
	if err != nil {
		return "", err
	}
	return Redact(string(data)), nil
}
