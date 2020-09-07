package grass

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"log"
	"math/rand"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/mcfx0/grass/network"
	"github.com/mcfx0/grass/network/tun"
)

const MAX_PEER_CONNS = 8

type Conn struct {
	Conn         net.Conn
	Reader       *bufio.Reader
	Writer       Writer
	Latency      []uint32
	tmpKeys      *TmpKeySet
	peerTmpKeys  *TmpKeySet
	WriteCh      chan QueuedPacket
	WriterStopCh chan bool
}

type Peer struct {
	Id              uint32
	Conns           []*Conn
	ConnInfo        []string
	ConnsMutex      *sync.RWMutex
	LastSendClients OtherClientsInfo
}

type ListenInfo struct {
	LocalAddr   string
	NetworkAddr string
	Port        uint16
}

type ClientConfig struct {
	Id                   uint32
	Key                  []byte
	IPv4                 uint32
	IPv4Gateway          uint32
	IPv4Mask             uint32
	IPv4DNS              []uint32
	Listen               []ListenInfo
	BootstrapNodes       []string
	CheckClientsInterval int
	PingInterval         int
	CheckConnInterval    int
	Debug                bool
	ConnectNewPeer       bool
	StartTun             bool
}

type Client struct {
	Id                 uint32
	IPv4               uint32
	Key                []byte
	Peers              map[uint32]*Peer
	PeerMutex          *sync.RWMutex
	OClients           OtherClients
	LastCheckClients   OtherClientsInfo
	Tun                *network.TunHandler
	Config             ClientConfig
	ConnInfo           string
	KnownConnInfo      map[string]uint32
	KnownConnInfoMutex *sync.Mutex
	running            bool
}

func (peer *Peer) AvgLatency() uint32 {
	peer.ConnsMutex.RLock()
	tot := 0
	fail := 0
	sum := 0
	for i := 0; i < len(peer.Conns); i++ {
		tmp := peer.Conns[i].Latency
		for j := 0; j < len(tmp); j++ {
			if tmp[j] == 0 {
				fail++
			} else {
				sum += int(tmp[j])
			}
			tot++
		}
	}
	peer.ConnsMutex.RUnlock()
	if tot == fail {
		return 100000000
	}
	res := float64(sum) / float64(tot-fail) / float64(tot-fail) * float64(tot)
	resn := int(res)
	if resn < 2 {
		return 2
	}
	if resn > 100000000 {
		return 100000000
	}
	return uint32(resn)
}

func (peer *Peer) SendPacket(tp uint8, data []byte, client *Client) error {
	// a naive implementation, need to be improved
	peer.ConnsMutex.RLock()
	defer peer.ConnsMutex.RUnlock()
	if len(peer.Conns) == 0 {
		return fmt.Errorf("no usable connections")
	}
	//return WriteRawPacket(tp, data, peer.Conns[rand.Intn(len(peer.Conns))], client)
	peer.Conns[rand.Intn(len(peer.Conns))].WriteCh <- QueuedPacket{Type: tp, Data: data}
	return nil
}

func (peer *Peer) SendPacketV2(tp uint8, data []byte, client *Client, maxQlen int) bool {
	peer.ConnsMutex.RLock()
	defer peer.ConnsMutex.RUnlock()
	if len(peer.Conns) == 0 {
		return false
	}
	for i := 0; i < len(peer.Conns); i++ {
		//fmt.Printf("%d ", len(peer.Conns[i].WriteCh))
		if len(peer.Conns[i].WriteCh) <= maxQlen {
			peer.Conns[i].WriteCh <- QueuedPacket{Type: tp, Data: data}
			return true
		}
	}
	return false
}

func (client *Client) HandleConn(rawConn net.Conn, connInfo string) error {
	var conn Conn
	conn.Conn = rawConn
	conn.Reader = bufio.NewReader(rawConn)
	conn.Writer = NewWriter(rawConn)
	conn.tmpKeys = NewTmpKeySet(120)
	conn.peerTmpKeys = NewTmpKeySet(120)
	conn.WriteCh = make(chan QueuedPacket, 300)
	conn.WriterStopCh = make(chan bool, 10)

	var pkt Packet
	pkt.Type = GrassPacketInfo
	pkt.Content = make([]byte, 4)
	binary.LittleEndian.PutUint32(pkt.Content, client.Id)
	err := WritePacket(&pkt, &conn, client)
	if err != nil {
		return err
	}

	var peerId uint32
	for {
		err := ReadPacket(&pkt, &conn, client)
		if err != nil {
			return err
		}
		if pkt.Type == GrassPacketInfo {
			if len(pkt.Content) != 4 {
				return fmt.Errorf("Unexpected length of info packet")
			}
			peerId = binary.LittleEndian.Uint32(pkt.Content)
			break
		}
	}
	log.Printf("Connected to peer %08x\n", peerId)
	if connInfo != "" {
		client.KnownConnInfoMutex.Lock()
		client.KnownConnInfo[connInfo] = peerId
		client.KnownConnInfoMutex.Unlock()
	}

	client.PeerMutex.Lock()
	peer, ok := client.Peers[peerId]
	if !ok {
		peer = &Peer{Id: peerId, ConnsMutex: &sync.RWMutex{}}
		peer.LastSendClients = make(OtherClientsInfo)
		if client.Peers == nil {
			client.Peers = make(map[uint32]*Peer)
		}
		client.Peers[peerId] = peer
	}
	client.PeerMutex.Unlock()
	peer.ConnsMutex.Lock()
	peer.Conns = append(peer.Conns, &conn)
	peer.ConnsMutex.Unlock()

	working := true
	var pingKey uint32 = 0
	var pingTime int64 = 0

	go func() {
		for working {
			changed := false
			if pingTime != 0 {
				conn.Latency = append(conn.Latency, 0)
				changed = true
			}
			if len(conn.Latency) > 10 {
				conn.Latency = conn.Latency[len(conn.Latency)-10:]
				changed = true
			}
			if changed {
				go client.OClients.UpdateLatencyAdd(peerId, peer.AvgLatency())
			}
			pingKey = rand.Uint32()
			pingTime = time.Now().UnixNano()
			tmp := make([]byte, 4)
			binary.LittleEndian.PutUint32(tmp, pingKey)
			if err := WriteRawPacket(GrassPacketPing, tmp, &conn, client); err != nil && client.Config.Debug {
				log.Printf("Error in ping thread: %v\n", err)
				return
			}
			peer.ConnsMutex.RLock()
			wait := int(client.Config.PingInterval) * len(peer.Conns)
			peer.ConnsMutex.RUnlock()
			if len(conn.Latency) < 10 {
				wait /= 10
			}
			wait += rand.Intn(10) - 5
			if wait < 0 {
				wait = 0
			}
			time.Sleep(time.Second * time.Duration(wait))
		}
	}()

	go func() {
		for {
			select {
			case pkt := <-conn.WriteCh:
				WriteRawPacket(pkt.Type, pkt.Data, &conn, client)
			case <-conn.WriterStopCh:
				return
			}
		}
	}()

	closeConn := func() {
		working = false
		peer.ConnsMutex.Lock()
		i := 0
		for ; i < len(peer.Conns); i++ {
			if peer.Conns[i] == &conn {
				break
			}
		}
		if i != len(peer.Conns) {
			peer.Conns[i] = peer.Conns[len(peer.Conns)-1]
			peer.Conns = peer.Conns[:len(peer.Conns)-1]
		}
		peer.ConnsMutex.Unlock()
		if len(peer.Conns) == 0 {
			client.PeerMutex.Lock()
			delete(client.Peers, peer.Id)
			client.PeerMutex.Unlock()
			client.OClients.ClearClients(peerId)
		}
		log.Printf("Disconnected from peer %08x\n", peerId)
		rawConn.Close()
		conn.WriterStopCh <- true
	}

	for {
		err := ReadPacket(&pkt, &conn, client)
		if err != nil {
			closeConn()
			return err
		}
		if client.Config.Debug {
			log.Printf("Packet from %08x: %d %v\n", peerId, pkt.Type, pkt.Content)
		}
		switch pkt.Type {
		case GrassPacketIPv4:
			tmp, err := DecodePacketIPv4(pkt.Content)
			if err != nil {
				return fmt.Errorf("Error in IPv4 packet: %v", err)
			}
			go client.HandlePacketIPv4(tmp)
		case GrassPacketPing:
			if len(pkt.Content) != 4 {
				return fmt.Errorf("Unexpected length of ping packet")
			}
			if err = WriteRawPacket(GrassPacketPong, pkt.Content, &conn, client); err != nil {
				return err
			}
		case GrassPacketInfo:
			continue
		case GrassPacketPong:
			if len(pkt.Content) != 4 {
				return fmt.Errorf("Unexpected length of pong packet")
			}
			tKey := binary.LittleEndian.Uint32(pkt.Content)
			if tKey == pingKey {
				latency := uint32((time.Now().UnixNano() - pingTime) / 1000)
				if latency == 0 {
					latency = 1
				}
				conn.Latency = append(conn.Latency, latency)
				pingKey = 0
				pingTime = 0
				go client.OClients.UpdateLatencyAdd(peerId, peer.AvgLatency())
			}
		case GrassPacketClients:
			tmp, err := DecodePacketClients(pkt.Content)
			if client.Config.Debug {
				log.Printf("Packet Clients from %08x: %v\n", peerId, tmp)
			}
			if err != nil {
				return fmt.Errorf("Error in Clients packet: %v", err)
			}
			go client.OClients.UpdateClients(tmp, peerId, peer.AvgLatency())
		case GrassPacketFarPing:
			tmp, err := DecodePacketFarPing(pkt.Content)
			if err != nil {
				return fmt.Errorf("Error in FarPing packet: %v", err)
			}
			go client.HandlePacketFarPing(tmp)
		case GrassPacketExpired:
			continue
		default:
			closeConn()
			return fmt.Errorf("Unknown packet type")
		}
	}

	return nil
}

func (client *Client) CheckClients() {
	lstLatency := make(map[uint32]uint32)
	latencyIncreaseCnt := make(map[uint32]int)
	for client.running {
		time.Sleep(time.Second * time.Duration(client.Config.CheckClientsInterval))
		var peers []*Peer
		client.PeerMutex.RLock()
		for _, pr := range client.Peers {
			peers = append(peers, pr)
		}
		client.PeerMutex.RUnlock()

		tmp := make(OtherClientsInfo)
		/*if client.Config.Debug {
			fmt.Println("peers:")
		}*/
		for i := 0; i < len(peers); i++ {
			/*if client.Config.Debug {
				fmt.Printf("id=%08x lat=%v avgLat=%d\n", peers[i].Id, peers[i].Conns[0].Latency, peers[i].AvgLatency())
			}*/
			tmp[peers[i].Id] = OtherClientInfo{ConnInfo: "", IPv4: 0, Latency: peers[i].AvgLatency()}
		}
		tmp[client.Id] = OtherClientInfo{ConnInfo: client.ConnInfo, IPv4: client.IPv4, Latency: 1}
		tmpt, useful := GenDiffPacketClients(tmp, client.LastCheckClients)
		client.OClients.UpdateClients(tmpt, client.Id, 0)
		client.LastCheckClients = tmp

		tmp = make(OtherClientsInfo)
		var ocs []*OtherClient
		client.OClients.M.Lock()
		for _, oc := range client.OClients.C {
			ocs = append(ocs, oc)
		}
		client.OClients.M.Unlock()
		if client.Config.Debug {
			log.Println("clients:")
		}
		for i := 0; i < len(ocs); i++ {
			if client.Config.Debug {
				log.Printf("id=%08x conn=%s ipv4=%08x lat=%v latSort=%v\n", ocs[i].Id, ocs[i].ConnInfoStr, ocs[i].IPv4, ocs[i].Latency, ocs[i].LatencySorted)
			}
			lat := ocs[i].GetLatency()
			if llat, ok := lstLatency[ocs[i].Id]; ok {
				if lat > llat {
					t, ok := latencyIncreaseCnt[ocs[i].Id]
					if !ok {
						t = 0
					}
					t++
					if t < 10 {
						latencyIncreaseCnt[ocs[i].Id] = t
					} else {
						go func(id uint32) {
							var pkt PacketFarPing
							pkt.SrcNode = client.Id
							pkt.DstNode = id
							pkt.Status = FarPingRequest
							pkt.HistoryNodes = make([]uint32, 0)
							client.HandlePacketFarPing(pkt)
						}(ocs[i].Id)
						latencyIncreaseCnt[ocs[i].Id] = 0
					}
				} else if lat < llat {
					latencyIncreaseCnt[ocs[i].Id] = 0
				}
			}
			lstLatency[ocs[i].Id] = lat
			if !client.OClients.BannedClients.HasKey(ocs[i].Id) {
				tmp[ocs[i].Id] = OtherClientInfo{ConnInfo: ocs[i].ConnInfoStr, IPv4: ocs[i].IPv4, Latency: lat}
			}
		}

		for i := 0; i < len(peers); i++ {
			if rand.Intn(120) == 23 {
				peers[i].LastSendClients = make(OtherClientsInfo)
			}
			tmpt, useful = GenDiffPacketClients(tmp, peers[i].LastSendClients)
			peers[i].LastSendClients = tmp
			if useful {
				err := peers[i].SendPacket(GrassPacketClients, EncodePacketClients(tmpt), client)
				if err != nil {
					log.Printf("Error while sending Clients packet to %08x: %v\n", peers[i].Id, err)
				}
			}
		}

		if client.Config.ConnectNewPeer {
			client.KnownConnInfoMutex.Lock()
			for i := 0; i < len(ocs); i++ {
				tc := ocs[i].ConnInfo
				if ocs[i].Id == client.Id {
					continue
				}
				for j := 0; j < len(tc); j++ {
					client.KnownConnInfo[tc[j]] = ocs[i].Id
				}
			}
			client.KnownConnInfoMutex.Unlock()
		}
	}
}

func (client *Client) CheckConn() {
	for client.running {
		time.Sleep(time.Second * time.Duration(client.Config.CheckConnInterval))

		tmap := make(map[uint32][]string)

		addConn := func(id uint32, ci string) {
			_, ok := tmap[id]
			if !ok {
				tmap[id] = nil
			}
			tmap[id] = append(tmap[id], ci)
		}

		ct := make([]struct {
			connInfo string
			cid      uint32
		}, 0)
		client.KnownConnInfoMutex.Lock()
		for connInfo, cid := range client.KnownConnInfo {
			ct = append(ct, struct {
				connInfo string
				cid      uint32
			}{connInfo: connInfo, cid: cid})
		}
		client.KnownConnInfoMutex.Unlock()
		for i := 0; i < len(ct); i++ {
			connInfo := ct[i].connInfo
			cid := ct[i].cid
			client.PeerMutex.RLock()
			peer, ok := client.Peers[cid]
			if !ok {
				addConn(cid, connInfo)
				client.PeerMutex.RUnlock()
				continue
			}
			client.PeerMutex.RUnlock()
			peer.ConnsMutex.RLock()
			//log.Printf("conninfo: %v %v %v\n", connInfo, cid, peer)
			if len(peer.Conns) < MAX_PEER_CONNS {
				addConn(cid, connInfo)
			}
			peer.ConnsMutex.RUnlock()
		}

		for _, cis := range tmap {
			go func(s string) {
				conn, err := net.Dial("tcp", s)
				if err != nil {
					return
				}
				err = client.HandleConn(conn, s)
				if err != nil && client.Config.Debug {
					log.Printf("Error during handling conn: %v\n", err)
				}
			}(cis[rand.Intn(len(cis))])
		}
	}
}

func (client *Client) Start() error {
	rand.Seed(time.Now().UnixNano())
	log.Println("Starting...")
	client.Id = client.Config.Id
	client.IPv4 = client.Config.IPv4
	client.Key = client.Config.Key

	client.PeerMutex = &sync.RWMutex{}
	client.KnownConnInfoMutex = &sync.Mutex{}
	client.KnownConnInfo = make(map[string]uint32)
	client.LastCheckClients = make(OtherClientsInfo)
	client.OClients.Init(client.Config.CheckClientsInterval)
	client.running = true
	client.ConnInfo = ""

	if client.Config.StartTun {
		tmpIP := Uint32ToIP(client.IPv4).String()
		tmpGW := Uint32ToIP(client.Config.IPv4Gateway).String()
		tmpMask := Uint32ToIP(client.Config.IPv4Mask).String()
		tmpDNS := make([]string, 0)
		for i := 0; i < len(client.Config.IPv4DNS); i++ {
			tmpDNS = append(tmpDNS, Uint32ToIP(client.Config.IPv4DNS[i]).String())
		}
		tun, err := tun.OpenTunDevice("tun0", tmpIP, tmpGW, tmpMask, tmpDNS)
		if err != nil {
			return err
		}
		client.Tun = network.New(tun, client.HandleTunIPv4)
		go client.Tun.Run()
	}

	for i := 0; i < len(client.Config.Listen); i++ {
		go func(s ListenInfo) {
			listenAddr := s.LocalAddr + ":" + strconv.Itoa(int(s.Port))
			if client.ConnInfo != "" {
				client.ConnInfo += ","
			}
			client.ConnInfo += s.NetworkAddr + ":" + strconv.Itoa(int(s.Port))
			listener, err := net.Listen("tcp", listenAddr)
			if err != nil {
				log.Printf("Error during listening %s: %v\n", listenAddr, err)
				return
			}
			for client.running {
				conn, err := listener.Accept()
				if err != nil {
					continue
				}
				go func() {
					err := client.HandleConn(conn, "")
					if err != nil && client.Config.Debug {
						log.Printf("Error during handling conn: %v\n", err)
					}
				}()
			}
		}(client.Config.Listen[i])
	}

	for i := 0; i < len(client.Config.BootstrapNodes); i++ {
		go func(s string) {
			conn, err := net.Dial("tcp", s)
			if err != nil {
				log.Println(err)
				return
			}
			err = client.HandleConn(conn, s)
			if err != nil && client.Config.Debug {
				log.Printf("Error during handling conn: %v\n", err)
			}
		}(client.Config.BootstrapNodes[i])
	}

	go client.CheckClients()
	go client.CheckConn()
	return nil
}

func (client *Client) Stop() {
	client.running = false
	log.Println("Shutting down...")
	// close connections
}
