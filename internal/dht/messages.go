package dht

import (
	"fmt"

	"github.com/vmihailenco/msgpack/v5"
)

type PingRequest struct {
	FromID   ID     `msgpack:"from_id"`
	FromAddr string `msgpack:"from_addr"`
}

type PingResponse struct {
	FromID   ID     `msgpack:"from_id"`
	FromAddr string `msgpack:"from_addr"`
}

type FindNodeRequest struct {
	FromID   ID     `msgpack:"from_id"`
	FromAddr string `msgpack:"from_addr"`
	Target   ID     `msgpack:"target"`
}

type FindNodeResponse struct {
	FromID ID     `msgpack:"from_id"`
	Nodes  []Node `msgpack:"nodes"`
}

func Encode(v any) ([]byte, error) {
	b, err := msgpack.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("dht: msgpack encode: %w", err)
	}
	return b, nil
}

func Decode(data []byte, v any) error {
	if err := msgpack.Unmarshal(data, v); err != nil {
		return fmt.Errorf("dht: msgpack decode: %w", err)
	}
	return nil
}
