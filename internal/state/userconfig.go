package state

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/Brg92/agent-code-review/internal/core"
)

// DefaultKeepDays is how many days a run's work directory is kept when the
// user configuration does not say.
const DefaultKeepDays = 7

// UserConfig is the user's configuration, as defined by
// schemas/user-config.schema.json. It never holds a token.
type UserConfig struct {
	// PackPaths are extra pack directories, absolute and cleaned.
	PackPaths []string
	// KeepDays is how many days a run's work directory is kept; 0 keeps
	// every run.
	KeepDays int
}

// UserConfigError reports a user configuration file that is not valid:
// every invalid field, by JSON pointer.
type UserConfigError struct {
	Path   string
	Errors []core.FieldError
}

func (e *UserConfigError) Error() string {
	var b strings.Builder
	b.WriteString("invalid user configuration " + e.Path + ":")
	for _, f := range e.Errors {
		b.WriteString("\n" + f.Pointer + ": " + f.Message)
	}
	return b.String()
}

// UserConfigPath returns the path of the user configuration:
// agent-code-review/config.json in the user configuration directory.
func UserConfigPath(userConfigDir func() (string, error)) (string, error) {
	dir, err := userConfigDir()
	if err != nil {
		return "", fmt.Errorf("user configuration: %w", err)
	}
	return filepath.Join(dir, "agent-code-review", "config.json"), nil
}

// LoadUserConfig reads the user configuration at path. A missing file
// gives the defaults; a file that is not valid fails with a
// *UserConfigError.
func LoadUserConfig(path string) (UserConfig, error) {
	c := UserConfig{KeepDays: DefaultKeepDays}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return UserConfig{}, fmt.Errorf("user configuration: %w", err)
	}
	var errs []core.FieldError
	add := func(pointer, format string, args ...any) {
		errs = append(errs, core.FieldError{Pointer: pointer, Message: fmt.Sprintf(format, args...)})
	}
	var obj map[string]json.RawMessage
	dec := json.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(&obj); err != nil || obj == nil {
		add("", "not a JSON object")
	} else if _, err := dec.Token(); err != io.EOF {
		add("", "trailing data after the document")
	}
	if len(errs) == 0 {
		if _, ok := obj["schema"]; !ok {
			add("/schema", "is required")
		}
		for name, raw := range obj {
			switch name {
			case "schema":
				if n, ok := integer(raw); !ok || n != 1 {
					add("/schema", "%s is not 1", raw)
				}
			case "keepDays":
				n, ok := integer(raw)
				if !ok || n < 0 {
					add("/keepDays", "%s is not an integer of at least 0", raw)
					continue
				}
				c.KeepDays = n
			case "packPaths":
				var paths []string
				if err := json.Unmarshal(raw, &paths); err != nil || paths == nil {
					add("/packPaths", "must be an array of strings")
					continue
				}
				for i, p := range paths {
					if !rooted(p) {
						add("/packPaths/"+strconv.Itoa(i), "%q is not an absolute path", p)
						continue
					}
					c.PackPaths = append(c.PackPaths, filepath.Clean(filepath.FromSlash(p)))
				}
			default:
				add("/"+name, "is not a field of the user configuration")
			}
		}
	}
	if len(errs) > 0 {
		sort.SliceStable(errs, func(i, j int) bool { return errs[i].Pointer < errs[j].Pointer })
		return UserConfig{}, &UserConfigError{Path: path, Errors: errs}
	}
	return c, nil
}

// rooted reports whether p is an absolute path; on Windows, a path from
// the root of the current drive, such as /opt/packs, counts too.
func rooted(p string) bool {
	return strings.TrimSpace(p) != "" && (filepath.IsAbs(p) || strings.HasPrefix(p, "/") || strings.HasPrefix(p, `\`))
}

// integer decodes a JSON integer, not a string that holds one; a number
// with an integral value, such as 1.0, is one, as in JSON Schema.
func integer(raw json.RawMessage) (int, bool) {
	t := bytes.TrimSpace(raw)
	if len(t) == 0 || t[0] == '"' {
		return 0, false
	}
	var f float64
	if err := json.Unmarshal(t, &f); err != nil || f != math.Trunc(f) || math.Abs(f) > math.MaxInt32 {
		return 0, false
	}
	return int(f), true
}
