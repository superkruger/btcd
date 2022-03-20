package main

import (
	"encoding/json"
	"fmt"
	"github.com/btcsuite/btcd/database/delegate"
	"github.com/btcsuite/btcd/mining/delegateminer"
	"github.com/btcsuite/btcd/wire"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"log"
	"net/http"
)

type HeaderProvider interface {
	SetHeaderSender(headerSender delegateminer.HeaderSender)
	SocketClosed(conn *websocket.Conn, wsId string) error
	GetHeaderProblem(request *wire.HeaderProblemRequest, conn *websocket.Conn, wsId string) error
	ReportHashes(request *wire.HashesRequest, conn *websocket.Conn, wsId string) error
	SetHeaderSolution(solution *wire.HeaderSolution, conn *websocket.Conn, wsId string) error
	StopMining(conn *websocket.Conn, wsId string) error
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
	ExtraNonce      string
	BlockHeight     int32
	HashesCompleted int32
}

func (s *WebsocketServer) socketHandler(w http.ResponseWriter, r *http.Request) {
	wsId := uuid.NewString()
	dlgmLog.Infof("Socket handler")

	s.upgrader.CheckOrigin = func(r *http.Request) bool { return true }
	var err error

	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		dlgmLog.Errorf("Error during connection upgradation: %v", err)
		return
	}
	defer s.closeConn(conn)

	//s.conn = append(s.conn, conn)

	// The event loop
	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			dlgmLog.Errorf("Error during message reading: %v", err)
			break
		}
		dlgmLog.Debugf("Received: %s", message)

		minerMessage := MinerMessage{}
		err = json.Unmarshal(message, &minerMessage)
		if err != nil {
			dlgmLog.Errorf("Error during message unmarshalling: %v", err)
			break
		}

		switch minerMessage.Type {
		case "REQUEST":
			dlgmLog.Infof("REQUEST received %v", minerMessage)
			headerProblemRequest := wire.HeaderProblemRequest{
				Address:         minerMessage.Address,
				BlockHeight:     minerMessage.BlockHeight,
				HashesCompleted: minerMessage.HashesCompleted,
			}
			err := s.headerProvider.GetHeaderProblem(&headerProblemRequest, conn, wsId)
			if err != nil {
				dlgmLog.Errorf("Error (GetHeaderProblem) %v", err)
			}
			break
		case "STOP":
			dlgmLog.Infof("STOP received %v", minerMessage)
			err := s.headerProvider.StopMining(conn, wsId)
			if err != nil {
				dlgmLog.Errorf("Error (StopMining) %v", err)
			}
			break
		case "HASHES":
			dlgmLog.Debugf("HASHES received %v", minerMessage)
			hashesRequest := wire.HashesRequest{
				HashesCompleted: minerMessage.HashesCompleted,
			}
			err := s.headerProvider.ReportHashes(&hashesRequest, conn, wsId)
			if err != nil {
				dlgmLog.Errorf("Error (StopMining) %v", err)
			}
			break
		case "SOLVED":
			dlgmLog.Infof("SOLVED received %v", minerMessage)
			headerSolution := wire.HeaderSolution{
				Address:     minerMessage.Address,
				Nonce:       minerMessage.Nonce,
				ExtraNonce:  minerMessage.ExtraNonce,
				BlockHeight: minerMessage.BlockHeight,
			}
			err := s.headerProvider.SetHeaderSolution(&headerSolution, conn, wsId)
			if err != nil {
				dlgmLog.Errorf("Error (SetHeaderSolution) %v", err)
			} else {
				s.SendHeaderSolution(&headerSolution, conn)
			}
			break
		case "WINNERS":
			dlgmLog.Debugf("WINNERS request received %v", minerMessage)
			winners, err := s.getWinners()
			if err != nil {
				dlgmLog.Errorf("Error (getWinners) %v", err)
			} else {
				s.SendWinners(winners, conn)
			}
			break
		default:
			dlgmLog.Warnf("Unknown message type %v", minerMessage.Type)
		}
	}

	err = s.headerProvider.SocketClosed(conn, wsId)
	if err != nil {
		dlgmLog.Errorf("Could not close delegate socket %v", err)
	}
	dlgmLog.Infof("closing websocket handler...")
}

func (s *WebsocketServer) getWinners() (*wire.Winners, error) {
	solutions, err := delegate.NewSolutions()
	if err != nil {
		return nil, err
	}

	entries, err := solutions.FindAll()
	solutions.Close()
	if err != nil {
		return nil, err
	}

	winners := wire.Winners{
		Entries: []wire.Winner{},
	}

	for _, w := range entries {
		winner := wire.Winner{
			Address:  w.Address,
			SolvedAt: w.SolvedAt.Unix(),
			Hash:     w.Hash,
		}
		winners.Entries = append(winners.Entries, winner)
	}

	return &winners, nil
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
		dlgmLog.Errorf("Error during message marshalling: %v", err)
		return err
	}

	err = conn.WriteMessage(websocket.TextMessage, jsonMessage)
	if err != nil {
		dlgmLog.Errorf("Error during message writing: %v", err)
		return err
	}
	return nil
}

func (s *WebsocketServer) SendHeaderSolution(headerSolution *wire.HeaderSolution, conn *websocket.Conn) error {

	headerSolution.Type = "SOLUTION"
	jsonMessage, err := json.Marshal(headerSolution)
	if err != nil {
		dlgmLog.Errorf("Error during message marshalling: %v", err)
		return err
	}

	err = conn.WriteMessage(websocket.TextMessage, jsonMessage)
	if err != nil {
		dlgmLog.Errorf("Error during message writing: %v", err)
		return err
	}

	return nil
}

func (s *WebsocketServer) SendWinners(winners *wire.Winners, conn *websocket.Conn) error {

	winners.Type = "WINNERS"
	jsonMessage, err := json.Marshal(winners)
	if err != nil {
		dlgmLog.Errorf("Error during message marshalling: %v", err)
		return err
	}

	err = conn.WriteMessage(websocket.TextMessage, jsonMessage)
	if err != nil {
		dlgmLog.Errorf("Error during message writing: %v", err)
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
			"/etc/letsencrypt/live/api.minerlotto.com/fullchain.pem",
			"/etc/letsencrypt/live/api.minerlotto.com/privkey.pem",
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
