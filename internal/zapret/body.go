package zapret

import (
	"regexp"
	"strings"
)

// The strategy "body" is the router's NFQWS_OPT block, one token per line: "#marker" comments,
// "--option=value" lines and "--new" profile separators. Paths are stored relative to the data
// root ("files/fake/x.bin", "lists/x.txt") and made absolute only when winws is launched, so the
// same text can be pasted to/from a router (see NormalizePaths).

const (
	FK = "files/fake/"
	LS = "lists/"

	GoogleList  = LS + "zapret-hosts-google.txt"
	UserList    = LS + "zapret-hosts-user.txt"
	ExcludeList = LS + "zapret-hosts-user-exclude.txt"
	RknList     = LS + "zapret-hosts-rkn.txt"
)

var (
	nfqMarkRx    = regexp.MustCompile(`^[ \t]*#[ \t]*((Yv|Dv|Gv)[0-9]|udp443[ \t]*$)`)
	udp443MarkRx = regexp.MustCompile(`^[ \t]*#[ \t]*udp443[ \t]*$`)
	nochangeRx   = regexp.MustCompile(`(?i)#[ \t]*nochange`)
	neverRx      = regexp.MustCompile(`^#ZM_NEVER_MATCHES$`)
)

// Lines splits text into trimmed-right lines without empty ones.
func Lines(text string) []string {
	var out []string
	for _, l := range strings.Split(strings.ReplaceAll(text, "\r", ""), "\n") {
		l = strings.TrimRight(l, " \t")
		if strings.TrimSpace(l) == "" {
			continue
		}
		out = append(out, l)
	}
	return out
}

func Join(lines []string) string { return strings.Join(lines, "\n") }

func hasLine(body []string, want string) bool {
	for _, l := range body {
		if l == want {
			return true
		}
	}
	return false
}

func anyMatch(body []string, rx *regexp.Regexp) bool {
	for _, l := range body {
		if rx.MatchString(l) {
			return true
		}
	}
	return false
}

func firstMatch(body []string, rx *regexp.Regexp) []string {
	for _, l := range body {
		if m := rx.FindStringSubmatch(l); m != nil {
			return m
		}
	}
	return nil
}

// DropProfiles is a faithful port of the router's _nfq_drop_profiles awk program.
// Profiles (split on "--new") containing a line matching rx are removed unless a line also
// matches keep. Non-marker comments (strategy names) are hoisted to the top, de-duplicated;
// marker comments (#YvN/#DvN/#GvN/#udp443) stay attached to their profile; a #udp443 marker
// survives only on a profile that really has --filter-udp=443. Empty profiles and doubled
// "--new" disappear. removed reports whether anything was dropped.
func DropProfiles(body []string, rx, keep *regexp.Regexp) (out []string, removed bool) {
	var (
		names   []string
		seen    = map[string]bool{}
		o       []string
		b, pc   []string
		h       []string
		printed bool
	)
	holdIn := func() { b = append(b, h...); h = nil }
	flush := func() {
		if len(b) == 0 && len(pc) == 0 {
			return
		}
		drop, real, udp := false, 0, false
		for _, l := range pc {
			if rx.MatchString(l) {
				drop = true
			}
		}
		for _, l := range b {
			if rx.MatchString(l) {
				drop = true
			}
			if !strings.HasPrefix(strings.TrimLeft(l, " \t"), "#") {
				real++
			}
			if l == "--filter-udp=443" {
				udp = true
			}
		}
		if drop && keep != nil {
			for _, l := range append(append([]string{}, pc...), b...) {
				if keep.MatchString(l) {
					drop = false
				}
			}
		}
		if drop {
			removed = true
		} else if real > 0 {
			for _, l := range pc {
				if udp || !udp443MarkRx.MatchString(l) {
					o = append(o, l)
				}
			}
			if printed {
				o = append(o, "--new")
			}
			for _, l := range b {
				if udp || !udp443MarkRx.MatchString(l) {
					o = append(o, l)
				}
			}
			printed = true
		}
		b, pc = nil, nil
	}
	for _, l := range body {
		switch {
		case strings.TrimSpace(l) == "":
			continue
		case l == "--new":
			flush()
			pc = append(pc, h...)
			h = nil
		case strings.HasPrefix(strings.TrimLeft(l, " \t"), "#"):
			if nfqMarkRx.MatchString(l) {
				h = append(h, l)
			} else if !seen[l] {
				seen[l] = true
				names = append(names, l)
			}
		default:
			holdIn()
			b = append(b, l)
		}
	}
	holdIn()
	flush()
	return append(names, o...), removed
}

// Normalize rewrites the body into canonical form without removing anything (_nfq_normalize).
func Normalize(body []string) []string {
	out, _ := DropProfiles(body, neverRx, nil)
	return out
}

// DedupNew collapses consecutive "--new" lines.
func DedupNew(body []string) []string {
	var out []string
	for i, l := range body {
		if l == "--new" && i > 0 && body[i-1] == "--new" {
			continue
		}
		out = append(out, l)
	}
	return out
}

// Nochange reports the "#nochange" protection marker (panel must not touch the strategy).
func Nochange(text string) bool { return nochangeRx.MatchString(text) }

// StripNochange removes standalone #nochange lines.
func StripNochange(body []string) []string {
	var out []string
	rx := regexp.MustCompile(`(?i)^[ \t]*#[ \t]*nochange[ \t]*$`)
	for _, l := range body {
		if !rx.MatchString(l) {
			out = append(out, l)
		}
	}
	return out
}

// NormalizePaths converts router / Flowseal paths into the portable relative form.
func NormalizePaths(text string) string {
	r := strings.NewReplacer(
		"/opt/zapret2/files/fake/", FK,
		"/opt/zapret/files/fake/", FK,
		"/opt/zapret2/ipset/", LS,
		"/opt/zapret/ipset/", LS,
	)
	return r.Replace(text)
}

// RouterPaths converts the portable form back for export to an OpenWrt router.
func RouterPaths(text string) string {
	var out []string
	for _, l := range strings.Split(text, "\n") {
		if i := strings.Index(l, "="); i > 0 {
			v := l[i+1:]
			switch {
			case strings.HasPrefix(v, FK):
				l = l[:i+1] + "/opt/zapret/files/fake/" + strings.TrimPrefix(v, FK)
			case strings.HasPrefix(v, LS):
				l = l[:i+1] + "/opt/zapret/ipset/" + strings.TrimPrefix(v, LS)
			}
		}
		out = append(out, l)
	}
	return strings.Join(out, "\n")
}

// Name returns the human strategy label shown in status: non-marker comments joined
// (router status(): "v7", "general (ALT11)", "Custom", plus "QUIC").
func Name(body []string) string {
	var parts []string
	custom := false
	for _, l := range body {
		t := strings.TrimSpace(l)
		if !strings.HasPrefix(t, "#") {
			continue
		}
		n := strings.TrimSpace(strings.TrimPrefix(t, "#"))
		if strings.EqualFold(n, "udp443") || strings.EqualFold(n, "nochange") {
			continue
		}
		if strings.HasPrefix(strings.ToLower(n), "customstart") {
			custom = true
		}
		parts = append(parts, n)
	}
	if custom {
		return "Custom"
	}
	if hasLine(body, "--filter-udp=443") {
		parts = append(parts, "QUIC")
	}
	return strings.Join(parts, " ")
}

// Profiles splits a candidate file into named blocks: "#name" starts a block.
type Block struct {
	Name  string
	Lines []string // including the "#name" line
}

func SplitBlocks(text string, startRx *regexp.Regexp) []Block {
	var out []Block
	for _, l := range Lines(text) {
		if startRx.MatchString(l) {
			out = append(out, Block{Name: strings.TrimSpace(strings.TrimPrefix(l, "#")), Lines: []string{l}})
			continue
		}
		if len(out) > 0 {
			out[len(out)-1].Lines = append(out[len(out)-1].Lines, l)
		}
	}
	return out
}

var reHash = regexp.MustCompile(`^#`)
