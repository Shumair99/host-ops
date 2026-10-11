package health

import (
	"context"
	"crypto/x509"
	"errors"
	"net"
	"syscall"
	"time"
)

var (
	errRedirectLimit       = errors.New("redirect limit")
	errRedirectCredentials = errors.New("redirect credentials forbidden")
	errHTTPSDowngrade      = errors.New("HTTPS downgrade forbidden")
)

// Never expose err.Error(): HTTP errors can contain URLs, credentials and
// response-controlled text. Classify typed, wrapped errors instead.
func probeFailure(err error) (code, summary string) {
	var dns *net.DNSError
	var hostname x509.HostnameError
	var invalid x509.CertificateInvalidError
	var authority x509.UnknownAuthorityError
	switch {
	case errors.Is(err, errRedirectLimit):
		return "redirect_limit", "HTTP redirect limit exceeded"
	case errors.Is(err, errRedirectCredentials):
		return "redirect_credentials", "Redirect rejected: embedded credentials"
	case errors.Is(err, errHTTPSDowngrade):
		return "redirect_downgrade", "Redirect rejected: HTTPS would downgrade to HTTP"
	case errors.As(err, &dns):
		switch {
		case dns.IsNotFound:
			return "dns_not_found", "Resolver found no usable DNS address for this hostname"
		case dns.IsTimeout:
			return "dns_timeout", "DNS resolver timed out"
		default:
			return "dns_resolver_failure", "DNS resolver could not complete the lookup"
		}
	case errors.As(err, &hostname):
		return "tls_hostname_mismatch", "Certificate does not cover the requested hostname"
	case errors.As(err, &invalid):
		if invalid.Reason == x509.Expired && invalid.Cert != nil {
			if time.Now().After(invalid.Cert.NotAfter) {
				return "tls_certificate_expired", "Presented certificate has expired"
			}
			return "tls_certificate_not_yet_valid", "Presented certificate is not yet valid"
		}
		return "tls_certificate_invalid", "Certificate validation failed"
	case errors.As(err, &authority):
		return "tls_untrusted_certificate", "Certificate chain is not trusted by this machine"
	case errors.Is(err, syscall.ECONNREFUSED):
		return "connection_refused", "Endpoint refused the connection"
	case errors.Is(err, syscall.ENETUNREACH), errors.Is(err, syscall.EHOSTUNREACH):
		return "network_unreachable", "Endpoint network is unreachable from this machine"
	case errors.Is(err, context.DeadlineExceeded):
		return "probe_timeout", "Probe deadline elapsed; failing stage is unconfirmed"
	}
	var n net.Error
	if errors.As(err, &n) && n.Timeout() {
		return "probe_timeout", "Probe timed out; failing stage is unconfirmed"
	}
	return "request_failed", "Request failed; failing stage is unconfirmed"
}
