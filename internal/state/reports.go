package state

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Brg92/agent-code-review/internal/core"
)

// reportExt is the extension of a saved report.
const reportExt = ".txt"

// InsideGitError reports a folder for saved reports that is inside a git
// work tree: a report there could be committed with the code.
type InsideGitError struct {
	Dir string
	// WorkTree is the folder that holds .git.
	WorkTree string
}

func (e *InsideGitError) Error() string {
	return fmt.Sprintf("cannot save reports in %s: it is inside the git work tree %s", e.Dir, e.WorkTree)
}

// DefaultReportsDir returns the folder reports are saved to when the user
// names none: agent-code-review/reports in the home directory.
func DefaultReportsDir(userHomeDir func() (string, error)) (string, error) {
	home, err := userHomeDir()
	if err != nil {
		return "", fmt.Errorf("reports folder: %w", err)
	}
	return filepath.Join(home, "agent-code-review", "reports"), nil
}

// ReportName is the file name of the saved report of a change request:
// <last segment of the project>-<number>-<head8>_<yyyy-MM-dd_HHmm>.txt.
func ReportName(key core.Key, head string, at time.Time) (string, error) {
	if !headPattern.MatchString(head) {
		return "", fmt.Errorf("report name: head %q does not start with 8 lowercase hexadecimal characters", head)
	}
	return path.Base(key.Project) + "-" + strconv.Itoa(key.Number) + "-" + head[:8] + "_" + at.Format("2006-01-02_1504") + reportExt, nil
}

// RootReport is the content of the saved report of the root change
// request: its report, a blank line and the cascade summary.
func RootReport(report, cascade string) string {
	return strings.TrimRight(report, "\r\n") + "\n\n" + strings.TrimRight(cascade, "\r\n") + "\n"
}

// SaveReport writes content, with LF line endings and a final newline, to
// a new file named name in the absolute folder dir, which is created if
// needed. When name is taken, _2, _3 and so on are added before the
// extension. It refuses with an *InsideGitError a folder inside a git work
// tree, and returns the path of the file it wrote.
func SaveReport(dir, name, content string) (string, error) {
	if !filepath.IsAbs(dir) {
		return "", fmt.Errorf("save report: %q is not an absolute path", dir)
	}
	if name == "" || name != filepath.Base(name) || strings.ContainsAny(name, `/\`) || !strings.HasSuffix(name, reportExt) || name == reportExt {
		return "", fmt.Errorf("save report: %q is not a report file name", name)
	}
	dir = filepath.Clean(dir)
	if err := checkOutsideGit(dir); err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, privateDir); err != nil {
		return "", fmt.Errorf("save report: %w", err)
	}
	data := []byte(strings.TrimRight(strings.ReplaceAll(content, "\r\n", "\n"), "\n") + "\n")
	stem := strings.TrimSuffix(name, reportExt)
	for n := 1; ; n++ {
		file := name
		if n > 1 {
			file = stem + "_" + strconv.Itoa(n) + reportExt
		}
		p := filepath.Join(dir, file)
		f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			return "", fmt.Errorf("save report: %w", err)
		}
		_, werr := f.Write(data)
		if cerr := f.Close(); werr == nil {
			werr = cerr
		}
		if werr != nil {
			os.Remove(p)
			return "", fmt.Errorf("save report: %w", werr)
		}
		return p, nil
	}
}

// checkOutsideGit walks up from dir, and from the real path of its deepest
// existing folder, looking for .git, a folder or the file of a linked work
// tree.
func checkOutsideGit(dir string) error {
	starts := []string{dir}
	existing := dir
	for {
		if _, err := os.Stat(existing); err == nil {
			break
		}
		parent := filepath.Dir(existing)
		if parent == existing {
			break
		}
		existing = parent
	}
	if real, err := filepath.EvalSymlinks(existing); err == nil && real != existing {
		starts = append(starts, real)
	}
	for _, start := range starts {
		for d := start; ; d = filepath.Dir(d) {
			if _, err := os.Lstat(filepath.Join(d, ".git")); err == nil {
				return &InsideGitError{Dir: dir, WorkTree: d}
			}
			if filepath.Dir(d) == d {
				break
			}
		}
	}
	return nil
}
