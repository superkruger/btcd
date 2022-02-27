package websocketserver

import (
	"encoding/json"
	"fmt"
	"github.com/btcsuite/btcd/wire"
	"github.com/gorilla/websocket"
	"log"
	"net/http"
)

type Config struct {
	SSL  bool   `short:"s" long:"ssl" description:"Run socket server with ssl"`
	Port string `short:"p" long:"port" description:"Socket port. Default 80, or 443 if SSL"`
}

type HeaderClient interface {
	RequestHeaderProblem(request *wire.HeaderProblemRequest, conn *websocket.Conn) error
	ProvideHeaderSolution(headerSolution *wire.HeaderSolution, conn *websocket.Conn) error
}

type WebsocketServer struct {
	config       *Config
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
			err := s.headerclient.RequestHeaderProblem(&headerProblemRequest, conn)
			if err != nil {
				log.Println("Error (RequestHeaderProblem)", err)
			}
			break
		case "SOLVED":
			log.Println("SOLVED received")
			headerSolution := wire.HeaderSolution{
				Address:     minerMessage.Address,
				Nonce:       minerMessage.Nonce,
				ExtraNonce:  minerMessage.ExtraNonce,
				BlockHeight: minerMessage.BlockHeight,
			}
			err := s.headerclient.ProvideHeaderSolution(&headerSolution, conn)
			if err != nil {
				log.Println("Error (ProvideHeaderSolution)", err)
			}
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

	headerProblem.Type = "PROBLEM"
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

func (s *WebsocketServer) SendHeaderSolution(headerSolution *wire.HeaderSolution, conn *websocket.Conn) {

	headerSolution.Type = "SOLUTION"
	jsonMessage, err := json.Marshal(headerSolution)
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

	address := ":" + s.config.Port
	if s.config.SSL {
		log.Fatal(http.ListenAndServeTLS(
			address,
			"/etc/letsencrypt/live/api.decentravest.com/fullchain.pem",
			"/etc/letsencrypt/live/api.decentravest.com/privkey.pem",
			nil))
	} else {
		log.Fatal(http.ListenAndServe(address, nil))
	}
}

func NewWebsocketServer(config *Config) *WebsocketServer {
	return &WebsocketServer{
		config:   config,
		upgrader: websocket.Upgrader{},
		//conn:     make([]*websocket.Conn, 0),
	}
}
