package wire

type HeaderProblemResponse struct {
	Header      BlockHeader
	Address     string
	BlockHeight int32
	ExtraNonce  uint64
}

type HeaderProblemRequest struct {
	Address         string
	BlockHeight     int32
	HashesPerSecond int32
}
