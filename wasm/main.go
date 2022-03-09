package main

import (
	"encoding/json"
	"fmt"
	"github.com/btcsuite/btcd/blockchain"
	"github.com/btcsuite/btcd/wire"
	"os"
	"strconv"
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

	i, err := strconv.ParseInt(argsWithoutProg[1], 10, 32)
	if err != nil {
		fmt.Errorf("start nonce could not be parsed")
	}
	startNonce := uint32(i)

	i, err = strconv.ParseInt(argsWithoutProg[2], 10, 32)
	if err != nil {
		fmt.Errorf("end nonce could not be parsed")
	}
	endNonce := uint32(i)

	start := time.Now()
	solved, res := calculateHash(([]byte)(argsWithoutProg[0]), startNonce, endNonce)
	end := time.Now()

	fmt.Println("Hashing completed in", end.Sub(start))

	js.Global().Call("HashResult", solved, res)
}

func calculateHash(jsonHeader []byte, startNonce uint32, endNonce uint32) (bool, uint32) {

	blockHeader := wire.BlockHeader{}
	err := json.Unmarshal(jsonHeader, &blockHeader)
	if err != nil {
		fmt.Sprintf("unable to get unmarshal header. Error %s occurred\n", err)
		return false, 0
	}

	targetDifficulty := *blockchain.CompactToBig(blockHeader.Bits)

	fmt.Printf("header %d, start %d, end %d\n", blockHeader.Version, startNonce, endNonce)
	fmt.Printf("targetDifficulty %d\n", blockHeader.Bits)

	i := startNonce
	for ; i <= endNonce; i++ {
		blockHeader.Nonce = i
		headerHash := blockHeader.BlockHash()

		if blockchain.HashToBig(&headerHash).Cmp(&targetDifficulty) <= 0 {
			fmt.Printf("header hash solved (%d)%s\n", i, headerHash.String())

			return true, i
		}

		//if i%1000000 == 0 {
		//	fmt.Println("Hashed", i, time.Now())
		//}
	}
	return false, i - startNonce
}
