package rest

import (
	"encoding/json"
	"strconv"
)

type tUser struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

type Payload struct {
	Link   string
	Params string
}

func (p Payload) MarshalBinary() ([]byte, error) {
	return json.Marshal(p)
}

func (p *Payload) UnmarshalBinary(data []byte) error {
	return json.Unmarshal(data, p)
}

type arrString []string

func (s arrString) StringSliceToUintSlice() ([]uint, error) {
	strSlice := []string(s)
	uintSlice := make([]uint, len(strSlice))
	for i, str := range s {
		u64, err := strconv.ParseUint(str, 10, 64)
		if err != nil {
			return nil, err
		}
		uintSlice[i] = uint(u64)
	}
	return uintSlice, nil
}
