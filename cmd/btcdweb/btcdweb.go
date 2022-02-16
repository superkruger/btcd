package main

import (
	"github.com/btcsuite/btcd/socketclient"
	"github.com/btcsuite/btcd/websocketserver"
)

func main() {
	websocketserver := websocketserver.NewWebsocketServer()
	socketClient := socketclient.NewSocketClient(websocketserver)

	socketClient.Connect()
	websocketserver.Start(socketClient)
}
