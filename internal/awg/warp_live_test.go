package awg

import (
	"context"
	"os"
	"testing"
)

func TestWarpRegisterLive(t *testing.T) {
	if os.Getenv("ZM_LIVE") == "" {
		t.Skip()
	}
	k, err := Register(context.Background(), t.Logf)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("v4=%s v6=%s peer=%s", k.V4, k.V6, k.Peer)
	t.Log(WarpConfText(k, Endpoints[0], I1("dns"), true))
}
