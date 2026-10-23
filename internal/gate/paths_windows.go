package gate

import (
	"io/fs"
	"os"
	"syscall"
)

func isReparse(info fs.FileInfo) bool {
	data, ok := info.Sys().(*syscall.Win32FileAttributeData)
	return ok && data.FileAttributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT != 0
}

func readFlags() int { return os.O_RDONLY }
