package evade

import (
	"context"
	"net"
	"testing"
	"time"
)

func bp(b bool) *bool { return &b }

func TestClassify(t *testing.T) {
	cases := []struct {
		name      string
		ev        Evidence
		wantClass Class
		wantPlan  Plan
	}{
		{
			name:      "нет сети вообще",
			ev:        Evidence{TCPOK: false, RUOK: bp(false), ForeignOK: bp(false)},
			wantClass: ClassUnknown, wantPlan: PlanTunnel,
		},
		{
			name:      "мобильный белый список",
			ev:        Evidence{TCPOK: false, RUOK: bp(true), ForeignOK: bp(false)},
			wantClass: ClassWhitelist, wantPlan: PlanTunnel,
		},
		{
			name:      "TCP не встаёт",
			ev:        Evidence{TCPOK: false, DirectErr: "таймаут"},
			wantClass: ClassUnknown, wantPlan: PlanTunnel,
		},
		{
			name: "всё открывается",
			ev: Evidence{TCPOK: true, DirectTLSOk: true, SplitTLSOk: true,
				SmallBytes: 8192, SmallUs: 20000, BulkBytes: 262144, BulkUs: 400000},
			wantClass: ClassOK, wantPlan: PlanDirect,
		},
		{
			name: "шейпинг по объёму",
			ev: Evidence{TCPOK: true, DirectTLSOk: true,
				SmallBytes: 8192, SmallUs: 20000, BulkBytes: 262144, BulkUs: 8000000},
			wantClass: ClassThrottle, wantPlan: PlanTunnel,
		},
		{
			name:      "фильтрация по SNI",
			ev:        Evidence{TCPOK: true, DirectTLSOk: false, SplitTLSOk: true, RSTms: 300},
			wantClass: ClassSNI, wantPlan: PlanDesync,
		},
		{
			name:      "обрыв не по SNI",
			ev:        Evidence{TCPOK: true, DirectTLSOk: false, SplitTLSOk: false, RSTms: 200},
			wantClass: ClassReset, wantPlan: PlanDesync,
		},
	}
	for _, c := range cases {
		got := Classify(c.ev)
		if got.Class != c.wantClass {
			t.Errorf("%s: класс %q, ожидался %q (%s)", c.name, got.Class, c.wantClass, got.Reason)
		}
		if got.Plan != c.wantPlan {
			t.Errorf("%s: план %q, ожидался %q", c.name, got.Plan, c.wantPlan)
		}
		if got.Reason == "" {
			t.Errorf("%s: пустое объяснение", c.name)
		}
	}
}

func TestClassifySNIPrefersDesync(t *testing.T) {
	// При SNI-фильтрации desync дешевле app-level split, а split остаётся запасным.
	v := Classify(Evidence{TCPOK: true, SplitTLSOk: true})
	if Decide(v) != PlanDesync {
		t.Fatalf("Decide = %q, ожидался desync", Decide(v))
	}
	if v.Fallback != PlanSplit {
		t.Fatalf("fallback = %q, ожидался split", v.Fallback)
	}
}

func TestThrottleNeedsBothSamples(t *testing.T) {
	// Один замер — не повод объявлять шейпинг: таймаут bulk не должен превращаться в приговор.
	if throttled(Evidence{SmallBytes: 8192, SmallUs: 20000}) {
		t.Fatal("bulk без данных не должен считаться шейпингом")
	}
	if throttled(Evidence{SmallBytes: 0, BulkBytes: 262144, BulkUs: 9000000}) {
		t.Fatal("без baseline нельзя судить")
	}
	// Медленный baseline: всё медленно, это не селективный шейпинг.
	if throttled(Evidence{SmallBytes: 8192, SmallUs: 3000000, BulkBytes: 262144, BulkUs: 60000000}) {
		t.Fatal("медленный baseline не должен давать throttle")
	}
}

func TestScanKeepsOrderAndRecovers(t *testing.T) {
	probe := func(ctx context.Context, host string, opt Options) Evidence {
		switch host {
		case "ok":
			return Evidence{TCPOK: true, DirectTLSOk: true,
				SmallBytes: 8192, SmallUs: 10000, BulkBytes: 262144, BulkUs: 100000}
		case "boom":
			panic("сеть сломалась")
		default:
			return Evidence{TCPOK: true, SplitTLSOk: true}
		}
	}
	opt := DefaultOptions()
	opt.Parallel = 2
	got := Scan(context.Background(), []string{"sni", "boom", "ok"}, opt, probe)
	if len(got) != 3 {
		t.Fatalf("получено %d вердиктов", len(got))
	}
	if got[0].Class != ClassSNI || got[2].Class != ClassOK {
		t.Fatalf("порядок нарушен: %+v", got)
	}
	if got[1].Class != ClassUnknown {
		t.Fatalf("паника должна давать unknown, получено %q", got[1].Class)
	}
}

func TestSplitConnSendsFirstByteSeparately(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	s := &splitConn{Conn: client, at: 1, delay: 30 * time.Millisecond}
	payload := []byte("0123456789")
	done := make(chan error, 1)
	go func() { _, err := s.Write(payload); done <- err }()

	first := make([]byte, 1)
	_ = server.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := server.Read(first); err != nil {
		t.Fatalf("первый байт не пришёл: %v", err)
	}
	if first[0] != '0' {
		t.Fatalf("первый байт %q", first[0])
	}
	// Пауза должна быть реальной: остаток не может прийти мгновенно.
	_ = server.SetReadDeadline(time.Now().Add(10 * time.Millisecond))
	tmp := make([]byte, 16)
	if _, err := server.Read(tmp); err == nil {
		t.Fatal("остаток пришёл без паузы — split не работает")
	}
	_ = server.SetReadDeadline(time.Now().Add(time.Second))
	rest := make([]byte, 16)
	n, err := server.Read(rest)
	if err != nil {
		t.Fatalf("остаток не пришёл: %v", err)
	}
	if string(rest[:n]) != string(payload[1:]) {
		t.Fatalf("остаток %q", rest[:n])
	}
	if err := <-done; err != nil {
		t.Fatalf("Write вернул ошибку: %v", err)
	}
}

func TestSplitConnSingleWriteWhenShort(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()
	s := &splitConn{Conn: client, at: 8, delay: time.Second}
	go func() { _, _ = s.Write([]byte("abc")) }()
	_ = server.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	buf := make([]byte, 8)
	n, err := server.Read(buf)
	if err != nil || string(buf[:n]) != "abc" {
		t.Fatalf("короткая запись должна уходить целиком: %q %v", buf[:n], err)
	}
}

func TestHostPort(t *testing.T) {
	if got := hostPort("example.com", "443"); got != "example.com:443" {
		t.Fatalf("hostPort = %q", got)
	}
	if got := hostPort("example.com:8443", "443"); got != "example.com:8443" {
		t.Fatalf("hostPort c портом = %q", got)
	}
}
