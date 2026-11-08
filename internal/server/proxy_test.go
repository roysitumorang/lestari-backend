package server

import (
	"io"
	"log/slog"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"

	"github.com/roysitumorang/lestari-backend/internal/config"
)

// ipApp mirrors the proxy settings of New with a route that echoes c.IP().
// Requests made with app.Test arrive from 0.0.0.0.
func ipApp(t *testing.T, proxies []string) *fiber.App {
	t.Helper()
	trust, header := trustProxy(config.Config{TrustedProxies: proxies, ProxyHeader: "X-Forwarded-For"}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	app := fiber.New(fiber.Config{ProxyHeader: header, TrustProxy: header != "", TrustProxyConfig: trust, EnableIPValidation: true})
	app.Get("/ip", func(c fiber.Ctx) error { return c.SendString(c.IP()) })
	return app
}

func clientIP(t *testing.T, app *fiber.App, xff string) string {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), "GET", "/ip", nil)
	if xff != "" {
		req.Header.Set("X-Forwarded-For", xff)
	}
	res, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	b, _ := io.ReadAll(res.Body)
	return string(b)
}

func TestClientIPBehindProxy(t *testing.T) {
	cases := []struct {
		name    string
		proxies []string
		xff     string
		want    string
	}{
		{"trusted proxy forwards the client", []string{"0.0.0.0"}, "203.0.113.7", "203.0.113.7"},
		{"spoofed entry is skipped (proxy appends the real peer)", []string{"0.0.0.0"}, "6.6.6.6, 203.0.113.7", "203.0.113.7"},
		{"chain of trusted proxies is walked", []string{"0.0.0.0", "10.0.0.0/8"}, "203.0.113.7, 10.1.2.3", "203.0.113.7"},
		{"untrusted peer: header ignored", []string{"loopback"}, "203.0.113.7", "0.0.0.0"},
		{"no proxies configured: header ignored", nil, "203.0.113.7", "0.0.0.0"},
		{"invalid entries are ignored", []string{"not-an-ip"}, "203.0.113.7", "0.0.0.0"},
		{"trusted proxy without header", []string{"0.0.0.0"}, "", "0.0.0.0"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := clientIP(t, ipApp(t, tc.proxies), tc.xff); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestTrustedProxiesConfig(t *testing.T) {
	t.Setenv("TRUSTED_PROXIES", "")
	if got := config.Load().TrustedProxies; len(got) != 1 || got[0] != "loopback" {
		t.Errorf("default: %v", got)
	}
	t.Setenv("TRUSTED_PROXIES", "none")
	if got := config.Load().TrustedProxies; len(got) != 0 {
		t.Errorf("none: %v", got)
	}
	t.Setenv("TRUSTED_PROXIES", "loopback, 10.0.0.0/8 ,192.0.2.10")
	if got := config.Load().TrustedProxies; len(got) != 3 || got[1] != "10.0.0.0/8" {
		t.Errorf("list: %v", got)
	}
}
