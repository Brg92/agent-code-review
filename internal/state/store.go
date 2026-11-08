package state

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"time"
	"unicode/utf8"

	"github.com/Brg92/agent-code-review/internal/core"
)

// FileName is the name of the state file in a run's work directory.
const FileName = "state.json"

// SchemaVersion is the version of the state file format.
const SchemaVersion = 1

// InvalidStateError reports a state file that cannot be loaded.
type InvalidStateError struct {
	Path string
	Err  error
}

func (e *InvalidStateError) Error() string { return fmt.Sprintf("invalid state %s: %v", e.Path, e.Err) }

func (e *InvalidStateError) Unwrap() error { return e.Err }

// file is the JSON form of a State; the field order is the file's.
type file struct {
	Schema         int          `json:"schema"`
	RunID          string       `json:"runId"`
	Forge          core.Forge   `json:"forge"`
	Root           core.Key     `json:"root"`
	Phase          Phase        `json:"phase"`
	Cascade        core.Limits  `json:"cascade"`
	CreatedAt      string       `json:"createdAt"`
	UpdatedAt      string       `json:"updatedAt"`
	ChangeRequests []core.Entry `json:"changeRequests"`
}

// timestampPattern is the timestamp pattern of the state schema.
var timestampPattern = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(?:\.[0-9]+)?Z$`)

func formatTime(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func parseTime(name, s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, fmt.Errorf("missing %s", name)
	}
	if !timestampPattern.MatchString(s) {
		return time.Time{}, fmt.Errorf("%s %q is not a UTC RFC 3339 time", name, s)
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("%s: %w", name, err)
	}
	return t, nil
}

// Save writes s to FileName in dir, which must exist. It refuses a state
// that does not validate. The file is written to a temporary file in dir
// and renamed over FileName, so a reader sees either the old or the new
// state.
func Save(dir string, s *State) error {
	if err := s.Validate(); err != nil {
		return err
	}
	data, err := encode(s)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".state-*.tmp")
	if err != nil {
		return fmt.Errorf("save state: %w", err)
	}
	done := false
	defer func() {
		if !done {
			tmp.Close()
			os.Remove(tmp.Name())
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		return fmt.Errorf("save state: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("save state: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("save state: %w", err)
	}
	if err := os.Rename(tmp.Name(), filepath.Join(dir, FileName)); err != nil {
		os.Remove(tmp.Name())
		done = true
		return fmt.Errorf("save state: %w", err)
	}
	done = true
	return nil
}

// encode returns the JSON of s: two-space indent, no HTML escaping, a
// final newline.
func encode(s *State) ([]byte, error) {
	f := file{
		Schema:         SchemaVersion,
		RunID:          s.RunID,
		Forge:          s.Forge,
		Root:           s.Root,
		Phase:          s.Phase,
		Cascade:        s.Cascade,
		CreatedAt:      formatTime(s.CreatedAt),
		UpdatedAt:      formatTime(s.UpdatedAt),
		ChangeRequests: s.ChangeRequests,
	}
	if f.ChangeRequests == nil {
		f.ChangeRequests = []core.Entry{}
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(f); err != nil {
		return nil, fmt.Errorf("save state: %w", err)
	}
	return buf.Bytes(), nil
}

// Load reads FileName from dir. A missing file returns an error matching
// fs.ErrNotExist; a file that is not a valid state returns an
// *InvalidStateError, which wraps a *ConsistencyError when the problem is
// one Validate finds. Resuming a run is loading its state and continuing
// from its phase.
func Load(dir string) (*State, error) {
	path := filepath.Join(dir, FileName)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	s, err := decode(data)
	if err != nil {
		return nil, &InvalidStateError{Path: path, Err: err}
	}
	return s, nil
}

func decode(data []byte) (*State, error) {
	if !utf8.Valid(data) {
		return nil, errors.New("not valid UTF-8")
	}
	var version struct {
		Schema *int `json:"schema"`
	}
	if err := json.Unmarshal(data, &version); err != nil {
		return nil, err
	}
	switch {
	case version.Schema == nil:
		return nil, errors.New("missing schema")
	case *version.Schema != SchemaVersion:
		return nil, fmt.Errorf("unsupported schema version %d", *version.Schema)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var f file
	if err := dec.Decode(&f); err != nil {
		return nil, err
	}
	if f.ChangeRequests == nil {
		return nil, errors.New("missing changeRequests")
	}
	created, err := parseTime("createdAt", f.CreatedAt)
	if err != nil {
		return nil, err
	}
	updated, err := parseTime("updatedAt", f.UpdatedAt)
	if err != nil {
		return nil, err
	}
	s := &State{
		RunID:          f.RunID,
		Forge:          f.Forge,
		Root:           f.Root,
		Phase:          f.Phase,
		Cascade:        f.Cascade,
		CreatedAt:      created,
		UpdatedAt:      updated,
		ChangeRequests: f.ChangeRequests,
	}
	if err := s.Validate(); err != nil {
		return nil, err
	}
	return s, nil
}
