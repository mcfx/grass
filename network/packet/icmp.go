package packet

import (
	"encoding/binary"
	"fmt"
	"sync"
)

type ICMPv4 struct {
	Type     uint8
	Code     uint8
	Checksum uint16
	Payload  []byte
}

var (
	icmp4Pool *sync.Pool = &sync.Pool{
		New: func() interface{} {
			return &ICMPv4{}
		},
	}
)

func NewICMPv4() *ICMPv4 {
	var zero ICMPv4
	icmp4 := icmp4Pool.Get().(*ICMPv4)
	*icmp4 = zero
	return icmp4
}

func ReleaseICMPv4(icmp4 *ICMPv4) {
	// clear internal slice references
	icmp4.Payload = nil
	icmp4Pool.Put(icmp4)
}

func ParseICMPv4(pkt []byte, icmp4 *ICMPv4) error {
	if len(pkt) < 4 {
		return fmt.Errorf("payload too small for ICMPv4: %d bytes", len(pkt))
	}

	icmp4.Type = uint8(pkt[0])
	icmp4.Code = uint8(pkt[1])
	icmp4.Checksum = binary.BigEndian.Uint16(pkt[2:4])
	if len(pkt) > 4 {
		icmp4.Payload = pkt[4:]
	} else {
		icmp4.Payload = nil
	}

	return nil
}

func (icmp4 *ICMPv4) Serialize(hdr []byte, ckFields ...[]byte) error {
	if len(hdr) != 4 {
		return fmt.Errorf("incorrect buffer size: %d buffer given, 4 needed", len(hdr))
	}
	hdr[0] = byte(icmp4.Type)
	hdr[1] = byte(icmp4.Code)
	hdr[2] = 0
	hdr[3] = 0
	icmp4.Checksum = Checksum(ckFields...)
	binary.BigEndian.PutUint16(hdr[2:], icmp4.Checksum)
	return nil
}
