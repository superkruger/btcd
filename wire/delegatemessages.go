package wire

type HeaderProblemRequest struct {
	Address         string
	BlockHeight     int32
	HashesCompleted int32
}

type HeaderProblemResponse struct {
	Type        string
	Header      BlockHeader
	Address     string
	BlockHeight int32
	ExtraNonce  string
}

type HashesRequest struct {
	HashesCompleted int32
}

type HeaderSolution struct {
	Type        string
	Address     string
	Nonce       uint32
	ExtraNonce  string
	BlockHeight int32
	BlockHash   string
}

type Winner struct {
	Address  string
	Hash     string
	SolvedAt int64
}

type Winners struct {
	Type    string
	Entries []Winner
}
