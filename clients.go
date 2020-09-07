package grass

import (
	"sort"
	"strings"
	"sync"
)

type LatencyPair struct {
	Id      uint32
	Latency uint32
}

type pair struct {
	a uint32
	b uint32
}

type OtherClient struct {
	Id            uint32
	IPv4          uint32
	Latency       map[uint32]uint32 // connId->latency
	LatencyAdd    map[uint32]uint32
	LatencySorted []LatencyPair
	LatencyMutex  *sync.RWMutex
	ConnInfo      []string
	ConnInfoStr   string
}

type OtherClients struct {
	C             map[uint32]*OtherClient
	M             *sync.RWMutex
	IPv4Map       map[uint32]uint32
	BannedClients *TmpKeySet
}

type pairArray []pair

func (s pairArray) Len() int {
	return len(s)
}

func (s pairArray) Less(i, j int) bool {
	return s[i].a < s[j].a
}

func (s pairArray) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}

func (oc *OtherClient) SortLatency() {
	oc.LatencyMutex.RLock()
	var tmp pairArray
	for vid, la := range oc.Latency {
		tmp = append(tmp, pair{a: la, b: vid})
	}
	oc.LatencyMutex.RUnlock()
	sort.Sort(tmp)
	var tmp2 []LatencyPair
	for i := 0; i < len(tmp); i++ {
		tmp2 = append(tmp2, LatencyPair{Id: tmp[i].b, Latency: tmp[i].a})
	}
	oc.LatencyMutex.Lock()
	oc.LatencySorted = tmp2
	oc.LatencyMutex.Unlock()
}

func (oc *OtherClient) GetLatency() uint32 {
	oc.LatencyMutex.RLock()
	defer oc.LatencyMutex.RUnlock()
	if len(oc.Latency) == 0 {
		return 100000000
	}
	return oc.LatencySorted[0].Latency
}

func (c *OtherClients) Init(checkInterval int) {
	c.C = make(map[uint32]*OtherClient)
	c.M = &sync.RWMutex{}
	c.IPv4Map = make(map[uint32]uint32)
	c.BannedClients = NewTmpKeySet(uint(checkInterval * 10))
}

func (c *OtherClients) IPv4ToId(ipv4 uint32) uint32 {
	c.M.RLock()
	defer c.M.RUnlock()
	res, ok := c.IPv4Map[ipv4]
	if ok {
		return res
	}
	return 0
}

func (c *OtherClients) UpdateClient(id, ipv4, viaId, latency, latencyAdd uint32) {
	c.M.Lock()
	oc, ok := c.C[id]
	if !ok {
		oc = &OtherClient{Id: id, IPv4: ipv4, Latency: make(map[uint32]uint32), LatencyAdd: make(map[uint32]uint32), LatencyMutex: &sync.RWMutex{}}
		c.C[id] = oc
		if ipv4 != 0 {
			c.IPv4Map[ipv4] = id
		}
	}
	c.M.Unlock()
	if oc.IPv4 == 0 && ipv4 != 0 {
		oc.IPv4 = ipv4
		c.M.Lock()
		c.IPv4Map[ipv4] = id
		c.M.Unlock()
	}
	if viaId == 0 {
		return
	}
	oc.LatencyMutex.Lock()
	if latency == 0 {
		delete(oc.Latency, viaId)
		delete(oc.LatencyAdd, viaId)
	} else if !c.BannedClients.HasKey(id) {
		oc.Latency[viaId] = latency
		oc.LatencyAdd[viaId] = latencyAdd
	}
	oc.LatencyMutex.Unlock()
	oc.SortLatency()
}

func (c *OtherClients) UpdateClientConnInfo(id uint32, connInfo string) {
	if c.BannedClients.HasKey(id) {
		return
	}
	c.M.Lock()
	oc, ok := c.C[id]
	if !ok {
		oc = &OtherClient{Id: id, IPv4: 0, Latency: make(map[uint32]uint32), LatencyAdd: make(map[uint32]uint32), LatencyMutex: &sync.RWMutex{}}
		c.C[id] = oc
	}
	c.M.Unlock()
	if connInfo == "" {
		oc.ConnInfo = make([]string, 0)
	} else {
		oc.ConnInfo = strings.Split(connInfo, ",")
	}
	oc.ConnInfoStr = connInfo
}

func (c *OtherClients) UpdateClients(pkt PacketClients, viaId, addLatency uint32) {
	tmp := make(map[uint32]struct{})
	for i := 0; i < int(pkt.Count); i++ {
		if pkt.ConnInfo[i].Ok {
			c.UpdateClientConnInfo(pkt.Ids[i], pkt.ConnInfo[i].Val)
		}
		tid := viaId
		tlat := pkt.Latency[i].Val + addLatency
		if !pkt.Latency[i].Ok {
			tid = 0
			tlat = 0
		}
		c.UpdateClient(pkt.Ids[i], pkt.IPv4[i].Val, tid, tlat, addLatency)
		tmp[pkt.Ids[i]] = struct{}{}
	}
	c.M.Lock()
	ocs := make([]*OtherClient, 0)
	for id, oc := range c.C {
		_, ok := tmp[id]
		if !ok {
			ocs = append(ocs, oc)
		}
	}
	c.M.Unlock()
	var dels []uint32
	for i := 0; i < len(ocs); i++ {
		oc := ocs[i]
		oc.LatencyMutex.Lock()
		_, ok := oc.Latency[viaId]
		if !ok {
			oc.LatencyMutex.Unlock()
		} else {
			delete(oc.Latency, viaId)
			oc.LatencyMutex.Unlock()
			oc.SortLatency()
			if len(oc.Latency) == 0 {
				dels = append(dels, oc.Id)
			}
		}
	}
	c.M.Lock()
	for i := 0; i < len(dels); i++ {
		oc, ok := c.C[dels[i]]
		if ok {
			_, ok = c.IPv4Map[oc.IPv4]
			if ok {
				delete(c.IPv4Map, oc.IPv4)
			}
			delete(c.C, dels[i])
		}
	}
	c.M.Unlock()
}

func (c *OtherClients) UpdateLatencyAdd(viaId, addLatency uint32) {
	c.M.Lock()
	ocs := make([]*OtherClient, 0)
	for _, oc := range c.C {
		ocs = append(ocs, oc)
	}
	c.M.Unlock()
	for i := 0; i < len(ocs); i++ {
		oc := ocs[i]
		oc.LatencyMutex.Lock()
		_, ok := oc.Latency[viaId]
		if !ok {
			oc.LatencyMutex.Unlock()
		} else {
			oc.Latency[viaId] = oc.Latency[viaId] - oc.LatencyAdd[viaId] + addLatency
			oc.LatencyAdd[viaId] = addLatency
			oc.LatencyMutex.Unlock()
			oc.SortLatency()
		}
	}
}

func (c *OtherClients) ClearClients(viaId uint32) {
	var pkt PacketClients
	pkt.Count = 0
	c.UpdateClients(pkt, viaId, 0)
}
