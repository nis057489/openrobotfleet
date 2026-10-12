package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"sync"
)

// BinarySHA256 is the hex SHA-256 of the running agent executable, or "" if
// it cannot be read. The controller compares it with the agent builds it
// ships to tell whether this agent is out of date. Computed once: an update
// replaces the file and restarts the process.
var BinarySHA256 = sync.OnceValue(func() string {
	path, err := os.Executable()
	if err != nil {
		return ""
	}
	return fileSHA256(path)
})

func fileSHA256(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return ""
	}
	return hex.EncodeToString(h.Sum(nil))
}

// FileSHA256 hashes a file the way BinarySHA256 does, for the controller to
// fingerprint the agent builds it holds.
func FileSHA256(path string) string { return fileSHA256(path) }
