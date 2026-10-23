package gate

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

type reparseInfo struct{}

func (reparseInfo) Name() string       { return "junction" }
func (reparseInfo) Size() int64        { return 0 }
func (reparseInfo) Mode() os.FileMode  { return os.ModeDir }
func (reparseInfo) ModTime() time.Time { return time.Time{} }
func (reparseInfo) IsDir() bool        { return true }
func (reparseInfo) Sys() any {
	return &syscall.Win32FileAttributeData{FileAttributes: syscall.FILE_ATTRIBUTE_REPARSE_POINT}
}

func TestWindowsReparseAndCaseAlias(t *testing.T) {
	if !unsafeFileInfo(reparseInfo{}) {
		t.Fatal("Windows reparse attribute was ignored")
	}
	manifest := writeFixture(t, map[string]string{"report.xml": healthyXML}, `{"version":1,"files":["report.xml","REPORT.XML"]}`)
	if _, err := os.Stat(filepath.Join(filepath.Dir(manifest), "REPORT.XML")); err != nil {
		t.Skip("test volume is case-sensitive")
	}
	requireCode(t, Check(manifest, Options{DuplicateScope: "file"}), "report_alias", 1)
}
