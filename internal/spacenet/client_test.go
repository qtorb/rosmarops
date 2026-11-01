package spacenet

import (
	"crypto/tls"
	"net/http"
	"net/url"
	"reflect"
	"testing"
	"time"
)

// withProxy swaps the proxy resolver for one that returns proxyURL ("" = none)
// for the duration of the test, so the proxy branch is deterministic (the real
// http.ProxyFromEnvironment caches the environment once per process).
func withProxy(t *testing.T, proxyURL string) {
	t.Helper()
	orig := proxyResolver
	t.Cleanup(func() { proxyResolver = orig })
	proxyResolver = func(*http.Request) (*url.URL, error) {
		if proxyURL == "" {
			return nil, nil
		}
		return url.Parse(proxyURL)
	}
}

func TestFixedHostConstants(t *testing.T) {
	if Host != "app.rosmarops.com" {
		t.Errorf("Host = %q", Host)
	}
	if Addr != "app.rosmarops.com:443" {
		t.Errorf("Addr = %q", Addr)
	}
	if got := URL("/api/v1/connect/start"); got != "https://app.rosmarops.com/api/v1/connect/start" {
		t.Errorf("URL = %q", got)
	}
}

func TestClientHardening(t *testing.T) {
	c := Client()
	if c.Timeout != RequestTimeout || RequestTimeout != 30*time.Second {
		t.Errorf("timeout = %v", c.Timeout)
	}
	if c.CheckRedirect == nil || c.CheckRedirect(nil, nil) == nil {
		t.Error("redirects must be refused")
	}
	tr, ok := c.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport is %T", c.Transport)
	}
	if tr.TLSClientConfig == nil || tr.TLSClientConfig.MinVersion != tls.VersionTLS12 {
		t.Error("TLS minimum must be 1.2")
	}
	if tr.TLSClientConfig != nil && tr.TLSClientConfig.InsecureSkipVerify {
		t.Error("TLS verification must not be skipped")
	}
	if tr.TLSClientConfig != nil && tr.TLSClientConfig.RootCAs != nil {
		t.Error("TLS must use the system roots (RootCAs nil)")
	}
}

// Without a proxy, only app.rosmarops.com:443 may be dialled.
func TestDialAllowed_NoProxy(t *testing.T) {
	withProxy(t, "")
	if err := dialAllowed(Addr); err != nil {
		t.Errorf("dialAllowed(%q) = %v, want nil", Addr, err)
	}
	for _, bad := range []string{"evil.example:443", "app.rosmarops.com:80", "app.rosmarops.com:8443", "1.2.3.4:443", "app.rosmarops.com.evil.example:443"} {
		if err := dialAllowed(bad); err == nil {
			t.Errorf("dialAllowed(%q) = nil, want refusal", bad)
		}
	}
}

// With HTTPS_PROXY set, only the proxy may be dialled (the CONNECT target stays
// app.rosmarops.com:443, pinned by TLS, so it is never a direct dial).
func TestDialAllowed_Proxy(t *testing.T) {
	withProxy(t, "http://proxy.corp.example:8080")
	pa, err := proxyAddr()
	if err != nil || pa != "proxy.corp.example:8080" {
		t.Fatalf("proxyAddr = %q, %v", pa, err)
	}
	if err := dialAllowed("proxy.corp.example:8080"); err != nil {
		t.Errorf("dialAllowed(proxy) = %v, want nil", err)
	}
	// A direct dial to the fixed host is refused while the proxy is in force.
	if err := dialAllowed(Addr); err == nil {
		t.Error("dialAllowed(Addr) should be refused when a proxy is configured")
	}
	if err := dialAllowed("evil.example:443"); err == nil {
		t.Error("dialAllowed(evil) must be refused under a proxy too")
	}
}

// A proxy URL without an explicit port defaults to 443 (https) / 80 (http).
func TestProxyAddr_DefaultPort(t *testing.T) {
	withProxy(t, "https://proxy.corp.example")
	if pa, _ := proxyAddr(); pa != "proxy.corp.example:443" {
		t.Errorf("https proxy no port → %q, want :443", pa)
	}
	withProxy(t, "http://proxy.corp.example")
	if pa, _ := proxyAddr(); pa != "proxy.corp.example:80" {
		t.Errorf("http proxy no port → %q, want :80", pa)
	}
}

// Production honours the environment (HTTPS_PROXY / NO_PROXY): the default
// resolver is the standard library's, which reads it once at startup.
func TestDefaultProxyResolverIsEnvironment(t *testing.T) {
	got := reflect.ValueOf(proxyResolver).Pointer()
	want := reflect.ValueOf(http.ProxyFromEnvironment).Pointer()
	if got != want {
		t.Error("default proxyResolver must be http.ProxyFromEnvironment")
	}
}
