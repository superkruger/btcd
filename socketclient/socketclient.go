package socketclient

import (
	"fmt"
	"github.com/btcsuite/btcd/socketserver"
	"github.com/btcsuite/btcd/websocketserver"
	"github.com/cmcoffee/go-ezipc"
	"github.com/gorilla/websocket"
)

type SocketClient struct {
	cl              *ezipc.EzIPC
	ready           chan bool
	websocketserver *websocketserver.WebsocketServer
	requests        map[string]*websocket.Conn
}

func NewSocketClient(websocketserver *websocketserver.WebsocketServer) *SocketClient {
	return &SocketClient{
		cl:              ezipc.New(),
		ready:           make(chan bool),
		websocketserver: websocketserver,
		requests:        make(map[string]*websocket.Conn),
	}
}

func (s *SocketClient) Connect() {

	err := s.cl.RegisterName("ForwardHeader", s.ForwardHeader)
	if err != nil {
		fmt.Println(err)
		return
	}

	fmt.Println("Opening socket file...")

	err = s.cl.Dial("/tmp/blab.sock")
	if err != nil {
		fmt.Println(err)
		return
	}

	err = s.cl.Call("Connected", nil, nil)
	if err != nil {
		fmt.Println(err)
	}

}

func (s *SocketClient) RequestHeader(message *websocketserver.MinerMessage, conn *websocket.Conn) {

	// store request
	s.requests[message.Address] = conn

	err := s.cl.Call("RequestHeader", message.Address, nil)
	if err != nil {
		fmt.Printf("RequestHeader Call failed: %s\n", err)
		return
	}

}

func (s *SocketClient) ForwardHeader(message socketserver.HeaderMessage, unused *int) error {
	fmt.Printf("SendHeader %v\n", message)
	s.websocketserver.ForwardHeader(message, s.requests[message.Address])

	// remove request
	delete(s.requests, message.Address)
	return nil
}

func (s *SocketClient) SolvedHash(message *websocketserver.MinerMessage, conn *websocket.Conn) {

}
