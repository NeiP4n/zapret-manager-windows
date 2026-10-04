package zapret

import (
	"fmt"
	"strings"
)

// Built-in strategy catalogue, identical to the router backend (strategy_v1..v10, Dv1..Dv17,
// strategy_Gv*, strategy_TCP_common) with paths in portable form.

const (
	PortsGameUDP = "88,1024-2407,2409-4499,4502-19293,19345-49999,50101-65535"
	PortsGameTCP = "2099,2802,2302,2502,3478-3480,3724,6000-8000,8085,8090,8100,8903,8904,25565,27015-27030,27036-27037,35500-35600,50001,60442"

	DiscordUDP = "19294-19344,50000-50100"
	DiscordTCP = "2053,2083,2087,2096,8443"

	XtremePorts      = "80,88,444-65535"
	XtremeNfqwsPorts = "80,88,443-65535"
)

const exclHL = "--hostlist-exclude=" + ExcludeList

var vStrategies = map[int][]string{
	1:  {"#v1", "--filter-tcp=443", exclHL, "--dpi-desync=split2", "--dpi-desync-split-seqovl=681", "--dpi-desync-split-seqovl-pattern=" + FK + "stun.bin"},
	2:  {"#v2", "--filter-tcp=443", exclHL, "--dpi-desync=fake,multisplit", "--dpi-desync-split-seqovl=681", "--dpi-desync-split-pos=1", "--dpi-desync-fooling=ts", "--dpi-desync-repeats=8", "--dpi-desync-split-seqovl-pattern=" + FK + "stun.bin", "--dpi-desync-fake-tls-mod=rnd,dupsid,sni=www.google.com"},
	3:  {"#v3", "--filter-tcp=443", exclHL, "--dpi-desync=hostfakesplit", "--dpi-desync-hostfakesplit-mod=host=ozon.ru", "--dpi-desync-repeats=4", "--dpi-desync-fooling=ts,md5sig", "--dpi-desync-badseq-increment=0"},
	4:  {"#v4", "--filter-tcp=443", exclHL, "--dpi-desync=multisplit", "--dpi-desync-split-seqovl=582", "--dpi-desync-split-pos=1", "--dpi-desync-split-seqovl-pattern=" + FK + "stun.bin"},
	5:  {"#v5", "--filter-tcp=443", exclHL, "--dpi-desync=fake,fakeddisorder", "--dpi-desync-split-pos=1", "--dpi-desync-fake-tls=" + FK + "stun.bin", "--dpi-desync-fake-tls-mod=none", "--dpi-desync-fakedsplit-pattern=" + FK + "tls_clienthello_www_google_com.bin", "--dpi-desync-fooling=badseq,badsum", "--dpi-desync-badseq-increment=0"},
	6:  {"#v6", "--filter-tcp=443", exclHL, "--dpi-desync=hostfakesplit", "--dpi-desync-hostfakesplit-mod=host=i2.photo.2gis.com", "--dpi-desync-hostfakesplit-midhost=host-2", "--dpi-desync-split-seqovl=726", "--dpi-desync-fooling=badsum,badseq", "--dpi-desync-badseq-increment=0"},
	7:  {"#v7", "--filter-tcp=443", exclHL, "--dpi-desync=fake,multisplit", "--dpi-desync-split-seqovl=654", "--dpi-desync-split-pos=1", "--dpi-desync-fooling=badseq,badsum", "--dpi-desync-repeats=8", "--dpi-desync-split-seqovl-pattern=" + FK + "stun.bin", "--dpi-desync-fake-tls=" + FK + "stun.bin", "--dpi-desync-badseq-increment=0"},
	8:  {"#v8", "--filter-tcp=443", exclHL, "--dpi-desync=fake", "--dpi-desync-fooling=ts", "--dpi-desync-fake-tls=" + FK + "4pda.bin", "--dpi-desync-fake-tls-mod=none"},
	9:  {"#v9", "--filter-tcp=443", exclHL, "--dpi-desync=hostfakesplit", "--dpi-desync-fooling=badseq,badsum", "--dpi-desync-hostfakesplit-mod=host=ozon.ru", "--dpi-desync-badseq-increment=0"},
	10: {"#v10", "--filter-tcp=443", exclHL, "--dpi-desync=fake,split2", "--dpi-desync-split-pos=2", "--dpi-desync-fake-tls=" + FK + "tls_clienthello_www_google_com.bin", "--dpi-desync-hostfakesplit-mod=host=maxcdn.bootstrapcdn.com", "--dpi-desync-fake-tls-mod=rnd,sni=maxcdn.bootstrapcdn.com", "--dpi-desync-fooling=ts"},
}

func StrategyV(n int) []string { return append([]string(nil), vStrategies[n]...) }

const dvHead = "--filter-tcp=" + DiscordTCP

var dvCommon = []string{dvHead, "--hostlist-domains=discord.media"}

const gtls = FK + "tls_clienthello_www_google_com.bin"

var dvStrategies = map[int][]string{
	1:  {"--dpi-desync=multisplit", "--dpi-desync-split-seqovl=652", "--dpi-desync-split-pos=2", "--dpi-desync-split-seqovl-pattern=" + gtls},
	2:  {"--dpi-desync=fake,multisplit", "--dpi-desync-split-seqovl=681", "--dpi-desync-split-pos=1", "--dpi-desync-fooling=ts", "--dpi-desync-repeats=8", "--dpi-desync-split-seqovl-pattern=" + gtls, "--dpi-desync-fake-tls-mod=rnd,dupsid,sni=www.google.com"},
	3:  {"--dpi-desync=fake", "--dpi-desync-repeats=6", "--dpi-desync-fooling=ts", "--dpi-desync-fake-tls=" + gtls, "--dpi-desync-fake-tls-mod=none"},
	4:  {"--dpi-desync=multisplit", "--dpi-desync-split-seqovl=652", "--dpi-desync-split-pos=2", "--dpi-desync-split-seqovl-pattern=" + gtls},
	5:  {"--dpi-desync=fake,multisplit", "--dpi-desync-repeats=6", "--dpi-desync-fooling=badseq", "--dpi-desync-badseq-increment=1000", "--dpi-desync-fake-tls=" + gtls},
	6:  {"--dpi-desync=multisplit", "--dpi-desync-split-seqovl=681", "--dpi-desync-split-pos=1", "--dpi-desync-split-seqovl-pattern=" + gtls},
	7:  {"--dpi-desync=multisplit", "--dpi-desync-split-pos=2,sniext+1", "--dpi-desync-split-seqovl=679", "--dpi-desync-split-seqovl-pattern=" + gtls},
	8:  {"--dpi-desync=fake", "--dpi-desync-fake-tls-mod=none", "--dpi-desync-repeats=6", "--dpi-desync-fooling=badseq", "--dpi-desync-badseq-increment=2"},
	9:  {"--dpi-desync=fake,fakedsplit", "--dpi-desync-split-pos=1", "--dpi-desync-fooling=badseq", "--dpi-desync-badseq-increment=2", "--dpi-desync-repeats=8", "--dpi-desync-fake-tls-mod=rnd,dupsid,sni=www.google.com"},
	10: {"--dpi-desync=fake,multisplit", "--dpi-desync-split-seqovl=681", "--dpi-desync-split-pos=1", "--dpi-desync-fooling=badseq", "--dpi-desync-badseq-increment=10000000", "--dpi-desync-repeats=8", "--dpi-desync-split-seqovl-pattern=" + gtls, "--dpi-desync-fake-tls-mod=rnd,dupsid,sni=www.google.com"},
	11: {"--dpi-desync=fake,multisplit", "--dpi-desync-split-seqovl=681", "--dpi-desync-split-pos=1", "--dpi-desync-fooling=ts", "--dpi-desync-repeats=8", "--dpi-desync-split-seqovl-pattern=" + gtls, "--dpi-desync-fake-tls-mod=rnd,dupsid,sni=www.google.com"},
	12: {"--dpi-desync=fake", "--dpi-desync-repeats=6", "--dpi-desync-fooling=badseq", "--dpi-desync-badseq-increment=2", "--dpi-desync-fake-tls=" + gtls},
	13: {"--dpi-desync=fake", "--dpi-desync-repeats=6", "--dpi-desync-fooling=ts", "--dpi-desync-fake-tls=" + gtls},
	14: {"--dpi-desync=fake,fakedsplit", "--dpi-desync-repeats=6", "--dpi-desync-fooling=ts", "--dpi-desync-fakedsplit-pattern=0x00", "--dpi-desync-fake-tls=" + gtls},
	15: {"--dpi-desync=fake,multidisorder", "--dpi-desync-split-pos=1,midsld", "--dpi-desync-repeats=11", "--dpi-desync-fooling=badseq", "--dpi-desync-fake-tls=0x00000000", "--dpi-desync-fake-tls=" + gtls, "--dpi-desync-fake-tls-mod=rnd,dupsid,sni=www.google.com"},
	16: {"--dpi-desync=fake,hostfakesplit", "--dpi-desync-fake-tls-mod=rnd,dupsid,sni=www.google.com", "--dpi-desync-hostfakesplit-mod=host=www.google.com,altorder=1", "--dpi-desync-fooling=ts"},
	17: {"--dpi-desync=hostfakesplit", "--dpi-desync-repeats=4", "--dpi-desync-fooling=ts", "--dpi-desync-hostfakesplit-mod=host=www.google.com"},
}

// StrategyDv returns the Discord TCP profile lines (without marker).
func StrategyDv(n int) []string {
	return append(append([]string{}, dvCommon...), dvStrategies[n]...)
}

func strategyTCPCommon() []string {
	return []string{"--new", "--filter-tcp=" + PortsGameTCP, "--dpi-desync-any-protocol=1", "--dpi-desync-cutoff=n5",
		"--dpi-desync=multisplit", "--dpi-desync-split-seqovl=582", "--dpi-desync-split-pos=1",
		"--dpi-desync-split-seqovl-pattern=" + FK + "stun.bin"}
}

func strategyGv(n int) []string {
	if n == 1 {
		return []string{"#Gv1", "--new", "--filter-udp=" + PortsGameUDP, "--dpi-desync=fake", "--dpi-desync-cutoff=d2",
			"--dpi-desync-any-protocol=1", "--dpi-desync-fake-unknown-udp=" + FK + "stun.bin"}
	}
	return []string{fmt.Sprintf("#Gv%d", n), "--new", "--filter-udp=" + PortsGameUDP, "--dpi-desync=fake", "--dpi-desync-repeats=10",
		"--dpi-desync-any-protocol=1", "--dpi-desync-fake-unknown-udp=" + FK + "stun.bin", fmt.Sprintf("--dpi-desync-cutoff=n%d", n)}
}

func yvDefault() []string {
	return []string{"#Yv08", "--filter-tcp=443", "--hostlist=" + GoogleList, "--dpi-desync=hostfakesplit",
		"--dpi-desync-hostfakesplit-mod=host=google.com", "--dpi-desync-fooling=ts", "--new"}
}

func udp443Block() []string {
	return []string{"#udp443", "--filter-udp=443", "--hostlist=" + GoogleList, "--dpi-desync=fake", "--dpi-desync-repeats=11",
		"--dpi-desync-fake-quic=" + FK + "quic_initial_www_google_com.bin", "--new"}
}

func discordBlock() []string {
	return []string{"--new", "--filter-udp=" + DiscordUDP, "--filter-l7=discord,stun",
		"--dpi-desync=fake", "--dpi-desync-fake-discord=" + FK + "stun.bin",
		"--dpi-desync-fake-stun=" + FK + "stun.bin", "--dpi-desync-repeats=6",
		"#Dv1", "--new", dvHead, "--hostlist-domains=discord.media",
		"--dpi-desync=multisplit", "--dpi-desync-split-seqovl=652", "--dpi-desync-split-pos=2",
		"--dpi-desync-split-seqovl-pattern=" + gtls}
}

// FakeFiles that Discord / game profiles may switch between (router discord_set_fake whitelist).
var FakeChoices = []string{"stun.bin", "stun2.bin", "quic_initial_4pda_to.bin", "quic_initial_tencent_com.bin",
	"tls_clienthello_sochi_park.bin", "quic_initial_www_google_com.bin", "quic_initial_steamcommunity_com.bin",
	"quic_initial_5ka_ru.bin", "quic_initial_rutube_ru.bin"}

// Extra fake files the manager pulls from Flowseal (router do_add_fake_flow) and their aliases.
var flowFakes = []string{"stun2.bin", "quic_initial_tencent_com.bin", "quic_initial_steamcommunity_com.bin",
	"tls_clienthello_sochi_park.bin", "quic_initial_4pda_to.bin", "quic_initial_5ka_ru.bin",
	"tls_clienthello_5ka_ru.bin", "quic_initial_rutube_ru.bin"}

// Google Play / YouTube domains always present in the google hostlist (_add_gp_domains).
var gpDomains = []string{"gvt1.com", "googleplay.com", "play.google.com", "beacons.gvt2.com",
	"play.googleapis.com", "play-fe.googleapis.com", "lh3.googleusercontent.com",
	"android.clients.google.com", "connectivitycheck.gstatic.com",
	"play-lh.googleusercontent.com", "play-games.googleusercontent.com",
	"prod-lt-playstoregatewayadapter-pa.googleapis.com", "youtubei.youtube.com"}

// Default google hostlist used when the file does not exist yet (zapret ipset_def + YouTube).
var googleDefaults = strings.Fields(`youtube.com youtu.be ytimg.com ggpht.com googlevideo.com googleapis.com
googleusercontent.com gstatic.com google.com withyoutube.com youtube-nocookie.com youtubeeducation.com
youtubekids.com youtube-ui.l.google.com youtubei.googleapis.com yt.be yt3.ggpht.com`)
