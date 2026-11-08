package state

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"regexp"
	"time"
)

// runIDPattern is the runId pattern of the state schema. A run id is also
// the name of the run's work directory.
var runIDPattern = regexp.MustCompile(`^[A-Za-z0-9-]{8,64}$`)

// ValidRunID reports whether id is an acceptable run id.
func ValidRunID(id string) bool { return runIDPattern.MatchString(id) }

// NewRunID returns a run id YYYYMMDDThhmmssZ-xxxxxxxx: now in UTC, then 8
// lowercase hexadecimal characters from 4 bytes of random.
func NewRunID(now time.Time, random io.Reader) (string, error) {
	b := make([]byte, 4)
	if _, err := io.ReadFull(random, b); err != nil {
		return "", fmt.Errorf("run id: %w", err)
	}
	return now.UTC().Format("20060102T150405Z") + "-" + hex.EncodeToString(b), nil
}

// RandomRunID returns NewRunID(now, crypto/rand.Reader).
func RandomRunID(now time.Time) (string, error) { return NewRunID(now, rand.Reader) }
