package state

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/Brg92/agent-code-review/internal/core"
)

// WorkDirEnv names the environment variable that replaces the default base
// of the work directories, <user cache dir>/agent-code-review.
const WorkDirEnv = "ACR_WORKDIR"

// Files of a run's work directory, and of the directory of each change
// request in it.
const (
	CascadeFile     = "cascade.txt"
	WorkPackageFile = "workpackage.json"
	JudgementFile   = "judgement.json"
	FindingsFile    = "findings.json"
	ReportFile      = "report.txt"
)

// runsDir is the directory of the run directories under the base.
const runsDir = "runs"

// privateDir is the mode of the directories the CLI creates: only the user
// may read them.
const privateDir = 0o700

var headPattern = regexp.MustCompile(`^[0-9a-f]{8}`)

// WorkDirBase returns the base of the work directories: the absolute path
// in WorkDirEnv, or agent-code-review in the user cache directory.
func WorkDirBase(getenv func(string) string, userCacheDir func() (string, error)) (string, error) {
	if dir := getenv(WorkDirEnv); dir != "" {
		if !filepath.IsAbs(dir) {
			return "", fmt.Errorf("%s must be an absolute path, not %q", WorkDirEnv, dir)
		}
		return filepath.Clean(dir), nil
	}
	cache, err := userCacheDir()
	if err != nil {
		return "", fmt.Errorf("work directory: %w", err)
	}
	return filepath.Join(cache, "agent-code-review"), nil
}

// CreateRunDir creates, if needed, the work directory of a run,
// <base>/runs/<runID>, and returns its path.
func CreateRunDir(base, runID string) (string, error) {
	if !ValidRunID(runID) {
		return "", fmt.Errorf("work directory: %q is not a run id", runID)
	}
	dir := filepath.Join(base, runsDir, runID)
	if err := os.MkdirAll(dir, privateDir); err != nil {
		return "", fmt.Errorf("work directory: %w", err)
	}
	return dir, nil
}

// ChangeRequestDirName is the name of the directory of a change request in
// a run: <project with / as _>-<number>-<first 8 characters of head>.
func ChangeRequestDirName(key core.Key, head string) (string, error) {
	if !headPattern.MatchString(head) {
		return "", fmt.Errorf("work directory: head %q does not start with 8 lowercase hexadecimal characters", head)
	}
	if key.IsZero() || key.Number < 1 {
		return "", errors.New("work directory: no change request")
	}
	return strings.ReplaceAll(key.Project, "/", "_") + "-" + strconv.Itoa(key.Number) + "-" + head[:8], nil
}

// CreateChangeRequestDir creates, if needed, the directory of a change
// request in the work directory of a run, and returns its path.
func CreateChangeRequestDir(runDir string, key core.Key, head string) (string, error) {
	name, err := ChangeRequestDirName(key, head)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(runDir, name)
	if err := os.Mkdir(dir, privateDir); err != nil && !errors.Is(err, os.ErrExist) {
		return "", fmt.Errorf("work directory: %w", err)
	}
	return dir, nil
}
