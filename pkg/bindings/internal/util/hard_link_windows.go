package util

import (
	"os"
)

func CheckHardLink(fi os.FileInfo) (devino, bool) {
	return devino{}, false
}
