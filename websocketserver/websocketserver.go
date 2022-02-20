package websocketserver

import (
	"encoding/json"
	"fmt"
	"github.com/btcsuite/btcd/wire"
	"github.com/gorilla/websocket"
	"log"
	"net/http"
)

type HeaderClient interface {
	RequestHeaderProblem(request *wire.HeaderProblemRequest, conn *websocket.Conn)
	ProvideHeaderSolution(headerSolution *wire.HeaderSolution, conn *websocket.Conn)
}

type WebsocketServer struct {
	upgrader     websocket.Upgrader
	headerclient HeaderClient
	//conn         []*websocket.Conn
}

type MinerMessage struct {
	Type            string
	Address         string
	Nonce           uint32
	ExtraNonce      uint64
	BlockHeight     int32
	HashesPerSecond int32
}

func (s *WebsocketServer) socketHandler(w http.ResponseWriter, r *http.Request) {
	log.Println("Socket handler")

	s.upgrader.CheckOrigin = func(r *http.Request) bool { return true }
	var err error

	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Print("Error during connection upgradation:", err)
		return
	}
	defer s.closeConn(conn)

	//s.conn = append(s.conn, conn)

	// The event loop
	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			log.Println("Error during message reading:", err)
			break
		}
		log.Printf("Received: %s", message)

		minerMessage := MinerMessage{}
		err = json.Unmarshal(message, &minerMessage)
		if err != nil {
			log.Println("Error during message unmarshalling:", err)
			break
		}

		switch minerMessage.Type {
		case "REQUEST":
			log.Println("REQUEST received")
			headerProblemRequest := wire.HeaderProblemRequest{
				Address:         minerMessage.Address,
				BlockHeight:     minerMessage.BlockHeight,
				HashesPerSecond: minerMessage.HashesPerSecond,
			}
			s.headerclient.RequestHeaderProblem(&headerProblemRequest, conn)
			break
		case "SOLVED":
			log.Println("SOLVED received")
			headerSolution := wire.HeaderSolution{
				Address:     minerMessage.Address,
				Nonce:       minerMessage.Nonce,
				ExtraNonce:  minerMessage.ExtraNonce,
				BlockHeight: minerMessage.BlockHeight,
			}
			s.headerclient.ProvideHeaderSolution(&headerSolution, conn)
			break
		default:
			log.Println("Unknown message type", minerMessage.Type)
		}

		//err = conn.WriteMessage(messageType, message)
		//if err != nil {
		//	log.Println("Error during message writing:", err)
		//	break
		//}
	}

	log.Println("closing websocket handler...")
}

func (s *WebsocketServer) home(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "Index Page")
}

func (s *WebsocketServer) closeConn(conn *websocket.Conn) {
	conn.Close()
	// TODO remove from array
}

func (s *WebsocketServer) SendHeaderProblem(headerProblem *wire.HeaderProblemResponse, conn *websocket.Conn) {

	jsonMessage, err := json.Marshal(headerProblem)
	if err != nil {
		log.Println("Error during message marshalling:", err)
		return
	}

	err = conn.WriteMessage(websocket.TextMessage, jsonMessage)
	if err != nil {
		log.Println("Error during message writing:", err)
	}
}

func (s *WebsocketServer) Start(headerclient HeaderClient) {
	s.headerclient = headerclient

	fmt.Println("Starting WebsocketServer...")
	http.HandleFunc("/socket", s.socketHandler)
	http.HandleFunc("/", s.home)
	log.Fatal(http.ListenAndServeTLS(
		":443",
		"/etc/letsencrypt/live/api.decentravest.com/fullchain.pem",
		"/etc/letsencrypt/live/api.decentravest.com/privkey.pem",
		nil))
}

func NewWebsocketServer() *WebsocketServer {
	return &WebsocketServer{
		upgrader: websocket.Upgrader{},
		//conn:     make([]*websocket.Conn, 0),
	}
}
