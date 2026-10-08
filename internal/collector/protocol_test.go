package collector

import (
	"bufio"
	"context"
	"strings"
	"testing"
)

func TestProtocolRejectsWrongHostAndVersion(t *testing.T) {
	for _, frame := range []string{`{"version":99,"host_id":"h","kind":"hello"}`, `{"version":1,"host_id":"another","kind":"hello"}`} {
		session := Session{scanner: bufio.NewScanner(strings.NewReader(frame)), host: "h"}
		if _, err := session.Next(); err == nil {
			t.Fatal("protocol mismatch accepted")
		}
	}
	if err := Serve(context.Background(), strings.NewReader(`{"version":999,"host":{"id":"h"}}`), &strings.Builder{}); err == nil {
		t.Fatal("invalid collector request accepted")
	}
}
