package daemon

import (
	"crypto/rand"
	"encoding/base32"
	"strconv"
	"strings"
	"time"
)

// newID returns a time-ordered, URL-safe identifier (ULID-like without an
// external dependency): a millisecond timestamp prefix plus 80 bits of random.
func newID() string {
	var buf [10]byte
	_, _ = rand.Read(buf[:])
	ts := strconv.FormatInt(time.Now().UnixMilli(), 36)
	enc := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(buf[:])
	return strings.ToUpper(ts) + enc
}
