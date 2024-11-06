//go:build windows && amd64

package tags

/*
#cgo CFLAGS: -I${SRCDIR}/include/taglib
#cgo LDFLAGS: -L${SRCDIR}/lib/windows/amd64/lib -ltag_c -ltag -lstdc++ -lm -lz
*/
import "C"
