package wol

import (
	"encoding/hex"
	"fmt"
	"net"
	"strings"
)

// BuildMagicPacket constructs a WoL magic packet for the given MAC address.
// mac must be colon-separated hex, e.g. "AA:BB:CC:DD:EE:FF" or "aa:bb:cc:dd:ee:ff".
func BuildMagicPacket(mac string) ([]byte, error) {
	parts := strings.Split(mac, ":")
	if len(parts) != 6 {
		return nil, fmt.Errorf("invalid MAC address %q: must be 6 colon-separated hex bytes", mac)
	}
	hw := make([]byte, 6)
	for i, p := range parts {
		b, err := hex.DecodeString(p)
		if err != nil {
			return nil, fmt.Errorf("invalid MAC address %q: %w", mac, err)
		}
		if len(b) != 1 {
			return nil, fmt.Errorf("invalid MAC address %q: segment %q must be exactly 2 hex chars", mac, p)
		}
		hw[i] = b[0]
	}

	pkt := make([]byte, 102)
	for i := 0; i < 6; i++ {
		pkt[i] = 0xFF
	}
	for rep := 0; rep < 16; rep++ {
		copy(pkt[6+rep*6:], hw)
	}
	return pkt, nil
}

// Wake sends a WoL magic packet for mac to the UDP broadcast address on port 9.
func Wake(mac string) error {
	pkt, err := BuildMagicPacket(mac)
	if err != nil {
		return err
	}
	conn, err := net.Dial("udp", "255.255.255.255:9")
	if err != nil {
		return fmt.Errorf("wol: opening UDP socket: %w", err)
	}
	defer conn.Close()
	if _, err = conn.Write(pkt); err != nil {
		return fmt.Errorf("wol: sending packet: %w", err)
	}
	return nil
}
