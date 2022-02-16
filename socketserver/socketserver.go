package socketserver

import (
	"fmt"
	"github.com/btcsuite/btcd/wire"
	"github.com/cmcoffee/go-ezipc"
)

type HeaderProvider interface {
	GetCurrentHeader() *wire.BlockHeader
}

type HeaderMessage struct {
	Header     wire.BlockHeader
	Address    string
	ExtraNonce uint64
}

type SocketServer struct {
	//myKV  common.KV
	cl             *ezipc.EzIPC
	ready          chan bool
	headerProvider HeaderProvider
}

func NewSocketServer() *SocketServer {
	return &SocketServer{
		//myKV: common.KV{
		//	Data: make(map[uint64]common.BlockHeader),
		//},
		cl:    ezipc.New(),
		ready: make(chan bool),
	}
}

func (s *SocketServer) Start(headerProvider HeaderProvider) {
	s.headerProvider = headerProvider

	//err := s.cl.Register(&s.myKV)
	//if err != nil {
	//	fmt.Printf("Error: %s\n", err)
	//	return
	//}

	//s.cl.RegisterName("KVCount", s.myKV.CountKeys)

	err := s.cl.RegisterName("Connected", s.Connected)
	if err != nil {
		fmt.Println(err)
		return
	}

	err = s.cl.RegisterName("RequestHeader", s.RequestHeader)
	if err != nil {
		fmt.Println(err)
		return
	}

	fmt.Println("Listening for requests!")
	//
	//n := BlockHeader{Nonce: uint32(99999), Bits: uint32(7777777)}
	//
	//err = s.myKV.Set(0, &n)
	////err = cl.Call("KV.Set", 0, n)
	//if err != nil {
	//	fmt.Printf("Call failed: %s\n", err)
	//	return
	//}

	//ready <- true

	err = s.cl.Listen("/tmp/blab.sock")
	if err != ezipc.ErrClosed {
		fmt.Printf("Connection Error: %s\n", err)
		return
	}
}

func (s *SocketServer) Send(b *wire.BlockHeader, extraNonce uint64) {

	headerMessage := HeaderMessage{
		Header:     *b,
		ExtraNonce: extraNonce,
	}

	//err := s.myKV.Set(extraNonce, b)
	err := s.cl.Call("SendHeader", 0, &headerMessage)
	if err != nil {
		fmt.Printf("Call failed: %s\n", err)
		return
	}

	//err = s.cl.Call("Ping", 0, extraNonce)
	//if err != nil {
	//	fmt.Printf("Ping failed: %s\n", err)
	//	return
	//}

}

func (s *SocketServer) Ready() chan bool {
	return s.ready
}

func (s *SocketServer) Connected(unused int, unused2 *int) error {
	fmt.Printf("Client Connected\n")
	return nil
}

func (s *SocketServer) RequestHeader(address string, unused *int) error {
	fmt.Printf("Header Requested for %s\n", address)

	currentHeader := s.headerProvider.GetCurrentHeader()

	headerMessage := HeaderMessage{
		Header:  *currentHeader,
		Address: address,
	}

	err := s.cl.Call("ForwardHeader", headerMessage, nil)
	if err != nil {
		fmt.Printf("ForwardHeader Call failed: %s\n", err)
		return nil
	}

	return nil
}
