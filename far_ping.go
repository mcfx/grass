package grass

import (
	"encoding/binary"
	"fmt"
)

const (
	FarPingRequest uint32 = 0
	FarPingReply   uint32 = 1
	FarPingCircuit uint32 = 2
)

type PacketFarPing struct {
	SrcNode      uint32
	DstNode      uint32
	Status       uint32
	HistoryNodes []uint32
}

func DecodePacketFarPing(buf []byte) (PacketFarPing, error) {
	var pkt PacketFarPing
	if len(buf) < 12 {
		return pkt, fmt.Errorf("incomplete packet")
	}
	pkt.SrcNode = binary.LittleEndian.Uint32(buf[:4])
	pkt.DstNode = binary.LittleEndian.Uint32(buf[4:8])
	pkt.Status = binary.LittleEndian.Uint32(buf[8:12])
	buf = buf[12:]

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
	return pkt, nil
}

func EncodePacketFarPing(pkt PacketFarPing) []byte {
	buf := make([]byte, 12)
	binary.LittleEndian.PutUint32(buf[:4], pkt.SrcNode)
	binary.LittleEndian.PutUint32(buf[4:8], pkt.DstNode)
	binary.LittleEndian.PutUint32(buf[8:12], pkt.Status)
	buf = appendUvarint(buf, uint64(len(pkt.HistoryNodes)))
	tmpU32 := make([]byte, 4)
	for i := 0; i < len(pkt.HistoryNodes); i++ {
		binary.LittleEndian.PutUint32(tmpU32, pkt.HistoryNodes[i])
		buf = append(buf, tmpU32...)
	}
	return buf
}

func (client *Client) HandlePacketFarPing(pkt PacketFarPing) error {
	if pkt.DstNode == client.Id {
		if pkt.Status == FarPingReply {
			return nil
		}
		if pkt.Status == FarPingRequest {
			var reply PacketFarPing
			reply.DstNode = pkt.SrcNode
			reply.SrcNode = client.Id
			reply.Status = FarPingReply
			reply.HistoryNodes = make([]uint32, 0)
			return client.HandlePacketFarPing(reply)
		}
		if pkt.Status == FarPingCircuit {
			client.OClients.BannedClients.AddKey(pkt.SrcNode)
			return nil
		}
		return nil
	}

	client.OClients.M.RLock()
	oc, ok := client.OClients.C[pkt.DstNode]
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
	var nextId uint32
	for i := 0; i < n; i++ {
		tid := oc.LatencySorted[i].Id
		vis := false
		for j := 0; j < nh; j++ {
			if tid == pkt.HistoryNodes[j] {
				vis = true
			}
		}
		if !vis {
			nextId = tid
			break
		}
	}
	if nextId == 0 {
		var reply PacketFarPing
		reply.DstNode = pkt.SrcNode
		reply.SrcNode = pkt.DstNode
		//reply.SrcNode = client.Id
		reply.Status = FarPingCircuit
		reply.HistoryNodes = make([]uint32, 0)
		oc.LatencyMutex.RUnlock()
		return client.HandlePacketFarPing(reply)
	}
	if nextId == client.Id {
		nextId = pkt.DstNode
	}
	oc.LatencyMutex.RUnlock()

	client.PeerMutex.RLock()
	peer, ok := client.Peers[nextId]
	client.PeerMutex.RUnlock()
	if !ok {
		return fmt.Errorf("target conn closed unexpectedly")
	}
	pkt.HistoryNodes = append(pkt.HistoryNodes, client.Id)
	return peer.SendPacket(GrassPacketFarPing, EncodePacketFarPing(pkt), client)
}
