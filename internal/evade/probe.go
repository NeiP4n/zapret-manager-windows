package evade

// Network probes (layer L1). They deliberately use a plain socket, not WinDivert: they must
// run without the driver, without a filter and without touching the user's traffic. What they
// cannot see (overlapping IP fragments, SEQ overlap handling, packet-level TTL fooling) is
// listed in docs/adaptive-core.md as the next step, not pretended here.

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Options tunes the probes. Defaults trade ~10-20 seconds per host for a verdict.
type Options struct {
	Timeout    time.Duration // на одну операцию
	SplitAt    int           // сколько байт ClientHello отправить первым куском
	SplitDelay time.Duration // пауза между кусками
	SmallBytes int64         // маленькая выборка для baseline
	BulkBytes  int64         // объём, на котором виден шейпинг
	Parallel   int           // сколько хостов проверять одновременно

	CheckWhitelist bool   // включить детект белых списков (два лишних соединения)
	RUHost         string // заведомо «родной» хост
	ForeignAddr    string // заведомо зарубежный адрес
	ForeignSNI     string
}

// DefaultOptions: 8 КБ и 256 КБ — граница эвристики ТСПУ лежит около 15 КБ, поэтому
// маленькая выборка обязана быть меньше неё, а большая — заметно больше.
func DefaultOptions() Options {
	return Options{
		Timeout:        6 * time.Second,
		SplitAt:        1,
		SplitDelay:     120 * time.Millisecond,
		SmallBytes:     8 << 10,
		BulkBytes:      256 << 10,
		Parallel:       3,
		CheckWhitelist: false,
		RUHost:         "ya.ru",
		ForeignAddr:    "1.1.1.1:443",
		ForeignSNI:     "cloudflare.com",
	}
}

// splitConn sends the first n bytes of the first Write, pauses, then sends the rest.
// This is the app-level analogue of winws's split: enough to break SNI extraction on a DPI
// that does not reassemble, and it needs no driver at all.
type splitConn struct {
	net.Conn
	at    int
	delay time.Duration
	done  bool
}

func (s *splitConn) Write(p []byte) (int, error) {
	if s.done || s.at <= 0 || len(p) <= s.at {
		s.done = true
		return s.Conn.Write(p)
	}
	s.done = true
	n, err := s.Conn.Write(p[:s.at])
	if err != nil {
		return n, err
	}
	if s.delay > 0 {
		time.Sleep(s.delay)
	}
	m, err := s.Conn.Write(p[s.at:])
	return n + m, err
}

func hostPort(host, port string) string {
	if _, _, err := net.SplitHostPort(host); err == nil {
		return host
	}
	return net.JoinHostPort(host, port)
}

// dialTLS opens TCP (NODELAY) and completes a TLS handshake, optionally with the first
// ClientHello bytes sent separately. Certificate verification is off on purpose: a blocked
// handshake and a MITM are different findings, and a cert error would hide a reset.
func dialTLS(ctx context.Context, addr, sni string, splitAt int, delay, timeout time.Duration) (*tls.Conn, error) {
	d := &net.Dialer{Timeout: timeout}
	raw, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	if tc, ok := raw.(*net.TCPConn); ok {
		_ = tc.SetNoDelay(true) // иначе первый кусок склеится со вторым и проба бессмысленна
	}
	_ = raw.SetDeadline(time.Now().Add(timeout))
	var c net.Conn = raw
	if splitAt > 0 {
		c = &splitConn{Conn: raw, at: splitAt, delay: delay}
	}
	tc := tls.Client(c, &tls.Config{ServerName: sni, InsecureSkipVerify: true})
	if err := tc.HandshakeContext(ctx); err != nil {
		_ = raw.Close()
		return nil, err
	}
	return tc, nil
}

func ms(d time.Duration) int { return int(d.Milliseconds()) }

// us возвращает дробные микросекунды. Ноль означает «быстрее разрешения таймера»:
// чтение 64 КБ на loopback регулярно даёт 0 нс. Подставлять сюда нижнюю границу нельзя —
// полученная из неё скорость завышена на порядки и даёт ложный шейпинг, поэтому 0 так и
// остаётся 0, а вызывающий считает скорость неизвестной (см. throttled).
func us(d time.Duration) float64 { return float64(d.Nanoseconds()) / 1000 }

// ProbeHost measures one host: TCP, direct TLS, split TLS, then a bulk transfer.
func ProbeHost(ctx context.Context, host string, opt Options) Evidence {
	if opt.Timeout <= 0 {
		opt = DefaultOptions()
	}
	ev := Evidence{Host: host, Port: PortOf(host, "443")}
	addr := hostPort(host, "443")
	// SNI — только имя хоста: "host:port" невалиден как ServerName и уронил бы handshake.
	sni := host
	if h, _, err := net.SplitHostPort(host); err == nil {
		sni = h
	}

	t0 := time.Now()
	conn, err := (&net.Dialer{Timeout: opt.Timeout}).DialContext(ctx, "tcp", addr)
	ev.TCPms = ms(time.Since(t0))
	if err != nil {
		ev.DirectErr = trimErr(err)
		ev.RSTms = ev.TCPms
	} else {
		ev.TCPOK = true
		_ = conn.Close()
	}

	if ev.TCPOK {
		start := time.Now()
		if c, err := dialTLS(ctx, addr, sni, 0, 0, opt.Timeout); err != nil {
			ev.DirectErr = trimErr(err)
			ev.RSTms = ms(time.Since(start))
		} else {
			ev.DirectTLSOk = true
			_ = c.Close()
		}
		if c, err := dialTLS(ctx, addr, sni, opt.SplitAt, opt.SplitDelay, opt.Timeout); err != nil {
			ev.SplitErr = trimErr(err)
		} else {
			ev.SplitTLSOk = true
			_ = c.Close()
		}
	}

	// Скорость меряем только если хоть один путь открылся, иначе это просто таймаут.
	if ev.DirectTLSOk || ev.SplitTLSOk {
		splitAt := 0
		if !ev.DirectTLSOk {
			splitAt = opt.SplitAt
		}
		ev.SmallBytes, ev.SmallUs, ev.BulkBytes, ev.BulkUs = fetchStream(ctx, addr, sni, splitAt,
			opt.SplitDelay, opt.Timeout, opt.SmallBytes, opt.BulkBytes)
	}

	if opt.CheckWhitelist {
		ru := reachable(ctx, hostPort(opt.RUHost, "443"), opt.RUHost, opt.Timeout)
		foreign := reachable(ctx, opt.ForeignAddr, opt.ForeignSNI, opt.Timeout)
		ev.RUOK, ev.ForeignOK = &ru, &foreign
	}
	return ev
}

// fetchStream читает один ответ и меряет два окна одного и того же потока: первые small
// байт и следующие bulk байт. Одно соединение и два окна важнее, чем две отдельные
// выборки: иначе в «маленький» замер попадает TTFB, и он измеряет задержку, а не полосу
// (на localhost это давало фальшивый шейпинг).
func fetchStream(ctx context.Context, addr, sni string, splitAt int, delay, timeout time.Duration, small, bulk int64) (int, float64, int, float64) {
	tr := &http.Transport{
		DisableKeepAlives: true,
		DialTLSContext: func(dctx context.Context, network, a string) (net.Conn, error) {
			return dialTLS(dctx, addr, sni, splitAt, delay, timeout)
		},
	}
	defer tr.CloseIdleConnections()
	cl := &http.Client{Transport: tr, Timeout: timeout * 3}
	total := small + bulk
	req, err := http.NewRequestWithContext(ctx, "GET", "https://"+addr+"/", nil)
	if err != nil {
		return 0, 0, 0, 0
	}
	req.Host = sni
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) zmwin")
	req.Header.Set("Range", fmt.Sprintf("bytes=0-%d", total-1))

	resp, err := cl.Do(req)
	if err != nil {
		return 0, 0, 0, 0
	}
	defer resp.Body.Close()

	smallN, smallD := readWindow(resp.Body, small)
	bulkN, bulkD := readWindow(resp.Body, bulk)
	if int64(smallN) < small/4 {
		// слишком короткий ответ, чтобы судить о скорости
		smallN, smallD = 0, 0
	}
	if int64(bulkN) < bulk/4 {
		bulkN, bulkD = 0, 0
	}
	return smallN, smallD, bulkN, bulkD
}

// readWindow читает до n байт и возвращает прочитанное и время чтения.
func readWindow(r io.Reader, n int64) (int, float64) {
	buf := make([]byte, 32<<10)
	var got int
	start := time.Now()
	for int64(got) < n {
		want := n - int64(got)
		if want > int64(len(buf)) {
			want = int64(len(buf))
		}
		m, err := r.Read(buf[:want])
		got += m
		if err != nil {
			break
		}
	}
	d := time.Since(start)
	if got == 0 {
		return 0, 0
	}
	return got, us(d)
}

func reachable(ctx context.Context, addr, sni string, timeout time.Duration) bool {
	c, err := dialTLS(ctx, addr, sni, 0, 0, timeout)
	if err != nil {
		return false
	}
	_ = c.Close()
	return true
}

// trimErr keeps the error text short enough for the panel.
func trimErr(err error) string {
	if err == nil {
		return ""
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return "таймаут"
	}
	s := err.Error()
	if i := strings.LastIndex(s, ": "); i >= 0 && len(s) > 80 {
		s = s[i+2:]
	}
	if len(s) > 120 {
		s = s[:120] + "…"
	}
	return s
}

// PortOf is a small helper for callers that build Evidence from their own probes.
func PortOf(host string, def string) string {
	if _, p, err := net.SplitHostPort(host); err == nil {
		return p
	}
	if def == "" {
		def = "443"
	}
	if _, err := strconv.Atoi(def); err != nil {
		return "443"
	}
	return def
}
