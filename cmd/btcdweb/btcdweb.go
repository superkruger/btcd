package main

import (
	"fmt"
	"github.com/btcsuite/btcd/socketclient"
	"github.com/btcsuite/btcd/websocketserver"
	"github.com/jessevdk/go-flags"
	"os"
)

func main() {
	cfg := websocketserver.Config{
		SSL:  false,
		Port: "80",
	}

	parser := flags.NewParser(&cfg, flags.Default)
	_, err := parser.Parse()
	if err != nil {
		if e, ok := err.(*flags.Error); !ok || e.Type != flags.ErrHelp {
			parser.WriteHelp(os.Stderr)
		}
		return
	}

	if cfg.SSL && cfg.Port == "80" {
		cfg.Port = "443"
	}

	fmt.Println("Starting btcd web server with config", cfg)

	websocketserver := websocketserver.NewWebsocketServer(&cfg)
	socketClient := socketclient.NewSocketClient(websocketserver)

	socketClient.Connect()
	websocketserver.Start(socketClient)
}
