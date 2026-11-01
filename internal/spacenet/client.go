// Package spacenet is the only package in rosmarops that uses the network
// (CLAUDE.md invariant 1, BV-137 / CTO-13 §3). It speaks HTTPS to the single
// fixed host app.rosmarops.com and nothing else: the dialer refuses any other
// address, every redirect is rejected, there is no HTTP/3, each request is
// capped at 30 s, TLS is standard (system roots, 1.2 minimum, no pinning), and
// HTTPS_PROXY / NO_PROXY are honoured while Windows WinINET and PAC are not.
//
// Nothing here runs until the person's permission is recorded in the state;
// enforcing that is the caller's job (the connect and send paths), and the
// runtime trust test proves no connection happens without it.
package spacenet

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"
)

// Host is the only host spacenet ever contacts. It is a compile-time constant:
// no build tag or environment variable changes it (trust test §3 check 5).
const Host = "app.rosmarops.com"

// Addr is Host with the HTTPS port — the only address a direct dial may reach.
const Addr = Host + ":443"

// RequestTimeout caps every request end to end (CTO-13 §3).
const RequestTimeout = 30 * time.Second

// ErrDisallowedHost is returned when something tries to reach anything but the
// fixed host (or, under a proxy, that proxy).
var ErrDisallowedHost = errors.New("spacenet: only HTTPS to " + Host + " is allowed")

// prodBaseURL is the production scheme+host: a compile-time constant, so
// -ldflags -X cannot repoint it (CTO-14 P4). The host guarantee does not rest
// on it in any case: the dialer (dialAllowed) and the RoundTripper
// (fixedHostRoundTripper) both pin the destination to the Host constant, so even
// a repointed baseURL cannot make a production client reach another host — the
// dialer would refuse the dial and the RoundTripper would refuse the request.
const prodBaseURL = "https://" + Host

// baseURL is the scheme+host the requests are built against. In production it is
// prodBaseURL (the fixed host); tests point it at a local server. It never
// changes what a production Client() reaches (see prodBaseURL), so production is
// unaffected.
var baseURL = prodBaseURL

// URL is the absolute URL of path (which starts with "/") on the fixed host.
func URL(path string) string { return baseURL + path }

// fixedURL resolves the environment proxy for requests to the fixed host.
var fixedURL = &url.URL{Scheme: "https", Host: Host}

// proxyResolver decides the proxy for a request. In production it is
// http.ProxyFromEnvironment (HTTPS_PROXY / NO_PROXY, read once at startup);
// tests replace it to exercise the proxy branch deterministically. PAC scripts
// and the Windows WinINET proxy are never consulted.
var proxyResolver = http.ProxyFromEnvironment

// proxyAddr is the host:port of the proxy for the fixed host, or "" when none
// applies.
func proxyAddr() (string, error) {
	p, err := proxyResolver(&http.Request{URL: fixedURL})
	if err != nil {
		return "", err
	}
	if p == nil {
		return "", nil
	}
	port := p.Port()
	if port == "" {
		if p.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	return net.JoinHostPort(p.Hostname(), port), nil
}

// dialAllowed permits exactly one TCP target: the proxy when one is configured
// for the fixed host, otherwise app.rosmarops.com:443. With a proxy the only
// dial is to the proxy; the CONNECT target is still app.rosmarops.com:443 and
// TLS verifies that name, so the proxy cannot redirect the destination.
func dialAllowed(address string) error {
	pa, err := proxyAddr()
	if err != nil {
		return err
	}
	if pa != "" {
		if address == pa {
			return nil
		}
		return fmt.Errorf("%w: refused dial to %q (proxy %s)", ErrDisallowedHost, address, pa)
	}
	if address == Addr {
		return nil
	}
	return fmt.Errorf("%w: refused dial to %q", ErrDisallowedHost, address)
}

// fixedHostRoundTripper wraps the transport and refuses, before any dial, every
// request that is not HTTPS to the exact Host constant (CTO-14 P4). It closes
// the proxy hole: under a proxy the transport's CONNECT target is the request
// URL's host, which the dialer alone does not check, so a request built against
// a repointed baseURL would otherwise tunnel to another host. Host is a
// constant, so no build flag or variable can widen this.
type fixedHostRoundTripper struct{ base http.RoundTripper }

func (rt fixedHostRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL == nil || req.URL.Scheme != "https" || req.URL.Hostname() != Host {
		scheme, host := "", ""
		if req.URL != nil {
			scheme, host = req.URL.Scheme, req.URL.Host
		}
		return nil, fmt.Errorf("%w: refused request to %q (%s)", ErrDisallowedHost, host, scheme)
	}
	return rt.base.RoundTrip(req)
}

// Client is the only http.Client spacenet builds for production (see the
// package doc for the guarantees it enforces). It is constructed inside the
// connect and send paths from a Permission; nothing takes it as a parameter, so
// no caller can pass a weaker client (CTO-14 P3).
func Client() *http.Client {
	d := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	tr := &http.Transport{
		Proxy: proxyResolver,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			switch network {
			case "tcp", "tcp4", "tcp6":
			default:
				return nil, fmt.Errorf("%w: refused network %q", ErrDisallowedHost, network)
			}
			if err := dialAllowed(address); err != nil {
				return nil, err
			}
			return d.DialContext(ctx, network, address)
		},
		ForceAttemptHTTP2:     true, // h2 is fine; only HTTP/3 is excluded (no h3 transport is used)
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		MaxIdleConns:          2,
		IdleConnTimeout:       30 * time.Second,
	}
	return &http.Client{
		Transport: fixedHostRoundTripper{base: tr},
		Timeout:   RequestTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return fmt.Errorf("%w: redirects are refused", ErrDisallowedHost)
		},
	}
}
