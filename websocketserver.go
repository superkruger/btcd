package main

import (
	"encoding/json"
	"fmt"
	"github.com/btcsuite/btcd/mining/delegateminer"
	"github.com/btcsuite/btcd/wire"
	"github.com/gorilla/websocket"
	"log"
	"net/http"
)

type HeaderProvider interface {
	SetHeaderSender(headerSender delegateminer.HeaderSender)
	SocketClosed(conn *websocket.Conn) error
	GetHeaderProblem(request *wire.HeaderProblemRequest, conn *websocket.Conn) error
	SetHeaderSolution(solution *wire.HeaderSolution, conn *websocket.Conn) error
	StopMining(conn *websocket.Conn) error
}

type WebsocketServer struct {
	config         *config
	upgrader       websocket.Upgrader
	headerProvider HeaderProvider
	//conn         []*websocket.Conn
}

type MinerMessage struct {
	Type            string
	Address         string
	Nonce           uint32
	ExtraNonce      uint64
	BlockHeight     int32
	HashesCompleted uint64
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
			log.Println("REQUEST received", minerMessage)
			headerProblemRequest := wire.HeaderProblemRequest{
				Address:         minerMessage.Address,
				BlockHeight:     minerMessage.BlockHeight,
				HashesCompleted: minerMessage.HashesCompleted,
			}
			err := s.headerProvider.GetHeaderProblem(&headerProblemRequest, conn)
			if err != nil {
				log.Println("Error (GetHeaderProblem)", err)
			}
			break
		case "STOP":
			log.Println("STOP received", minerMessage)
			err := s.headerProvider.StopMining(conn)
			if err != nil {
				log.Println("Error (StopMining)", err)
			}
			break
		case "SOLVED":
			log.Println("SOLVED received", minerMessage)
			headerSolution := wire.HeaderSolution{
				Address:     minerMessage.Address,
				Nonce:       minerMessage.Nonce,
				ExtraNonce:  minerMessage.ExtraNonce,
				BlockHeight: minerMessage.BlockHeight,
			}
			err := s.headerProvider.SetHeaderSolution(&headerSolution, conn)
			if err != nil {
				log.Println("Error (SetHeaderSolution)", err)
			} else {
				s.SendHeaderSolution(&headerSolution, conn)
			}
			break
		default:
			log.Println("Unknown message type", minerMessage.Type)
		}
	}

	err = s.headerProvider.SocketClosed(conn)
	if err != nil {
		log.Printf("Could not close delegate socket %v", err)
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

func (s *WebsocketServer) SendHeaderProblem(headerProblem *wire.HeaderProblemResponse, conn *websocket.Conn) error {

	headerProblem.Type = "PROBLEM"
	jsonMessage, err := json.Marshal(headerProblem)
	if err != nil {
		log.Println("Error during message marshalling:", err)
		return err
	}

	err = conn.WriteMessage(websocket.TextMessage, jsonMessage)
	if err != nil {
		log.Println("Error during message writing:", err)
		return err
	}
	return nil
}

func (s *WebsocketServer) SendHeaderSolution(headerSolution *wire.HeaderSolution, conn *websocket.Conn) error {

	headerSolution.Type = "SOLUTION"
	jsonMessage, err := json.Marshal(headerSolution)
	if err != nil {
		log.Println("Error during message marshalling:", err)
		return err
	}

	err = conn.WriteMessage(websocket.TextMessage, jsonMessage)
	if err != nil {
		log.Println("Error during message writing:", err)
		return err
	}

	return nil
}

func (s *WebsocketServer) Start(headerProvider HeaderProvider) {
	s.headerProvider = headerProvider
	s.headerProvider.SetHeaderSender(s)

	fmt.Println("Starting WebsocketServer...")
	http.HandleFunc("/socket", s.socketHandler)
	http.HandleFunc("/", s.home)

	address := ":" + s.config.WSS_Port
	if s.config.WSS_SSL {
		log.Fatal(http.ListenAndServeTLS(
			address,
			"/etc/letsencrypt/live/api.decentravest.com/fullchain.pem",
			"/etc/letsencrypt/live/api.decentravest.com/privkey.pem",
			nil))
	} else {
		log.Fatal(http.ListenAndServe(address, nil))
	}
}

func (s *WebsocketServer) Stop() {

}

func NewWebsocketServer(config *config) *WebsocketServer {
	return &WebsocketServer{
		config:   config,
		upgrader: websocket.Upgrader{},
		//conn:     make([]*websocket.Conn, 0),
	}
}
