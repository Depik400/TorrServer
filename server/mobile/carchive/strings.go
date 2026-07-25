package main

/*
#include <stdlib.h>
*/
import "C"
import (
	"unsafe"
)

func toCString(s string) *C.char {
	return C.CString(s)
}

func fromCString(c *C.char) string {
	if c == nil {
		return ""
	}
	return C.GoString(c)
}

//export TS_Free
func TS_Free(value *C.char) {
	if value != nil {
		C.free(unsafe.Pointer(value))
	}
}
