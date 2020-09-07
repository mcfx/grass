package grass

import (
	"encoding/binary"
	"fmt"
)

type PacketClientsString struct {
	Val string
	Ok  bool
}

type PacketClientsStringArr []PacketClientsString

type PacketClientsUint32 struct {
	Val uint32
	Ok  bool
}

type PacketClientsUint32Arr []PacketClientsUint32

func (a PacketClientsStringArr) SetOk(n int) {
	a[n].Ok = true
}

func (a PacketClientsStringArr) GetOk(n int) bool {
	return a[n].Ok
}

func (a PacketClientsUint32Arr) SetOk(n int) {
	a[n].Ok = true
}

func (a PacketClientsUint32Arr) GetOk(n int) bool {
	return a[n].Ok
}

type PacketClientsOkTypeArr interface {
	SetOk(int)
	GetOk(int) bool
}

type PacketClients struct {
	Count    uint64
	Ids      []uint32
	ConnInfo PacketClientsStringArr
	IPv4     PacketClientsUint32Arr
	Latency  PacketClientsUint32Arr
}

func DecodePacketClients(buf []byte) (PacketClients, error) {
	var pkt PacketClients
	var n int
	pkt.Count, n = binary.Uvarint(buf)
	if n > 0 {
		buf = buf[n:]
	} else if n == 0 {
		return pkt, fmt.Errorf("incomplete packet")
	} else {
		return pkt, fmt.Errorf("value larger than 64 bits")
	}
	n = int(pkt.Count)
	pkt.Ids = make([]uint32, n)
	pkt.ConnInfo = make([]PacketClientsString, n)
	pkt.IPv4 = make([]PacketClientsUint32, n)
	pkt.Latency = make([]PacketClientsUint32, n)
	if len(buf) < n*4 {
		return pkt, fmt.Errorf("incomplete packet")
	}
	for i := 0; i < n; i++ {
		pkt.Ids[i] = binary.LittleEndian.Uint32(buf[:4])
		buf = buf[4:]
	}

	n8 := (n + 7) / 8
	readOkType := func(s PacketClientsOkTypeArr) error {
		if len(buf) < n8 {
			return fmt.Errorf("incomplete packet")
		}
		for i := 0; i < n; i++ {
			if (int(buf[i>>3]) >> (i & 7) & 1) == 1 {
				s.SetOk(i)
			}
		}
		return nil
	}

	readString := func() (string, error) {
		tlen, nt := binary.Uvarint(buf)
		if nt > 0 {
			buf = buf[nt:]
		} else if nt == 0 {
			return "", fmt.Errorf("incomplete packet")
		} else {
			return "", fmt.Errorf("value larger than 64 bits")
		}
		if len(buf) < int(tlen) {
			return "", fmt.Errorf("incomplete packet")
		}
		defer func() { buf = buf[tlen:] }()
		return string(buf[:tlen]), nil
	}

	readOkType(pkt.ConnInfo)
	buf = buf[n8:]
	var err error
	for i := 0; i < n; i++ {
		if pkt.ConnInfo[i].Ok {
			pkt.ConnInfo[i].Val, err = readString()
			if err != nil {
				return pkt, err
			}
		}
	}

	readOkType(pkt.IPv4)
	buf = buf[n8:]
	for i := 0; i < n; i++ {
		if pkt.IPv4[i].Ok {
			if len(buf) < 4 {
				return pkt, fmt.Errorf("incomplete packet")
			}
			pkt.IPv4[i].Val = binary.LittleEndian.Uint32(buf[:4])
			buf = buf[4:]
		}
	}

	readOkType(pkt.Latency)
	buf = buf[n8:]
	for i := 0; i < n; i++ {
		if pkt.Latency[i].Ok {
			if len(buf) < 4 {
				return pkt, fmt.Errorf("incomplete packet")
			}
			pkt.Latency[i].Val = binary.LittleEndian.Uint32(buf[:4])
			buf = buf[4:]
		}
	}
	if len(buf) != 0 {
		return pkt, fmt.Errorf("extra data after packet")
	}

	return pkt, nil
}

func appendUvarint(s []byte, x uint64) []byte {
	n := len(s)
	s = append(s, make([]byte, binary.MaxVarintLen64)...)
	n += binary.PutUvarint(s[n:], x)
	return s[:n]
}

func EncodePacketClients(pkt PacketClients) []byte {
	var buf []byte
	buf = appendUvarint(buf, pkt.Count)
	n := int(pkt.Count)
	tmpU32 := make([]byte, 4)
	for i := 0; i < n; i++ {
		binary.LittleEndian.PutUint32(tmpU32, pkt.Ids[i])
		buf = append(buf, tmpU32...)
	}

	n8 := (n + 7) / 8
	writeOkType := func(s PacketClientsOkTypeArr) {
		tmp := make([]byte, n8)
		for i := 0; i < n; i++ {
			if s.GetOk(i) {
				tmp[i>>3] = tmp[i>>3] | byte(1<<(i&7))
			}
		}
		buf = append(buf, tmp...)
	}

	writeOkType(pkt.ConnInfo)
	for i := 0; i < n; i++ {
		if pkt.ConnInfo[i].Ok {
			buf = appendUvarint(buf, uint64(len(pkt.ConnInfo[i].Val)))
			buf = append(buf, pkt.ConnInfo[i].Val...)
		}
	}

	writeOkType(pkt.IPv4)
	for i := 0; i < n; i++ {
		if pkt.IPv4[i].Ok {
			binary.LittleEndian.PutUint32(tmpU32, pkt.IPv4[i].Val)
			buf = append(buf, tmpU32...)
		}
	}

	writeOkType(pkt.Latency)
	for i := 0; i < n; i++ {
		if pkt.Latency[i].Ok {
			binary.LittleEndian.PutUint32(tmpU32, pkt.Latency[i].Val)
			buf = append(buf, tmpU32...)
		}
	}

	return buf
}

type OtherClientInfo struct {
	ConnInfo string
	IPv4     uint32
	Latency  uint32
}

type OtherClientsInfo map[uint32]OtherClientInfo

func GenDiffPacketClients(cur, old OtherClientsInfo) (PacketClients, bool) {
	var pkt PacketClients
	n := len(cur)
	pkt.Count = uint64(n)
	pkt.Ids = make([]uint32, n)
	pkt.ConnInfo = make([]PacketClientsString, n)
	pkt.IPv4 = make([]PacketClientsUint32, n)
	pkt.Latency = make([]PacketClientsUint32, n)
	useful := len(cur) != len(old)
	i := 0
	for id, oc := range cur {
		pkt.Ids[i] = id
		oc2, ok := old[id]
		useful = useful || !ok
		if ok {
			if oc.ConnInfo != oc2.ConnInfo && oc.ConnInfo != "" {
				pkt.ConnInfo[i].Ok = true
				pkt.ConnInfo[i].Val = oc.ConnInfo
			}
			if oc.IPv4 != oc2.IPv4 && oc.IPv4 != 0 {
				pkt.IPv4[i].Ok = true
				pkt.IPv4[i].Val = oc.IPv4
			}
			if oc.Latency != oc2.Latency {
				pkt.Latency[i].Ok = true
				pkt.Latency[i].Val = oc.Latency
			}
		} else {
			if oc.ConnInfo != "" {
				pkt.ConnInfo[i].Ok = true
				pkt.ConnInfo[i].Val = oc.ConnInfo
			}
			if oc.IPv4 != 0 {
				pkt.IPv4[i].Ok = true
				pkt.IPv4[i].Val = oc.IPv4
			}
			pkt.Latency[i].Ok = true
			pkt.Latency[i].Val = oc.Latency
		}
		useful = useful || pkt.ConnInfo[i].Ok || pkt.IPv4[i].Ok || pkt.Latency[i].Ok
		i += 1
	}
	return pkt, useful
}
