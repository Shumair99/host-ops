package platform

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestQuoteTreatsShellMetacharactersAsLiteralArguments(t *testing.T) {
	for _, tc := range []struct{ in, want string }{{"", "''"}, {"a'b", "'a'\"'\"'b'"}, {"$(touch /tmp/no); `whoami`\n--option", "'$(touch /tmp/no); `whoami`\n--option'"}} {
		if got := Quote(tc.in); got != tc.want {
			t.Fatal(got, tc.want)
		}
	}
}

func TestInspectionOutputAndErrorsRemainBounded(t *testing.T) {
	for _, mode := range []string{"large", "error"} {
		data, err := (Command{}).Run(context.Background(), os.Args[0], "-test.run=TestCommandChild", "--", "hostops-child", mode)
		if err == nil || len(data) != 0 || strings.Contains(err.Error(), "private-secret") {
			t.Fatal(mode, len(data), err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (Command{}).Run(ctx, os.Args[0]); err == nil {
		t.Fatal("cancelled command succeeded")
	}
}

func TestCommandChild(t *testing.T) {
	for i, arg := range os.Args {
		if arg != "hostops-child" || i+1 >= len(os.Args) {
			continue
		}
		if os.Args[i+1] == "large" {
			fmt.Print(strings.Repeat("x", 5*1024*1024))
			os.Exit(0)
		}
		fmt.Fprint(os.Stderr, "private-secret")
		os.Exit(1)
	}
}
