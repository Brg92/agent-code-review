// Command digestcheck compares the digests of the release tree that the CI
// jobs of each system wrote. It is a development tool and never ships.
//
// Usage:
//
//	go run ./tools/digestcheck -n <count> <directory>
//
// It looks for files named digest.txt anywhere below <directory>, as the
// download of the CI artifacts lays them out. There must be exactly <count>
// of them, each one line with a SHA-256 in lower-case hexadecimal, and all
// equal. Exit codes: 0 they agree, 1 they differ or a file is not valid,
// 2 usage error.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
)

const (
	usage      = "usage: digestcheck -n <count> <directory>\n"
	digestFile = "digest.txt"
)

var sha256Line = regexp.MustCompile(`^[0-9a-f]{64}\n$`)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	fl := flag.NewFlagSet("digestcheck", flag.ContinueOnError)
	fl.SetOutput(stderr)
	n := fl.Int("n", 0, "number of digest files that must be found")
	if err := fl.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if *n < 1 || fl.NArg() != 1 {
		fmt.Fprint(stderr, usage)
		return 2
	}

	var paths []string
	err := filepath.WalkDir(fl.Arg(0), func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && d.Name() == digestFile {
			paths = append(paths, p)
		}
		return err
	})
	if err != nil {
		fmt.Fprintf(stderr, "digestcheck: %v\n", err)
		return 1
	}
	sort.Strings(paths)
	if len(paths) != *n {
		fmt.Fprintf(stderr, "digestcheck: found %d digest files, want %d\n", len(paths), *n)
		return 1
	}

	digests := map[string]string{}
	distinct := map[string]bool{}
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			fmt.Fprintf(stderr, "digestcheck: %v\n", err)
			return 1
		}
		if !sha256Line.Match(data) {
			fmt.Fprintf(stderr, "digestcheck: %s is not a SHA-256 on one line\n", p)
			return 1
		}
		digests[p] = string(data[:64])
		distinct[digests[p]] = true
	}
	if len(distinct) != 1 {
		fmt.Fprintln(stderr, "digestcheck: the digests differ:")
		for _, p := range paths {
			fmt.Fprintf(stderr, "  %s  %s\n", digests[p], p)
		}
		return 1
	}
	fmt.Fprintf(stdout, "%d digests, all %s\n", len(paths), digests[paths[0]])
	return 0
}
