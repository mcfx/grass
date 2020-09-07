package network

import (
	"net"
	"sync"

	"github.com/mcfx0/grass/network/packet"
)

type Icmp4Packet struct {
	Ip     *packet.IPv4
	Icmp4  *packet.ICMPv4
	MtuBuf []byte
	Wire   []byte
}

var (
	Icmp4PacketPool = &sync.Pool{
		New: func() interface{} {
			return &Icmp4Packet{}
		},
	}
)

func newICMPv4Packet() *Icmp4Packet {
	return Icmp4PacketPool.Get().(*Icmp4Packet)
}

func releaseICMPv4Packet(pkt *Icmp4Packet) {
	packet.ReleaseIPv4(pkt.Ip)
	packet.ReleaseICMPv4(pkt.Icmp4)
	if pkt.MtuBuf != nil {
		releaseBuffer(pkt.MtuBuf)
	}
	pkt.MtuBuf = nil
	pkt.Wire = nil
	Icmp4PacketPool.Put(pkt)
}

func ResponseICMPv4Packet(local net.IP, remote net.IP, Type uint8, Code uint8, respPayload []byte) *Icmp4Packet {
	ipid := packet.IPID()

	ip := packet.NewIPv4()
	icmp4 := packet.NewICMPv4()

	ip.Version = 4
	ip.Id = ipid
	ip.SrcIP = make(net.IP, len(local))
	copy(ip.SrcIP, local)
	ip.DstIP = make(net.IP, len(remote))
	copy(ip.DstIP, remote)
	ip.TTL = 64
	ip.Protocol = packet.IPProtocolICMPv4

	icmp4.Type = Type
	icmp4.Code = Code
	icmp4.Payload = respPayload

	pkt := newICMPv4Packet()
	pkt.Ip = ip
	pkt.Icmp4 = icmp4

	pkt.MtuBuf = newBuffer()
	payloadL := len(icmp4.Payload)
	payloadStart := MTU - payloadL
	icmp4HL := 4
	icmp4Start := payloadStart - icmp4HL
	pseduoStart := icmp4Start - packet.IPv4_PSEUDO_LENGTH
	ip.PseudoHeader(pkt.MtuBuf[pseduoStart:icmp4Start], packet.IPProtocolICMPv4, icmp4HL+payloadL)
	icmp4.Serialize(pkt.MtuBuf[icmp4Start:payloadStart], pkt.MtuBuf[icmp4Start:payloadStart], icmp4.Payload)
	if payloadL != 0 {
		copy(pkt.MtuBuf[payloadStart:], icmp4.Payload)
	}
	ipHL := ip.HeaderLength()
	ipStart := icmp4Start - ipHL
	ip.Serialize(pkt.MtuBuf[ipStart:icmp4Start], icmp4HL+(MTU-payloadStart))
	pkt.Wire = pkt.MtuBuf[ipStart:]

	return pkt
}
