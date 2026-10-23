package gate

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

type pathSnapshot struct {
	name string
	info fs.FileInfo
}

// The checks detect ordinary replacement and reject observed links. They are not
// an atomic filesystem snapshot. Root.Open contains traversal even if a checked
// component is replaced concurrently; the held file is checked independently.
type pathGuard struct {
	root  *os.Root
	parts []pathSnapshot
}

func (g pathGuard) unchanged() bool {
	for _, part := range g.parts {
		var info fs.FileInfo
		var err error
		if g.root == nil {
			info, err = os.Lstat(part.name)
		} else {
			info, err = g.root.Lstat(part.name)
		}
		if err != nil || unsafeFileInfo(info) || !os.SameFile(part.info, info) {
			return false
		}
		if part.info.Mode().IsRegular() && !sameSnapshot(part.info, info) {
			return false
		}
	}
	return true
}

func unsafeFileInfo(info fs.FileInfo) bool {
	return info.Mode()&os.ModeSymlink != 0 || isReparse(info)
}

func sameSnapshot(before, after fs.FileInfo) bool {
	return before != nil && after != nil && os.SameFile(before, after) &&
		before.Size() == after.Size() && before.ModTime().Equal(after.ModTime()) && after.Mode().IsRegular()
}

func openManifest(name string) (*os.File, *os.Root, pathGuard, string) {
	absolute, err := filepath.Abs(name)
	if err != nil || name == "" {
		return nil, nil, pathGuard{}, "manifest_read_error"
	}
	// The caller-selected manifest directory is the trusted root location.
	// Canonicalize that location once so OS aliases such as macOS /var work;
	// never canonicalize the manifest file or any declared report path.
	directory, err := filepath.EvalSymlinks(filepath.Dir(absolute))
	if err != nil {
		return nil, nil, pathGuard{}, "manifest_read_error"
	}
	directoryInfo, err := os.Lstat(directory)
	if err != nil || unsafeFileInfo(directoryInfo) {
		return nil, nil, pathGuard{}, "manifest_unsafe_path"
	}
	before, err := os.Lstat(filepath.Join(directory, filepath.Base(absolute)))
	if err != nil {
		return nil, nil, pathGuard{}, "manifest_read_error"
	}
	if unsafeFileInfo(before) {
		return nil, nil, pathGuard{}, "manifest_unsafe_path"
	}
	if !before.Mode().IsRegular() {
		return nil, nil, pathGuard{}, "manifest_not_regular"
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return nil, nil, pathGuard{}, "manifest_read_error"
	}
	openedDirectory, err := root.Stat(".")
	if err != nil || !os.SameFile(directoryInfo, openedDirectory) {
		root.Close()
		return nil, nil, pathGuard{}, "manifest_changed"
	}
	guard := pathGuard{root: root, parts: []pathSnapshot{{".", directoryInfo}, {filepath.Base(absolute), before}}}
	f, err := root.OpenFile(filepath.Base(absolute), readFlags(), 0)
	if err != nil {
		root.Close()
		return nil, nil, pathGuard{}, "manifest_read_error"
	}
	info, err := f.Stat()
	if err != nil || !sameSnapshot(before, info) || !guard.unchanged() {
		f.Close()
		root.Close()
		return nil, nil, pathGuard{}, "manifest_changed"
	}
	return f, root, guard, ""
}

func openReport(root *os.Root, name string) (*os.File, fs.FileInfo, pathGuard, string) {
	guard := pathGuard{root: root}
	current := ""
	components := strings.Split(name, "/")
	for i, part := range components {
		current = filepath.Join(current, part)
		info, err := root.Lstat(current)
		if err != nil {
			if os.IsNotExist(err) {
				return nil, nil, guard, "report_missing"
			}
			return nil, nil, guard, "report_read_error"
		}
		if unsafeFileInfo(info) {
			return nil, nil, guard, "report_unsafe_path"
		}
		if i < len(components)-1 && !info.IsDir() {
			return nil, nil, guard, "report_not_regular"
		}
		guard.parts = append(guard.parts, pathSnapshot{current, info})
	}
	before := guard.parts[len(guard.parts)-1].info
	if !before.Mode().IsRegular() {
		return nil, nil, guard, "report_not_regular"
	}
	f, err := root.OpenFile(filepath.FromSlash(name), readFlags(), 0)
	if err != nil {
		return nil, nil, guard, "report_read_error"
	}
	info, err := f.Stat()
	if err != nil || !sameSnapshot(before, info) || !guard.unchanged() {
		f.Close()
		return nil, nil, guard, "report_changed"
	}
	return f, info, guard, ""
}

func validReportPath(name string) bool {
	if name == "" || strings.ContainsAny(name, "\\:<>\"|?*\x00") || filepath.IsAbs(name) {
		return false
	}
	for _, ch := range name {
		if ch < 32 || ch == 127 {
			return false
		}
	}
	for _, part := range strings.Split(name, "/") {
		if part == "" || part == "." || part == ".." || strings.HasSuffix(part, ".") || strings.HasSuffix(part, " ") {
			return false
		}
		base, _, _ := strings.Cut(part, ".")
		upper := strings.ToUpper(base)
		if upper == "CON" || upper == "PRN" || upper == "AUX" || upper == "NUL" ||
			(len(upper) == 4 && (strings.HasPrefix(upper, "COM") || strings.HasPrefix(upper, "LPT")) && upper[3] >= '0' && upper[3] <= '9') {
			return false
		}
	}
	return true
}
