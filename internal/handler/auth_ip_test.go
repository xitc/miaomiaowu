package handler

import (
	"net/http/httptest"
	"testing"
)

func TestGetClientIPIgnoresForwardedHeadersFromDirectClient(t *testing.T) {
	req := httptest.NewRequest("GET", "https://example.test/", nil)
	req.RemoteAddr = "198.51.100.20:4567"
	req.Header.Set("CF-Connecting-IP", "203.0.113.1")
	req.Header.Set("X-Forwarded-For", "203.0.113.2")
	req.Header.Set("X-Real-IP", "203.0.113.3")

	if got := GetClientIP(req); got != "198.51.100.20" {
		t.Fatalf("GetClientIP() = %q, want direct peer IP", got)
	}
}

func TestGetClientIPTrustsReverseProxyXRealIP(t *testing.T) {
	req := httptest.NewRequest("GET", "https://example.test/", nil)
	req.RemoteAddr = "172.18.0.1:4567"
	req.Header.Set("CF-Connecting-IP", "203.0.113.1")
	req.Header.Set("X-Forwarded-For", "203.0.113.2, 198.51.100.9")
	req.Header.Set("X-Real-IP", "2001:db8::9")

	if got := GetClientIP(req); got != "2001:db8::9" {
		t.Fatalf("GetClientIP() = %q, want X-Real-IP", got)
	}
}

func TestGetClientIPUsesRightmostForwardedIPFromReverseProxy(t *testing.T) {
	req := httptest.NewRequest("GET", "https://example.test/", nil)
	req.RemoteAddr = "127.0.0.1:4567"
	req.Header.Set("X-Forwarded-For", "203.0.113.200, 198.51.100.12")

	if got := GetClientIP(req); got != "198.51.100.12" {
		t.Fatalf("GetClientIP() = %q, want rightmost forwarded IP", got)
	}
}

func TestGetClientIPFallsBackWhenProxyHeaderInvalid(t *testing.T) {
	req := httptest.NewRequest("GET", "https://example.test/", nil)
	req.RemoteAddr = "[::1]:4567"
	req.Header.Set("X-Real-IP", "not-an-ip")

	if got := GetClientIP(req); got != "::1" {
		t.Fatalf("GetClientIP() = %q, want proxy peer IP", got)
	}
}
