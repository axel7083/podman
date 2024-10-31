//go:build !windows

package util

import (
	"os"
	"syscall"
)

func CheckHardLink(fi os.FileInfo) (devino, bool) {
	st := fi.Sys().(*syscall.Stat_t)
	return devino{
		Dev: uint64(st.Dev), //nolint: unconvert
		Ino: st.Ino,
	}, st.Nlink > 1
}
