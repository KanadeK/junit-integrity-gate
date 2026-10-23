//go:build !windows

package gate

import (
	"io/fs"
	"os"
	"syscall"
)

func isReparse(info fs.FileInfo) bool { return false }

// Avoid blocking if a concurrently replaced regular file becomes a FIFO.
func readFlags() int { return os.O_RDONLY | syscall.O_NONBLOCK }
