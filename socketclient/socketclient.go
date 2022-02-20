package socketclient

import (
	"fmt"
	"github.com/btcsuite/btcd/websocketserver"
	"github.com/btcsuite/btcd/wire"
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

	err := s.cl.RegisterName("ReturnHeaderProblem", s.ReturnHeaderProblem)
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

func (s *SocketClient) RequestHeaderProblem(request *wire.HeaderProblemRequest, conn *websocket.Conn) {

	// store request
	s.requests[request.Address] = conn

	err := s.cl.Call("RequestHeaderProblem", *request, nil)
	if err != nil {
		fmt.Printf("RequestHeaderProblem Call failed: %s\n", err)
		return
	}
}

func (s *SocketClient) ProvideHeaderSolution(headerSolution *wire.HeaderSolution, conn *websocket.Conn) {

	// store request
	s.requests[headerSolution.Address] = conn

	err := s.cl.Call("ProvideHeaderSolution", *headerSolution, nil)
	if err != nil {
		fmt.Printf("ProvideHeaderSolution Call failed: %s\n", err)
		return
	}
}

func (s *SocketClient) ReturnHeaderProblem(headerProblem wire.HeaderProblemResponse, unused *int) error {
	fmt.Printf("ReturnHeaderProblem %v\n", headerProblem)
	s.websocketserver.SendHeaderProblem(&headerProblem, s.requests[headerProblem.Address])

	// remove request
	delete(s.requests, headerProblem.Address)
	return nil
}
