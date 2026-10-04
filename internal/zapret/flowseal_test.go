package zapret

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// TestFlowsealParity compares the Go parser with the router's own output (ZM_FS_DIR = unpacked
// Flowseal repo, ZM_FS_REF = /tmp/zapret-manager-luci/flowseal_strategies.txt from the router).
func TestFlowsealParity(t *testing.T) {
	dir, ref := os.Getenv("ZM_FS_DIR"), os.Getenv("ZM_FS_REF")
	if dir == "" || ref == "" {
		t.Skip("set ZM_FS_DIR and ZM_FS_REF")
	}
	want := map[string]string{}
	for _, b := range SplitBlocks(NormalizePaths(readFile(t, ref)), reHash) {
		want[b.Name] = Join(b.Lines)
	}
	files, _ := filepath.Glob(filepath.Join(dir, "general*.bat"))
	sort.Strings(files)
	got := 0
	for _, f := range files {
		name := strings.TrimSuffix(filepath.Base(f), ".bat")
		if name == "general (ALT5)" {
			continue
		}
		blk := Join(ParseFlowsealBat(name, readFile(t, f)))
		w, ok := want[name]
		if !ok {
			t.Errorf("router has no %q", name)
			continue
		}
		got++
		if blk != w {
			t.Errorf("%s differs:\n--- go\n%s\n--- router\n%s", name, blk, w)
		}
	}
	if got != len(want) {
		t.Errorf("parsed %d, router has %d", got, len(want))
	}
}

func readFile(t *testing.T, p string) string {
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
