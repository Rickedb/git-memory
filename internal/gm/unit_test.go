package gm

import (
	"testing"
)

func TestDisplayVersionDropsTheTagPrefix(t *testing.T) {
	old := Version
	defer func() { Version = old }()
	for in, want := range map[string]string{"0.0.1": "0.0.1", "v0.0.1": "0.0.1", "v1.2.3-rc1": "1.2.3-rc1"} {
		Version = in
		if got := displayVersion(); got != want {
			t.Errorf("displayVersion() with Version=%q = %q, want %q", in, got, want)
		}
	}
}
