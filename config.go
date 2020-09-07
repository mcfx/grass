package grass

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net"
	"strings"
)

type Config struct {
	Id                   uint32
	Key                  string
	InterfaceName        string
	MTU                  int
	IPv4                 string
	IPv4Gateway          string
	IPv4Mask             string
	IPv4DNS              string
	Listen               []ListenInfo
	BootstrapNodes       []string
	CheckClientsInterval int
	PingInterval         int
	CheckConnInterval    int
	Debug                bool
	ConnectNewPeer       bool
	StartTun             bool
}

func UnmarshalConfig(s []byte) (ClientConfig, error) {
	var c Config
	var r ClientConfig
	err := json.Unmarshal(s, &c)
	if err != nil {
		return r, err
	}

	r.Id = c.Id
	if c.Id == 0 {
		return r, fmt.Errorf("Id cannot be zero")
	}
	thash := sha256.Sum256([]byte(c.Key))
	r.Key = thash[:16]

	r.InterfaceName = c.InterfaceName
	r.MTU = c.MTU
	if r.MTU == 0 {
		r.MTU = 1500
	}
	if r.InterfaceName == "" {
		r.InterfaceName = "tun0"
	}

	if c.IPv4 != "" {
		r.IPv4 = IPToUint32(net.ParseIP(c.IPv4))
		r.IPv4Gateway = IPToUint32(net.ParseIP(c.IPv4Gateway))
		r.IPv4Mask = IPToUint32(net.ParseIP(c.IPv4Mask))
		tdns := strings.Split(c.IPv4DNS, ",")
		r.IPv4DNS = make([]uint32, 0)
		for i := 0; i < len(tdns); i++ {
			r.IPv4DNS = append(r.IPv4DNS, IPToUint32(net.ParseIP(tdns[i])))
		}
	}
	r.Listen = c.Listen
	r.BootstrapNodes = c.BootstrapNodes
	r.CheckClientsInterval = c.CheckClientsInterval
	r.PingInterval = c.PingInterval
	r.CheckConnInterval = c.CheckConnInterval
	r.Debug = c.Debug
	r.ConnectNewPeer = c.ConnectNewPeer
	r.StartTun = c.StartTun
	return r, nil
}
