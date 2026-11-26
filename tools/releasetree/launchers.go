package main

import (
	"bytes"
	"fmt"
	"strings"
)

// maxLauncherBytes keeps a launcher small: it only picks an executable.
const maxLauncherBytes = 2 * 1024

// launcher is a file of plugin/bin that is copied to bin/ of the tree.
type launcher struct {
	// Name is the file name, in plugin/bin and in bin.
	Name string
	// Executable marks it with git mode 100755.
	Executable bool
	// Start is how the file must begin, which tells what it is.
	Start string
	// CRLF is for a batch file, which Windows reads with CRLF line endings;
	// the sources have LF, as the repository's .gitattributes say.
	CRLF bool
}

var launchers = []launcher{
	{Name: "agent-code-review", Executable: true, Start: "#!/bin/sh\n"},
	{Name: "agent-code-review.cmd", Start: "@echo off\n", CRLF: true},
}

const (
	launcherSrcDir  = "plugin/bin/"
	launcherTreeDir = "bin/"
)

// isExecutable reports whether the file at path of the tree has git mode
// 100755. Only the shell launcher has.
func isExecutable(path string) bool {
	for _, l := range launchers {
		if l.Executable && path == launcherTreeDir+l.Name {
			return true
		}
	}
	return false
}

// gitMode is the git mode of the file at path of the tree.
func gitMode(path string) string {
	if isExecutable(path) {
		return "100755"
	}
	return "100644"
}

// treeModes lists the git mode of every file, one "<mode> <path>" per line in
// path order. A file system without an executable bit loses the mode, so
// whoever builds the commit takes it from here.
func treeModes(files map[string][]byte) string {
	var b strings.Builder
	for _, p := range sortedKeys(files) {
		fmt.Fprintf(&b, "%s %s\n", gitMode(p), p)
	}
	return b.String()
}

// addLaunchers reads the launchers of plugin/bin into the tree. Each must be
// what its name says and small; the batch launcher gets CRLF line endings.
func addLaunchers(files map[string][]byte, read func(string) ([]byte, error)) error {
	for _, l := range launchers {
		src := launcherSrcDir + l.Name
		data, err := read(src)
		if err != nil {
			return err
		}
		if !bytes.HasPrefix(data, []byte(l.Start)) {
			return fmt.Errorf("launcher %s must start with %q", src, strings.TrimSuffix(l.Start, "\n"))
		}
		if len(data) > maxLauncherBytes {
			return fmt.Errorf("launcher %s is %d bytes, over the limit of %d", src, len(data), maxLauncherBytes)
		}
		if l.CRLF {
			data = bytes.ReplaceAll(data, []byte("\n"), []byte("\r\n"))
		}
		files[launcherTreeDir+l.Name] = data
	}
	return nil
}
