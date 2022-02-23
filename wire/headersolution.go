package wire

type HeaderSolution struct {
	Type        string
	Address     string
	Nonce       uint32
	ExtraNonce  uint64
	BlockHeight int32
	BlockHash   string
}
