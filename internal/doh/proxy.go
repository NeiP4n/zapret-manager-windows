package doh

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"

	"golang.org/x/net/dns/dnsmessage"

	"github.com/zapretmanager/zmwin/internal/app"
)

// Proxy is a local DNS→DoH forwarder (Windows counterpart of https-dns-proxy on the router):
// it listens on 127.0.0.1:53 / [::1]:53 and sends every query as RFC 8484 POST to the provider.
type Proxy struct {
	URL       string
	Bootstrap []string // plain-DNS IPs used once to resolve the DoH host
	NoPlain   bool     // force mode: never send plain DNS (port 53 is firewalled)

	mu      sync.Mutex
	udp     []net.PacketConn
	tcp     []net.Listener
	client  *http.Client
	cache   map[string]cacheEntry
	ips     []string
	stats   Stats
	stopped bool
}

type Stats struct {
	Queries  int64  `json:"queries"`
	Errors   int64  `json:"errors"`
	CacheHit int64  `json:"cache_hits"`
	LastErr  string `json:"last_error,omitempty"`
	Resolved string `json:"server_ips"`
}

type cacheEntry struct {
	msg []byte
	exp time.Time
}

// DoH endpoints reachable by IP (certificates carry IP SANs) — bootstrap without plain DNS.
var ipDoH = []string{"https://1.1.1.1/dns-query", "https://8.8.8.8/dns-query", "https://9.9.9.9/dns-query"}

func (p *Proxy) Start(listen []string) error {
	p.cache = map[string]cacheEntry{}
	p.client = &http.Client{
		Timeout: 8 * time.Second,
		Transport: &http.Transport{
			DialContext:         p.dialDoH,
			ForceAttemptHTTP2:   true,
			MaxIdleConnsPerHost: 4,
			IdleConnTimeout:     90 * time.Second,
			TLSHandshakeTimeout: 6 * time.Second,
			TLSClientConfig:     &tls.Config{MinVersion: tls.VersionTLS12},
		},
	}
	var errs []string
	for _, addr := range listen {
		pc, err := net.ListenPacket("udp", addr)
		if err != nil {
			errs = append(errs, err.Error())
			continue
		}
		ln, err := net.Listen("tcp", addr)
		if err != nil {
			pc.Close()
			errs = append(errs, err.Error())
			continue
		}
		p.udp = append(p.udp, pc)
		p.tcp = append(p.tcp, ln)
		go p.serveUDP(pc)
		go p.serveTCP(ln)
	}
	if len(p.udp) == 0 {
		return fmt.Errorf("не удалось занять порт 53: %v (порт занят другой программой — например, «Общий доступ к интернету» Windows)", errs)
	}
	go p.janitor()
	return nil
}

func (p *Proxy) Stop() {
	p.mu.Lock()
	p.stopped = true
	p.mu.Unlock()
	for _, c := range p.udp {
		c.Close()
	}
	for _, l := range p.tcp {
		l.Close()
	}
	if p.client != nil {
		p.client.CloseIdleConnections()
	}
}

func (p *Proxy) Stats() Stats {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.stats
}

func (p *Proxy) janitor() {
	for {
		time.Sleep(time.Minute)
		p.mu.Lock()
		if p.stopped {
			p.mu.Unlock()
			return
		}
		now := time.Now()
		for k, v := range p.cache {
			if now.After(v.exp) {
				delete(p.cache, k)
			}
		}
		p.mu.Unlock()
	}
}

func (p *Proxy) serveUDP(pc net.PacketConn) {
	buf := make([]byte, 4096)
	for {
		n, addr, err := pc.ReadFrom(buf)
		if err != nil {
			return
		}
		q := append([]byte(nil), buf[:n]...)
		go func() {
			resp := p.handle(q)
			if resp == nil {
				return
			}
			if lim := udpLimit(q); len(resp) > lim {
				resp = truncate(q)
			}
			_, _ = pc.WriteTo(resp, addr)
		}()
	}
}

func (p *Proxy) serveTCP(ln net.Listener) {
	for {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		go func(c net.Conn) {
			defer c.Close()
			for {
				_ = c.SetDeadline(time.Now().Add(20 * time.Second))
				var l uint16
				if err := binary.Read(c, binary.BigEndian, &l); err != nil {
					return
				}
				q := make([]byte, l)
				if _, err := io.ReadFull(c, q); err != nil {
					return
				}
				resp := p.handle(q)
				if resp == nil {
					return
				}
				out := make([]byte, 2+len(resp))
				binary.BigEndian.PutUint16(out, uint16(len(resp)))
				copy(out[2:], resp)
				if _, err := c.Write(out); err != nil {
					return
				}
			}
		}(c)
	}
}

func (p *Proxy) handle(q []byte) []byte {
	if len(q) < 12 {
		return nil
	}
	id := binary.BigEndian.Uint16(q)
	key := string(q[2:])
	p.mu.Lock()
	p.stats.Queries++
	if e, ok := p.cache[key]; ok && time.Now().Before(e.exp) {
		p.stats.CacheHit++
		p.mu.Unlock()
		r := append([]byte(nil), e.msg...)
		binary.BigEndian.PutUint16(r, id)
		return r
	}
	p.mu.Unlock()

	wire := append([]byte(nil), q...)
	binary.BigEndian.PutUint16(wire, 0) // RFC 8484: ID 0 for cache friendliness
	resp, err := p.exchange(wire)
	if err != nil {
		p.mu.Lock()
		p.stats.Errors++
		p.stats.LastErr = err.Error()
		p.mu.Unlock()
		return servfail(q)
	}
	binary.BigEndian.PutUint16(resp, id)
	if ttl := minTTL(resp); ttl > 0 {
		p.mu.Lock()
		if len(p.cache) > 5000 {
			p.cache = map[string]cacheEntry{}
		}
		p.cache[key] = cacheEntry{msg: append([]byte(nil), resp...), exp: time.Now().Add(ttl)}
		p.mu.Unlock()
	}
	return resp
}

func (p *Proxy) exchange(wire []byte) ([]byte, error) {
	req, _ := http.NewRequest("POST", p.URL, bytes.NewReader(wire))
	req.Header.Set("Content-Type", "application/dns-message")
	req.Header.Set("Accept", "application/dns-message")
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("DoH HTTP %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 65535))
	if err != nil || len(b) < 12 {
		return nil, fmt.Errorf("пустой ответ DoH")
	}
	return b, nil
}

// dialDoH connects to the DoH server by IPs found through bootstrap, never through system DNS
// (system DNS points back at this proxy).
func (p *Proxy) dialDoH(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, _ := net.SplitHostPort(addr)
	if net.ParseIP(host) != nil {
		return (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, network, addr)
	}
	ips := p.serverIPs(ctx, host)
	if len(ips) == 0 {
		return nil, fmt.Errorf("bootstrap: не удалось узнать адрес %s", host)
	}
	var last error
	for _, ip := range ips {
		c, err := (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, network, net.JoinHostPort(ip, port))
		if err == nil {
			return c, nil
		}
		last = err
	}
	p.mu.Lock()
	p.ips = nil // re-resolve next time
	p.mu.Unlock()
	return nil, last
}

func (p *Proxy) serverIPs(ctx context.Context, host string) []string {
	p.mu.Lock()
	if len(p.ips) > 0 {
		ips := p.ips
		p.mu.Unlock()
		return ips
	}
	p.mu.Unlock()
	ips := Bootstrap(ctx, host, p.Bootstrap, p.NoPlain)
	p.mu.Lock()
	p.ips = ips
	p.stats.Resolved = fmt.Sprint(ips)
	p.mu.Unlock()
	return ips
}

// Bootstrap resolves host via plain DNS to the given IPs, then via IP-addressed DoH.
func Bootstrap(ctx context.Context, host string, plain []string, noPlain bool) []string {
	if !noPlain {
		for _, b := range plain {
			r := &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
				return (&net.Dialer{Timeout: 3 * time.Second}).DialContext(ctx, "udp", net.JoinHostPort(b, "53"))
			}}
			c, cancel := context.WithTimeout(ctx, 4*time.Second)
			addrs, err := r.LookupIP(c, "ip4", host)
			cancel()
			if err == nil && len(addrs) > 0 {
				var out []string
				for _, a := range addrs {
					out = append(out, a.String())
				}
				return out
			}
		}
	}
	for _, u := range ipDoH {
		if ips := dohLookup(ctx, u, host); len(ips) > 0 {
			return ips
		}
	}
	return nil
}

func dohLookup(ctx context.Context, server, host string) []string {
	name, err := dnsmessage.NewName(host + ".")
	if err != nil {
		return nil
	}
	m := dnsmessage.Message{Header: dnsmessage.Header{RecursionDesired: true},
		Questions: []dnsmessage.Question{{Name: name, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET}}}
	wire, _ := m.Pack()
	c, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(c, "POST", server, bytes.NewReader(wire))
	req.Header.Set("Content-Type", "application/dns-message")
	resp, err := (&http.Client{Transport: &http.Transport{Proxy: nil}}).Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 65535))
	var r dnsmessage.Message
	if r.Unpack(b) != nil {
		return nil
	}
	var out []string
	for _, a := range r.Answers {
		if v, ok := a.Body.(*dnsmessage.AResource); ok {
			out = append(out, net.IP(v.A[:]).String())
		}
	}
	return out
}

// Query resolves a name through a DoH URL — used by the panel's "проверить" button.
func Query(ctx context.Context, dohURL, name string, bootstrap []string) ([]string, error) {
	u, err := url.Parse(dohURL)
	if err != nil {
		return nil, err
	}
	p := &Proxy{URL: dohURL, Bootstrap: bootstrap}
	p.client = &http.Client{Timeout: 8 * time.Second, Transport: &http.Transport{DialContext: p.dialDoH, ForceAttemptHTTP2: true}}
	_ = u
	n, err := dnsmessage.NewName(name + ".")
	if err != nil {
		return nil, err
	}
	m := dnsmessage.Message{Header: dnsmessage.Header{RecursionDesired: true},
		Questions: []dnsmessage.Question{{Name: n, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET}}}
	wire, _ := m.Pack()
	resp, err := p.exchange(wire)
	if err != nil {
		return nil, err
	}
	var r dnsmessage.Message
	if err := r.Unpack(resp); err != nil {
		return nil, err
	}
	var out []string
	for _, a := range r.Answers {
		if v, ok := a.Body.(*dnsmessage.AResource); ok {
			out = append(out, net.IP(v.A[:]).String())
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("нет A-записей (код %v)", r.RCode)
	}
	return out, nil
}

func minTTL(msg []byte) time.Duration {
	var p dnsmessage.Parser
	h, err := p.Start(msg)
	if err != nil || h.RCode != dnsmessage.RCodeSuccess && h.RCode != dnsmessage.RCodeNameError {
		return 0
	}
	if err := p.SkipAllQuestions(); err != nil {
		return 0
	}
	min := uint32(300)
	found := false
	for {
		rh, err := p.AnswerHeader()
		if err != nil {
			break
		}
		found = true
		if rh.TTL < min {
			min = rh.TTL
		}
		if p.SkipAnswer() != nil {
			break
		}
	}
	if !found {
		min = 30 // negative / empty answers: short cache
	}
	if min < 5 {
		return 0
	}
	return time.Duration(min) * time.Second
}

func udpLimit(q []byte) int {
	var p dnsmessage.Parser
	if _, err := p.Start(q); err != nil {
		return 512
	}
	_ = p.SkipAllQuestions()
	_ = p.SkipAllAnswers()
	_ = p.SkipAllAuthorities()
	for {
		h, err := p.AdditionalHeader()
		if err != nil {
			return 512
		}
		if h.Type == dnsmessage.TypeOPT {
			if sz := int(h.Class); sz > 512 {
				return sz
			}
			return 512
		}
		if p.SkipAdditional() != nil {
			return 512
		}
	}
}

// truncate builds a header+question reply with TC set, so the client retries over TCP.
func truncate(q []byte) []byte {
	return reply(q, func(h *dnsmessage.Header) { h.Truncated = true })
}

func servfail(q []byte) []byte {
	return reply(q, func(h *dnsmessage.Header) { h.RCode = dnsmessage.RCodeServerFailure })
}

func reply(q []byte, mod func(h *dnsmessage.Header)) []byte {
	var p dnsmessage.Parser
	h, err := p.Start(q)
	if err != nil {
		return nil
	}
	qs, _ := p.AllQuestions()
	h.Response, h.RecursionAvailable = true, true
	mod(&h)
	m := dnsmessage.Message{Header: h, Questions: qs}
	b, err := m.Pack()
	if err != nil {
		return nil
	}
	return b
}

var _ = app.Logf
