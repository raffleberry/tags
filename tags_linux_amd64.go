//go:build linux && amd64

package tags

/*
#cgo CFLAGS: -I${SRCDIR}/include/taglib
#cgo LDFLAGS: -L${SRCDIR}/lib/linux/amd64/lib -static -ltag_c -ltag -lstdc++ -lm -lz
*/
import "C"
