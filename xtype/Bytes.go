package xtype

import (
	"unsafe"

	"github.com/kklab-com/goth-kkutil/hex"
)

type Bytes []byte

func (s Bytes) String() string {
	return *(*string)(unsafe.Pointer(&s))
}

func (s Bytes) Bytes() []byte {
	return s
}

func (s Bytes) Hex() string {
	return hex.EncodeToString(s)
}
