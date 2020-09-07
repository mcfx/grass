package grass

import (
	"encoding/binary"
	"fmt"
	"net"

	"github.com/mcfx0/grass/network"
	"github.com/mcfx0/grass/network/packet"
)

func Uint32ToIP(intIP uint32) net.IP {
	bytes := make([]byte, 4)
	binary.BigEndian.PutUint32(bytes, intIP)
	return net.IP(bytes)
}

func IPToUint32(ip net.IP) uint32 {
	return binary.BigEndian.Uint32([]byte(ip.To4()))
}

type PacketIPv4 struct {
	TargetNode   uint32
	TTL          uint8
	HistoryNodes []uint32
	Data         []byte
	IPPacket     *packet.IPv4
}

func (pkt *PacketIPv4) getIPPacket() error {
	pkt.IPPacket = packet.NewIPv4()
	return packet.ParseIPv4(pkt.Data, pkt.IPPacket)
}

func DecodePacketIPv4(buf []byte) (PacketIPv4, error) {
	var pkt PacketIPv4
	if len(buf) < 5 {
		return pkt, fmt.Errorf("incomplete packet")
	}
	pkt.TargetNode = binary.LittleEndian.Uint32(buf[:4])
	pkt.TTL = uint8(buf[4])
	buf = buf[5:]

	nh, n := binary.Uvarint(buf)
	if n > 0 {
		buf = buf[n:]
	} else if n == 0 {
		return pkt, fmt.Errorf("incomplete packet")
	} else {
		return pkt, fmt.Errorf("value larger than 64 bits")
	}

	if len(buf) < 4*int(nh) {
		return pkt, fmt.Errorf("incomplete packet")
	}
	pkt.HistoryNodes = make([]uint32, int(nh))
	for i := 0; i < int(nh); i++ {
		pkt.HistoryNodes[i] = binary.LittleEndian.Uint32(buf[:4])
		buf = buf[4:]
	}
	pkt.Data = buf
	pkt.IPPacket = nil
	return pkt, nil
}

func EncodePacketIPv4(pkt PacketIPv4) []byte {
	buf := make([]byte, 5)
	binary.LittleEndian.PutUint32(buf, pkt.TargetNode)
	buf[4] = byte(pkt.TTL)
	buf = appendUvarint(buf, uint64(len(pkt.HistoryNodes)))
	tmpU32 := make([]byte, 4)
	for i := 0; i < len(pkt.HistoryNodes); i++ {
		binary.LittleEndian.PutUint32(tmpU32, pkt.HistoryNodes[i])
		buf = append(buf, tmpU32...)
	}
	buf = append(buf, pkt.Data...)
	return buf
}

func (client *Client) HandleFinalIPv4(pkt PacketIPv4) error {
	if err := pkt.getIPPacket(); err != nil {
		return err
	}
	if pkt.IPPacket.Protocol == packet.IPProtocolICMPv4 {
		var icmp4src packet.ICMPv4
		if err := packet.ParseICMPv4(pkt.IPPacket.Payload, &icmp4src); err != nil {
			return err
		}
		if icmp4src.Type == 8 { // send icmp reply
			icmp4 := network.ResponseICMPv4Packet(Uint32ToIP(client.IPv4), pkt.IPPacket.SrcIP, 0, 0, icmp4src.Payload)
			return client.HandleTunIPv4(icmp4.Wire, icmp4.Ip)
		}
	}
	pkt.IPPacket.TTL = pkt.TTL
	packets := network.GenFragments(pkt.IPPacket, 0, pkt.IPPacket.Payload)
	for i := 0; i < len(packets); i++ {
		client.Tun.WriteCh <- packets[i]
	}
	return nil
}

func (client *Client) HandlePacketIPv4(pkt PacketIPv4) error {
	if pkt.TargetNode == client.Id {
		return client.HandleFinalIPv4(pkt)
	}
	if pkt.TTL == 0 {
		if err := pkt.getIPPacket(); err != nil {
			return err
		}
		payload := append([]byte{0, 0, 0, 0}, pkt.Data...)
		icmp4 := network.ResponseICMPv4Packet(Uint32ToIP(client.IPv4), pkt.IPPacket.SrcIP, 11, 0, payload)
		return client.HandleTunIPv4(icmp4.Wire, icmp4.Ip)
	}

	client.OClients.M.RLock()
	oc, ok := client.OClients.C[pkt.TargetNode]
	if !ok {
		client.OClients.M.RUnlock()
		return fmt.Errorf("target not reachable")
	}
	client.OClients.M.RUnlock()
	oc.LatencyMutex.RLock()
	n := len(oc.LatencySorted)
	if n == 0 {
		oc.LatencyMutex.RUnlock()
		return fmt.Errorf("target not reachable")
	}
	nh := len(pkt.HistoryNodes)
	qa := make([]uint32, 0)
	qb := make([]uint32, 0)
	for i := 0; i < n; i++ {
		tid := oc.LatencySorted[i].Id
		vis := false
		for j := 0; j < nh; j++ {
			if tid == pkt.HistoryNodes[j] {
				vis = true
			}
		}
		if !vis {
			qa = append(qa, tid)
		} else {
			qb = append(qb, tid)
		}
	}
	oc.LatencyMutex.RUnlock()
	qa = append(qa, qb...)

	pkt.TTL--
	pkt.HistoryNodes = append(pkt.HistoryNodes, client.Id)
	for mqlen := 2; mqlen <= 128; mqlen *= 2 {
		for i := 0; i < len(qa); i++ {
			client.PeerMutex.RLock()
			peer, ok := client.Peers[qa[i]]
			client.PeerMutex.RUnlock()
			//fmt.Printf("%d", peer.Id)
			if ok {
				if peer.SendPacketV2(GrassPacketIPv4, EncodePacketIPv4(pkt), client, mqlen) {
					return nil
				}
			}
		}
	}
	return fmt.Errorf("failed to send out packet")
}

func (client *Client) HandleTunIPv4(data []byte, pkt *packet.IPv4) error {
	var spkt PacketIPv4
	tmp := client.OClients.IPv4ToId(IPToUint32(pkt.DstIP))
	if tmp == 0 {
		return fmt.Errorf("target not reachable")
	}
	spkt.TargetNode = tmp
	spkt.TTL = pkt.TTL
	spkt.Data = data
	spkt.IPPacket = pkt
	return client.HandlePacketIPv4(spkt)
}
