package main

import (
	"encoding/json"
	"fmt"
	"github.com/btcsuite/btcd/blockchain"
	"github.com/btcsuite/btcd/wire"
	"os"
	"syscall/js"
	"time"
)

const (
	// maxNonce is the maximum value a nonce can be in a block header.
	maxNonce = ^uint32(0) // 2^32 - 1
)

func main() {
	argsWithoutProg := os.Args[1:]
	fmt.Println("Golang arguments: ", argsWithoutProg)

	start := time.Now()
	solved, res := calculateHash(([]byte)(argsWithoutProg[0]))
	end := time.Now()

	fmt.Println("Hashing completed in", end.Sub(start))

	js.Global().Call("HashResult", solved, res)
}

func calculateHash(jsonHeader []byte) (bool, uint32) {

	blockHeader := wire.BlockHeader{}
	err := json.Unmarshal(jsonHeader, &blockHeader)
	if err != nil {
		fmt.Sprintf("unable to get unmarshal header. Error %s occurred\n", err)
		return false, 0
	}

	targetDifficulty := *blockchain.CompactToBig(blockHeader.Bits)

	fmt.Printf("header %d\n", blockHeader.Version)
	fmt.Printf("targetDifficulty %d\n", blockHeader.Bits)

	for i := uint32(0); i <= maxNonce; i++ {
		blockHeader.Nonce = i
		headerHash := blockHeader.BlockHash()

		if blockchain.HashToBig(&headerHash).Cmp(&targetDifficulty) <= 0 {
			fmt.Printf("header hash solved (%d)%s\n", i, headerHash.String())

			return true, i
		}
	}
	return false, 0
}
