package repos

import (
	"bytes"
	"path/filepath"
	"strings"
)

// BV-186: the tracked-secret scan does not open images, documents or other
// binary files. Two guards: a fixed list of binary extensions (skipped by name,
// never opened), and a NUL byte in the first bytes of anything else (a binary
// file without a listed extension).

// binaryExtensions are the lowercase file extensions (with the dot) the scan
// skips without opening. Images, video, audio, documents, archives, fonts and
// executables/objects.
var binaryExtensions = map[string]bool{
	// images
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true, ".bmp": true,
	".tif": true, ".tiff": true, ".ico": true, ".heic": true, ".heif": true, ".psd": true,
	".ai": true, ".eps": true, ".raw": true, ".cr2": true, ".nef": true,
	// video
	".mp4": true, ".mov": true, ".avi": true, ".mkv": true, ".webm": true, ".wmv": true, ".m4v": true,
	// audio
	".mp3": true, ".wav": true, ".flac": true, ".aac": true, ".ogg": true, ".m4a": true,
	// documents
	".pdf": true, ".doc": true, ".docx": true, ".xls": true, ".xlsx": true, ".ppt": true, ".pptx": true,
	".odt": true, ".ods": true, ".odp": true, ".pages": true, ".numbers": true, ".key": true,
	".rtf": true, ".epub": true,
	// archives
	".zip": true, ".7z": true, ".rar": true, ".tar": true, ".gz": true, ".tgz": true, ".bz2": true,
	".xz": true, ".zst": true, ".jar": true, ".war": true, ".ear": true, ".apk": true, ".ipa": true,
	".dmg": true, ".iso": true,
	// fonts
	".woff": true, ".woff2": true, ".ttf": true, ".otf": true, ".eot": true,
	// executables and objects
	".exe": true, ".dll": true, ".so": true, ".dylib": true, ".bin": true, ".o": true, ".a": true,
	".lib": true, ".class": true, ".pyc": true, ".pyo": true, ".wasm": true, ".msi": true,
	".deb": true, ".rpm": true,
}

// binaryExtSniff is how many leading bytes the NUL check looks at (BV-186).
const binaryExtSniff = 8000

// isBinaryExt reports whether relPath's extension is a known binary type, so
// the file is skipped by name without being opened.
func isBinaryExt(relPath string) bool {
	return binaryExtensions[strings.ToLower(filepath.Ext(relPath))]
}

// looksBinary reports whether the first binaryExtSniff bytes of content hold a
// NUL byte, the mark of a binary file with no listed extension.
func looksBinary(content []byte) bool {
	n := len(content)
	if n > binaryExtSniff {
		n = binaryExtSniff
	}
	return bytes.IndexByte(content[:n], 0) >= 0
}
