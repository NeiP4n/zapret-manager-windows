package doh

import (
	"context"
	"net"
	"os"
	"testing"
	"time"
)

// TestProxyLive resolves through a real DoH provider via the local proxy (needs network; ZM_LIVE=1).
func TestProxyLive(t *testing.T) {
	if os.Getenv("ZM_LIVE") == "" {
		t.Skip("ZM_LIVE")
	}
	p := &Proxy{URL: "https://cloudflare-dns.com/dns-query", Bootstrap: []string{"1.1.1.1"}}
	if err := p.Start([]string{"127.0.0.1:15353"}); err != nil {
		t.Fatal(err)
	}
	defer p.Stop()
	for _, nw := range []string{"udp", "tcp"} {
		r := &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return net.Dial(nw, "127.0.0.1:15353")
		}}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		ips, err := r.LookupHost(ctx, "youtube.com")
		cancel()
		if err != nil || len(ips) == 0 {
			t.Fatalf("%s: %v", nw, err)
		}
		t.Logf("%s youtube.com → %v", nw, ips)
	}
	t.Logf("stats %+v", p.Stats())
	ips, err := Query(context.Background(), "https://dns.comss.one/dns-query", "rutracker.org", []string{"77.88.8.8"})
	t.Logf("comss rutracker.org → %v %v", ips, err)
}
