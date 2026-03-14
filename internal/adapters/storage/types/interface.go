package types

type ObjInterface interface {
	MarshalBinary() ([]byte, error)
	UnmarshalBinary(data []byte) error
}
