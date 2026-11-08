package packs

import (
	"fmt"
	"path"
	"strings"
)

// Reader reads a file of the repository at the reviewed commit, by its
// slash-separated path.
type Reader interface {
	ReadFile(path string) ([]byte, error)
}

// Activate returns, in set order, the packs with a detect sign that
// matches the repository, given the paths of its files.
func (s *Set) Activate(paths []string, repo Reader) ([]*Pack, error) {
	var active []*Pack
	for _, p := range s.Packs {
		for _, sg := range p.Detect {
			ok, err := sg.matches(paths, repo)
			if err != nil {
				return nil, fmt.Errorf("pack %s: detect: %w", p.ID, err)
			}
			if ok {
				active = append(active, p)
				break
			}
		}
	}
	return active, nil
}

// matches reports whether a file or directory of the repository matches
// the sign. A sign with contains matches only a file that contains the
// text; only such files are read.
func (s Sign) matches(paths []string, repo Reader) (bool, error) {
	for _, p := range paths {
		if s.Path.Match(p) {
			if s.Contains == "" {
				return true, nil
			}
			data, err := repo.ReadFile(p)
			if err != nil {
				return false, fmt.Errorf("read %s: %w", p, err)
			}
			if strings.Contains(string(data), s.Contains) {
				return true, nil
			}
			continue
		}
		if s.Contains != "" {
			continue
		}
		for dir := path.Dir(p); dir != "." && dir != "/"; dir = path.Dir(dir) {
			if s.Path.Match(dir) {
				return true, nil
			}
		}
	}
	return false, nil
}
