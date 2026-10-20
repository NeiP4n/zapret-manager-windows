package zapret

// Anti-fingerprint strategy generator (layer L2 of the adaptive core, see docs/adaptive-core.md).
//
// The router (and therefore this port) keeps one fixed NFQWS_OPT parameter set per strategy.
// Since June 2026 TSPU freezes connections by behaviour, so a constant parameter set is itself
// a fingerprint: the same split position, seqovl and fooling on every connection is as telling
// as the first bytes of a TLS handshake. RandomBody draws parameters from a validated envelope
// for every seed, renders the usual strategy body (one token per line, "#marker" first) and
// keeps it pasteable to/from the router — paths stay portable (files/fake/..., lists/...).

import (
	"fmt"
	"math/rand"
	"strconv"
	"strings"
)

// Envelope is the space RandomBody may pick from. Every value is one winws accepts; the
// defaults are the documented fake/split/multisplit/ttl family that the router presets use.
type Envelope struct {
	Desync    []string // --dpi-desync values
	SplitPos  []string // --dpi-desync-split-pos values
	Fooling   []string // --dpi-desync-fooling values
	SeqOvl    []int    // --dpi-desync-split-seqovl candidates, 0 = off
	TTLMin    int      // --dpi-desync-ttl range, 0 = do not set
	TTLMax    int
	RepeatMin int // --dpi-desync-repeats range
	RepeatMax int
	AutoTTL   bool   // emit --dpi-desync-autottl instead of --dpi-desync-ttl
	FakeTLS   string // --dpi-desync-fake-tls payload
	FakeMod   string // --dpi-desync-fake-tls-mod
}

// DefaultEnvelope is the conservative starting point: only primitives already used by the
// router presets, plus the TTL fooling that the presets never used.
func DefaultEnvelope() Envelope {
	return Envelope{
		Desync:    []string{"fake,multisplit", "multisplit", "fake,split2", "fake,disorder"},
		SplitPos:  []string{"1", "2", "midsld", "sniext+1"},
		Fooling:   []string{"ts", "badseq", "badsum", "badseq,badsum"},
		SeqOvl:    []int{0, 568, 582, 654, 681, 726},
		TTLMin:    3,
		TTLMax:    12,
		RepeatMin: 6,
		RepeatMax: 12,
		FakeTLS:   FK + "stun.bin",
		FakeMod:   "none",
	}
}

// RandomBody renders a strategy body (same format as state\strategy.txt) for the seed.
func RandomBody(seed int64) []string { return RandomBodyEnvelope(seed, DefaultEnvelope()) }

// RandomBodyEnvelope renders a strategy body from a custom envelope. The result is fully
// determined by (seed, env), so a strategy that works can be reproduced and exported.
func RandomBodyEnvelope(seed int64, env Envelope) []string {
	r := rand.New(rand.NewSource(seed))
	pick := func(xs []string) string { return xs[r.Intn(len(xs))] }
	between := func(a, b int) int {
		if b < a {
			a, b = b, a
		}
		return a + r.Intn(b-a+1)
	}

	desync := pick(env.Desync)
	out := []string{
		// Marker must not look like the router's own (#vN/#YvNN/#DvN/#GvN) or #udp443.
		fmt.Sprintf("#zm%06x", uint32(seed)&0xffffff),
		"--filter-tcp=443",
		"--hostlist-exclude=" + ExcludeList,
		"--dpi-desync=" + desync,
	}

	fake := strings.Contains(desync, "fake")
	split := fake || strings.Contains(desync, "split") || strings.Contains(desync, "disorder")

	if split && len(env.SplitPos) > 0 {
		out = append(out, "--dpi-desync-split-pos="+pick(env.SplitPos))
	}
	if len(env.Fooling) > 0 {
		out = append(out, "--dpi-desync-fooling="+pick(env.Fooling))
	}
	// TTL fooling: the decoy reaches the DPI but dies before the server. This is the knob the
	// router presets never expose and the one that matters on TSPU paths.
	if env.TTLMin > 0 && env.TTLMax > 0 {
		if env.AutoTTL {
			out = append(out, "--dpi-desync-autottl="+strconv.Itoa(between(2, 4)))
		} else {
			out = append(out, "--dpi-desync-ttl="+strconv.Itoa(between(env.TTLMin, env.TTLMax)))
		}
	}
	if fake && env.RepeatMax > 0 {
		out = append(out, "--dpi-desync-repeats="+strconv.Itoa(between(env.RepeatMin, env.RepeatMax)))
	}
	if split && len(env.SeqOvl) > 0 {
		// seqovl is only meaningful together with a pattern, as in the router presets.
		if ovl := env.SeqOvl[r.Intn(len(env.SeqOvl))]; ovl > 0 {
			out = append(out,
				"--dpi-desync-split-seqovl="+strconv.Itoa(ovl),
				"--dpi-desync-split-seqovl-pattern="+env.FakeTLS)
		}
	}
	if fake && env.FakeTLS != "" {
		out = append(out, "--dpi-desync-fake-tls="+env.FakeTLS)
		if env.FakeMod != "" {
			out = append(out, "--dpi-desync-fake-tls-mod="+env.FakeMod)
		}
	}
	return out
}

// ---------------- validation ----------------

var knownFlags = map[string]bool{
	"filter-tcp": true, "filter-udp": true, "filter-l3": true,
	"hostlist": true, "hostlist-exclude": true, "hostlist-domains": true,
	"dpi-desync": true,
	"dpi-desync-split-pos": true, "dpi-desync-split-seqovl": true,
	"dpi-desync-split-seqovl-pattern": true,
	"dpi-desync-fooling": true, "dpi-desync-repeats": true,
	"dpi-desync-ttl": true, "dpi-desync-ttl6": true, "dpi-desync-autottl": true,
	"dpi-desync-badseq-increment": true,
	"dpi-desync-fake-tls":                 true, "dpi-desync-fake-tls-mod": true,
	"dpi-desync-fake-quic": true, "dpi-desync-fake-unknown-udp": true,
	"dpi-desync-fake-discord": true, "dpi-desync-fake-stun": true,
	"dpi-desync-hostfakesplit-mod": true, "dpi-desync-hostfakesplit-midhost": true,
	"dpi-desync-fakedsplit-pattern": true, "dpi-desync-cutoff": true,
	"dpi-desync-any-protocol": true,
}

var (
	desyncModes = map[string]bool{
		"none": true, "fake": true, "split": true, "split2": true,
		"disorder": true, "disorder2": true, "multisplit": true,
		"fakedsplit": true, "fakeddisorder": true, "hostfakesplit": true, "oob": true,
	}
	foolingModes = map[string]bool{
		"none": true, "md5sig": true, "badsum": true, "badseq": true,
		"hopbyhop": true, "hopbyhop2": true, "datanoack": true, "ts": true,
	}
)

// MaxSplitPos / minTtl bound the numeric arguments so a typo cannot turn into a strategy
// that silently mangles every connection.
const (
	maxSplitPos = 64
	minTTL      = 0
	maxTTL      = 32
	maxRepeats  = 32
	maxSeqOvl   = 2000
)

// ValidateBody checks the grammar of a generated (or pasted) strategy body before it is
// written to state\strategy.txt. It is deliberately strict about flag names and value
// ranges: winws silently ignores what it does not understand, which reads as "strategy
// does nothing" to the user, and an out-of-range split position breaks the handshake.
func ValidateBody(lines []string) error {
	if len(lines) == 0 {
		return fmt.Errorf("пустая стратегия")
	}
	if !strings.HasPrefix(strings.TrimSpace(lines[0]), "#") {
		return fmt.Errorf("первая строка должна быть маркером #…, получено %q", lines[0])
	}
	seen := map[string]bool{}
	hasFilter := false
	for i, raw := range lines[1:] {
		l := strings.TrimSpace(raw)
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		if l == "--new" {
			continue
		}
		if !strings.HasPrefix(l, "--") {
			return fmt.Errorf("строка %d: ожидался флаг --…, получено %q", i+2, l)
		}
		flag, val, hasVal := strings.Cut(strings.TrimPrefix(l, "--"), "=")
		if !knownFlags[flag] {
			return fmt.Errorf("строка %d: неизвестный флаг --%s", i+2, flag)
		}
		if !hasVal {
			return fmt.Errorf("строка %d: у флага --%s нет значения", i+2, flag)
		}
		if flag == "filter-tcp" || flag == "filter-udp" {
			hasFilter = true
			continue
		}
		if strings.HasPrefix(flag, "dpi-desync") && flag != "dpi-desync" {
			seen[flag] = true
		}
		if err := validateValue(flag, val); err != nil {
			return fmt.Errorf("строка %d: %v", i+2, err)
		}
	}
	if !hasFilter {
		return fmt.Errorf("нет --filter-tcp/--filter-udp: перехватывать нечего")
	}
	if !seen["dpi-desync"] && len(seen) == 0 {
		return fmt.Errorf("нет ни одного --dpi-desync… флага")
	}
	return nil
}

func validateValue(flag, val string) error {
	num := func(min, max int) error {
		n, err := strconv.Atoi(val)
		if err != nil {
			return fmt.Errorf("--%s: ожидалось число, получено %q", flag, val)
		}
		if n < min || n > max {
			return fmt.Errorf("--%s=%d вне диапазона %d..%d", flag, n, min, max)
		}
		return nil
	}
	switch flag {
	case "dpi-desync":
		for _, m := range strings.Split(val, ",") {
			if !desyncModes[m] {
				return fmt.Errorf("--dpi-desync: неизвестный режим %q", m)
			}
		}
	case "dpi-desync-fooling":
		for _, m := range strings.Split(val, ",") {
			if !foolingModes[m] {
				return fmt.Errorf("--dpi-desync-fooling: неизвестный режим %q", m)
			}
		}
	case "dpi-desync-split-pos":
		for _, p := range strings.Split(val, ",") {
			switch p {
			case "midsld", "sniext", "sniext+1", "host+1", "host+2", "sld+1":
				continue
			}
			if err := validateNumber(p, flag, 1, maxSplitPos); err != nil {
				return err
			}
		}
	case "dpi-desync-ttl", "dpi-desync-ttl6":
		return num(minTTL, maxTTL)
	case "dpi-desync-repeats":
		return num(0, maxRepeats)
	case "dpi-desync-split-seqovl":
		return num(0, maxSeqOvl)
	case "dpi-desync-autottl":
		// winws accepts delta or delta:min:max; we only emit the plain delta form.
		n, err := strconv.Atoi(val)
		if err != nil || n < 1 || n > 12 {
			return fmt.Errorf("--%s=%q вне диапазона 1..12", flag, val)
		}
	}
	return nil
}

func validateNumber(s, flag string, min, max int) error {
	n, err := strconv.Atoi(s)
	if err != nil {
		return fmt.Errorf("--%s: %q не число", flag, s)
	}
	if n < min || n > max {
		return fmt.Errorf("--%s=%d вне диапазона %d..%d", flag, n, min, max)
	}
	return nil
}
