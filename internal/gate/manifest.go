package gate

import (
	"bytes"
	"encoding/json"
	"io"
	"math"
	"os"
	"unicode/utf8"
)

func readManifest(f *os.File, limits Limits) ([]string, string) {
	before, err := f.Stat()
	if err != nil {
		return nil, "manifest_read_error"
	}
	if before.Size() > limits.MaxManifestBytes {
		return nil, "manifest_too_large"
	}
	n := limits.MaxManifestBytes
	if n < math.MaxInt64 {
		n++
	}
	data, err := io.ReadAll(io.LimitReader(f, n))
	if err != nil {
		return nil, "manifest_read_error"
	}
	if int64(len(data)) > limits.MaxManifestBytes {
		return nil, "manifest_too_large"
	}
	after, err := f.Stat()
	if err != nil || !sameSnapshot(before, after) || int64(len(data)) != before.Size() {
		return nil, "manifest_changed"
	}
	if !utf8.Valid(data) {
		return nil, "manifest_invalid_json"
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	start, err := decoder.Token()
	if err != nil {
		return nil, "manifest_invalid_json"
	}
	if start != json.Delim('{') {
		return nil, "manifest_schema"
	}
	seen := map[string]bool{}
	var files []string
	for decoder.More() {
		token, tokenErr := decoder.Token()
		if tokenErr != nil {
			return nil, "manifest_invalid_json"
		}
		key, ok := token.(string)
		if !ok || seen[key] {
			return nil, "manifest_schema"
		}
		seen[key] = true
		var raw json.RawMessage
		if decoder.Decode(&raw) != nil {
			return nil, "manifest_invalid_json"
		}
		switch key {
		case "version":
			if !bytes.Equal(bytes.TrimSpace(raw), []byte("1")) {
				return nil, "manifest_version"
			}
		case "files":
			if len(raw) == 0 || raw[0] != '[' || json.Unmarshal(raw, &files) != nil {
				return nil, "manifest_schema"
			}
		default:
			return nil, "manifest_schema"
		}
	}
	if end, endErr := decoder.Token(); endErr != nil || end != json.Delim('}') {
		return nil, "manifest_invalid_json"
	}
	if _, endErr := decoder.Token(); endErr != io.EOF {
		return nil, "manifest_invalid_json"
	}
	if !seen["version"] || !seen["files"] {
		return nil, "manifest_schema"
	}
	if len(files) == 0 {
		return nil, "manifest_empty"
	}
	if len(files) > limits.MaxFiles {
		return nil, "manifest_files_limit"
	}
	return files, ""
}
