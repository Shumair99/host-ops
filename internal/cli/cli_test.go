package cli

import (
	"bytes"
	"context"
	"testing"
)

func TestSiteBeforeFlagsDoesNotMutateArguments(t *testing.T) {
	args := []string{"sites", "client-a", "--config", "../../examples/sites/docker.json", "--json"}
	var out, errOut bytes.Buffer
	if code := Run(context.Background(), args, &out, &errOut); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	if args[1] != "client-a" || !bytes.Contains(out.Bytes(), []byte(`"id": "client-a"`)) {
		t.Fatal("site argument lost or caller's slice mutated")
	}
}
