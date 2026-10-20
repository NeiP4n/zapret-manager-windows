package evade

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Живая проверка проб против локального TLS-сервера: handshake идёт двумя путями
// (целиком и с разрезанным ClientHello), после чего меряется скорость чтения.
// Сеть наружу не нужна, поэтому тест детерминированный.
func TestProbeHostLocalTLS(t *testing.T) {
	const bodySize = 300 << 10
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(bodySize))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(make([]byte, bodySize))
	}))
	defer srv.Close()

	addr := strings.TrimPrefix(srv.URL, "https://")
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("адрес сервера: %v", err)
	}

	opt := DefaultOptions()
	opt.Timeout = 3 * time.Second
	opt.SplitDelay = 20 * time.Millisecond
	opt.SmallBytes = 4 << 10
	opt.BulkBytes = 64 << 10

	ev := ProbeHost(context.Background(), addr, opt)

	if !ev.TCPOK {
		t.Fatalf("TCP не поднялся: %s", ev.DirectErr)
	}
	if !ev.DirectTLSOk {
		t.Fatalf("прямой handshake не прошёл: %s", ev.DirectErr)
	}
	if !ev.SplitTLSOk {
		t.Fatalf("handshake с разрезанным ClientHello не прошёл: %s", ev.SplitErr)
	}
	if ev.Port != port {
		t.Fatalf("порт %q, ожидался %q", ev.Port, port)
	}
	if ev.SmallBytes == 0 || ev.BulkBytes == 0 {
		t.Fatalf("скорость не измерена: small=%d bulk=%d", ev.SmallBytes, ev.BulkBytes)
	}
	if ev.BulkBytes < int(opt.BulkBytes)*3/4 {
		t.Fatalf("bulk прочитан не полностью: %d из %d", ev.BulkBytes, opt.BulkBytes)
	}
	// Скорость на loopback может быть неизмерима (быстрее разрешения часов). Это не
	// ошибка: rate=0 означает «слишком быстро, чтобы судить», и throttled() такое не ест.
	if throttled(ev) {
		t.Fatalf("loopback ошибочно принят за шейпинг: %+v", ev)
	}

	v := Classify(ev)
	if v.Class != ClassOK {
		t.Fatalf("класс %q (%s), ожидался ok", v.Class, v.Reason)
	}
}

// Обратная проверка: если порт закрыт, проба должна честно сказать "TCP не встаёт",
// а не превратить это в SNI-фильтрацию.
func TestProbeHostClosedPort(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close() // порт больше никто не слушает

	opt := DefaultOptions()
	opt.Timeout = 500 * time.Millisecond
	ev := ProbeHost(context.Background(), addr, opt)
	if ev.TCPOK {
		t.Skip("порт успел занять кто-то другой")
	}
	v := Classify(ev)
	if v.Class != ClassUnknown {
		t.Fatalf("класс %q (%s), ожидался unknown", v.Class, v.Reason)
	}
	if v.Plan != PlanTunnel {
		t.Fatalf("план %q, ожидался tunnel", v.Plan)
	}
}
