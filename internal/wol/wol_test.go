package wol_test

import (
	"testing"

	"github.com/jamietre/reveillm/internal/wol"
)

func TestBuildMagicPacket_valid(t *testing.T) {
	pkt, err := wol.BuildMagicPacket("AA:BB:CC:DD:EE:FF")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pkt) != 102 {
		t.Fatalf("want 102 bytes, got %d", len(pkt))
	}
	// first 6 bytes must be 0xFF
	for i := 0; i < 6; i++ {
		if pkt[i] != 0xFF {
			t.Errorf("byte %d: want 0xFF, got 0x%02X", i, pkt[i])
		}
	}
	// MAC repeated 16 times starting at byte 6
	mac := []byte{0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF}
	for rep := 0; rep < 16; rep++ {
		for i, b := range mac {
			off := 6 + rep*6 + i
			if pkt[off] != b {
				t.Errorf("rep %d byte %d: want 0x%02X, got 0x%02X", rep, i, b, pkt[off])
			}
		}
	}
}

func TestBuildMagicPacket_invalidMAC(t *testing.T) {
	cases := []string{"", "ZZ:ZZ:ZZ:ZZ:ZZ:ZZ", "AA:BB:CC:DD:EE", "not-a-mac"}
	for _, tc := range cases {
		_, err := wol.BuildMagicPacket(tc)
		if err == nil {
			t.Errorf("input %q: expected error, got nil", tc)
		}
	}
}
