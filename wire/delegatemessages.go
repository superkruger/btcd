package wire

type HeaderProblemResponse struct {
	Type        string
	Header      BlockHeader
	Address     string
	BlockHeight int32
	ExtraNonce  uint64
}

type HeaderProblemRequest struct {
	Address         string
	BlockHeight     int32
	HashesCompleted uint64
}

type HeaderSolution struct {
	Type        string
	Address     string
	Nonce       uint32
	ExtraNonce  uint64
	BlockHeight int32
	BlockHash   string
}
