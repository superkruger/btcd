//go:build js && wasm
// +build js,wasm

package main

import (
	"encoding/json"
	"fmt"
	"github.com/btcsuite/btcd/blockchain"
	"github.com/btcsuite/btcd/wire"
	"os"
	"strconv"
	"syscall/js"
)

const (
	// maxNonce is the maximum value a nonce can be in a block header.
	maxNonce = ^uint32(0) // 2^32 - 1
)

func main() {
	argsWithoutProg := os.Args[1:]
	//fmt.Println("Golang arguments: ", argsWithoutProg)

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

	i, err = strconv.ParseInt(argsWithoutProg[3], 10, 16)
	if err != nil {
		fmt.Errorf("workerPercentage could not be parsed")
	}
	workerPercentage := uint16(i)

	//start := time.Now()
	solved, res := calculateHash(([]byte)(argsWithoutProg[0]), startNonce, endNonce, workerPercentage)
	//end := time.Now()

	//fmt.Println("Hashing completed in", end.Sub(start))

	js.Global().Call("HashResult", solved, res)
}

func calculateHash(jsonHeader []byte, startNonce uint32, endNonce uint32, workerPercentage uint16) (bool, uint32) {
	//fmt.Printf("workerPercentage %v\n", workerPercentage)

	blockHeader := wire.BlockHeader{}
	err := json.Unmarshal(jsonHeader, &blockHeader)
	if err != nil {
		fmt.Sprintf("unable to get unmarshal header. Error %s occurred\n", err)
		return false, 0
	}

	targetDifficulty := *blockchain.CompactToBig(blockHeader.Bits)

	//fmt.Printf("header %d, start %d, end %d\n", blockHeader.Version, startNonce, endNonce)
	//fmt.Printf("targetDifficulty %d\n", blockHeader.Bits)

	//sleepTime := int32(0)
	//if workerPercentage != 100 {
	//	sleepPart := 1.0 / float32(workerPercentage)
	//	fmt.Printf("sleep part %v\n", sleepPart)
	//	sleepTime = int32(1000 * sleepPart)
	//}
	//fmt.Printf("Sleep time %v\n", sleepTime)

	//startTime := time.Now()

	i := startNonce
	for ; i <= endNonce; i++ {
		blockHeader.Nonce = i
		headerHash := blockHeader.BlockHash()

		if blockchain.HashToBig(&headerHash).Cmp(&targetDifficulty) <= 0 {
			fmt.Printf("header hash solved (%d)%s\n", i, headerHash.String())

			return true, i
		}

		//if sleepTime != 0 {
		//fmt.Printf("sleeping...\n")
		//time.Sleep(1 * time.Second)

		//if time.Now().Sub(startTime) > time.Second*10 {
		//	fmt.Printf("calculating too long, ending\n")
		//	break
		//}

		//gosleep := js.Global().Get("GoSleep")
		//slept, err := await(js.Global().Call("GoSleep", sleepTime))

		//if err != nil {
		//	fmt.Printf("Sleep error %v\n", err)
		//}

		//slept := js.Global().Call("GoSleep", sleepTime)

		//fmt.Printf("Slept. %v\n", slept)

		//time.Sleep(sleepTime)
		//if i%10 == 0 {
		//	fmt.Printf("sleeping\n")
		//}
		//}

		//if i%1000000 == 0 {
		//	fmt.Println("Hashed", i, time.Now())
		//}
	}
	return false, i
}

func await(awaitable js.Value) ([]js.Value, []js.Value) {
	then := make(chan []js.Value)
	defer close(then)
	thenFunc := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		then <- args
		return nil
	})
	defer thenFunc.Release()

	catch := make(chan []js.Value)
	defer close(catch)
	catchFunc := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		catch <- args
		return nil
	})
	defer catchFunc.Release()

	awaitable.Call("then", thenFunc).Call("catch", catchFunc)

	select {
	case result := <-then:
		return result, nil
	case err := <-catch:
		return nil, err
	}
}
