package review

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"

	"github.com/Brg92/agent-code-review/internal/core"
	"github.com/Brg92/agent-code-review/internal/packs"
)

// maxCriteriaFile is the largest criteria file that is read: far more than
// a list of rules needs, and a limit for a file that is not one.
const maxCriteriaFile = 1 << 20

// localProject is the project the criteria of a working copy are read for:
// a working copy has none, and nothing here depends on it.
var localProject = core.Key{Forge: core.GitHub, Host: "github.com", Project: "local/working-copy", Number: 1}

// CheckCriteria reads the criteria of the working copy in dir the way a
// review reads them from the target branch of a repository: the neutral
// layout, or the legacy one, with the packs named in them looked up in set.
// It reads files in dir and nothing else: it does not look in the
// directories above, it follows no link out of dir, it never reads git
// objects and it needs no token. What a review would ignore is in Ignored,
// and Notes; a configuration file that is not valid is one ignored item
// that names every invalid field, as in a review.
func CheckCriteria(ctx context.Context, dir string, set *packs.Set) (core.RepoCriteria, error) {
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return core.RepoCriteria{}, &Failure{Kind: KindUsage, What: "the directory to check is not a directory", Where: dir,
			Why: "it does not exist or is not a directory", Hint: "give the working copy of a repository, or no argument for the current directory"}
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return core.RepoCriteria{}, &Failure{Kind: KindUnexpected, What: "could not open the directory to check", Where: dir, Why: err.Error(), Err: err,
			Hint: "check that the directory can be read"}
	}
	defer root.Close()
	const ref = "the working copy"
	c, err := core.ReadCriteria(ctx, localFiles{root}, core.CriteriaInput{Project: localProject, Ref: ref, Rules: set.Rules})
	var invalid *core.CriteriaError
	switch {
	case errors.As(err, &invalid):
		return core.IgnoreInvalidCriteria(localProject, ref, invalid), nil
	case err != nil:
		var f *Failure
		if errors.As(err, &f) {
			return core.RepoCriteria{}, f
		}
		return core.RepoCriteria{}, &Failure{Kind: KindUnexpected, What: "could not read the criteria", Where: dir, Why: err.Error(), Err: err,
			Hint: "check that the files can be read, and that none is a link to a file outside the directory"}
	}
	namedPacks(set, &c)
	return c, nil
}

// localFiles is core.Files over a directory: the criteria of a working
// copy. Whatever the project and ref, it reads the directory.
type localFiles struct{ root *os.Root }

// Get implements core.Files.
func (l localFiles) Get(_ context.Context, _ core.Key, _, p string) ([]byte, bool, error) {
	f, err := l.root.Open(filepath.FromSlash(p))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, localFailure(p, err)
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxCriteriaFile+1))
	switch {
	case err != nil:
		return nil, false, localFailure(p, err)
	case len(data) > maxCriteriaFile:
		return nil, false, localFailure(p, fmt.Errorf("the file is larger than %d bytes", maxCriteriaFile))
	}
	return data, true, nil
}

// List implements core.Files.
func (l localFiles) List(_ context.Context, _ core.Key, _, dir string) ([]core.DirEntry, error) {
	d, err := l.root.Open(filepath.FromSlash(path.Clean(dir)))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, localFailure(dir, err)
	}
	defer d.Close()
	entries, err := d.ReadDir(-1)
	if err != nil {
		return nil, localFailure(dir, err)
	}
	out := make([]core.DirEntry, 0, len(entries))
	for _, e := range entries {
		out = append(out, core.DirEntry{Name: e.Name(), Dir: e.IsDir()})
	}
	return out, nil
}

func localFailure(p string, err error) error {
	return &Failure{Kind: KindUnexpected, What: "could not read a criteria file", Where: p, Why: err.Error(), Err: err,
		Hint: "check that the file can be read, and that it is not a link to a file outside the directory"}
}
