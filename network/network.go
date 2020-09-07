package network

import (
	"io"
	"log"
	"sync"

	"github.com/mcfx0/grass/network/packet"
)

const (
	MTU = 1500
)

type TunHandler struct {
	dev io.ReadWriteCloser

	writerStopCh chan bool
	WriteCh      chan interface{}

	wg sync.WaitGroup

	handler func([]byte, *packet.IPv4) error
}

func New(dev io.ReadWriteCloser, handler func([]byte, *packet.IPv4) error) *TunHandler {
	th := &TunHandler{
		dev:          dev,
		writerStopCh: make(chan bool, 10),
		WriteCh:      make(chan interface{}, 10000),
		handler:      handler,
	}
	return th
}

func (th *TunHandler) Run() {
	// writer
	go func() {
		th.wg.Add(1)
		defer th.wg.Done()
		for {
			select {
			case pkt := <-th.WriteCh:
				switch pkt.(type) {
				case *ipPacket:
					ip := pkt.(*ipPacket)
					th.dev.Write(ip.wire)
					releaseIPPacket(ip)
				case []byte:
					th.dev.Write(pkt.([]byte))
				}
			case <-th.writerStopCh:
				log.Printf("quit tun2handler writer")
				return
			}
		}
	}()

	// reader
	var buf [MTU]byte
	var ip packet.IPv4

	th.wg.Add(1)
	defer th.wg.Done()
	for {
		n, e := th.dev.Read(buf[:])
		if e != nil {
			// TODO: stop at critical error
			log.Printf("read packet error: %s", e)
			return
		}
		data := buf[:n]
		//log.Println(data)
		e = packet.ParseIPv4(data, &ip)
		if e != nil {
			log.Printf("error to parse IPv4: %s", e)
			continue
		}

		//go th.handler(data, &ip)
		th.handler(data, &ip)
		/*go func() {
			err := th.handler(data, &ip)
			if err != nil {
				log.Printf("ipv4 error: %v\n", err)
			}
		}()*/
	}
}
