package health

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestProbeFailureClassifiesWrappedEvidenceWithoutDisclosingURLs(t *testing.T) {
	for _, tc := range []struct {
		err  error
		code string
	}{
		{&net.DNSError{IsNotFound: true, Err: "secret"}, "dns_not_found"},
		{&net.DNSError{IsTimeout: true, Err: "secret"}, "dns_timeout"},
		{&net.DNSError{IsTemporary: true, Err: "secret"}, "dns_resolver_failure"},
		{x509.HostnameError{Host: "secret", Certificate: &x509.Certificate{}}, "tls_hostname_mismatch"},
		{x509.CertificateInvalidError{Cert: &x509.Certificate{NotAfter: time.Now().Add(-time.Hour)}, Reason: x509.Expired}, "tls_certificate_expired"},
		{x509.CertificateInvalidError{Cert: &x509.Certificate{NotBefore: time.Now().Add(time.Hour), NotAfter: time.Now().Add(2 * time.Hour)}, Reason: x509.Expired}, "tls_certificate_not_yet_valid"},
		{x509.UnknownAuthorityError{Cert: &x509.Certificate{}}, "tls_untrusted_certificate"},
		{syscall.ECONNREFUSED, "connection_refused"},
		{syscall.ENETUNREACH, "network_unreachable"},
		{context.DeadlineExceeded, "probe_timeout"},
		{errRedirectLimit, "redirect_limit"},
		{errRedirectCredentials, "redirect_credentials"},
		{errHTTPSDowngrade, "redirect_downgrade"},
		{errors.New("secret unknown failure"), "request_failed"},
	} {
		err := &url.Error{Op: "Get", URL: "https://user:secret@example.com/?token=secret", Err: fmt.Errorf("wrapped: %w", tc.err)}
		code, summary := probeFailure(err)
		if code != tc.code || strings.Contains(summary, "secret") || strings.Contains(summary, "example.com") {
			t.Fatal(tc.code, code, summary)
		}
	}
}
