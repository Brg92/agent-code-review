// Command releasetree writes the tree of the release branch: the manifests of
// every host with the version injected, one agent per host translated from the
// neutral source, the skills, the packs, the schemas, the licence and the
// README and the two launchers of bin/, restricted to
// docs/release-allowlist.txt. It writes no binary. It is a development tool and
// never ships.
//
// Usage:
//
//	go run ./tools/releasetree -out <dir> [-src <repository>] [-version <semver>] [-digest <file>] [-modes <file>]
//
// <dir> must not exist or be empty. The version defaults to 0.0.0-dev. The
// output is the same, byte for byte, on every run and every system; the
// last line printed ends with the SHA-256 of the tree, to compare runs, and
// -digest writes it alone to a file once the tree is written. -modes writes
// the git mode of every file, "100755 bin/agent-code-review" for the
// executable launcher and 100644 for the rest, for whoever builds the commit
// on a file system without an executable bit. The
// optional MCP server entry of the hosts is not reachable from the command
// line: it is never part of a release.
// Exit codes: 0 the tree was written, 1 it could not be built or written,
// 2 usage error.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
)

const usage = "usage: releasetree -out <dir> [-src <repository>] [-version <semver>] [-digest <file>] [-modes <file>]\n"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	fl := flag.NewFlagSet("releasetree", flag.ContinueOnError)
	fl.SetOutput(stderr)
	src := fl.String("src", ".", "root of the repository")
	out := fl.String("out", "", "directory to write the tree to; it must not exist or be empty")
	version := fl.String("version", "0.0.0-dev", "SemVer string put in every manifest")
	digest := fl.String("digest", "", "file to write the SHA-256 of the tree to, as one line")
	modes := fl.String("modes", "", "file to write the git mode and path of every file to, one per line")
	if err := fl.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if *out == "" || fl.NArg() > 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	if !validVersion(*version) {
		fmt.Fprintf(stderr, "releasetree: version %q is not a SemVer string\n", *version)
		return 2
	}

	root := os.DirFS(*src)
	allowlist, err := readAllowlist(root)
	if err != nil {
		fmt.Fprintf(stderr, "releasetree: %v\n", err)
		return 1
	}
	files, err := buildTree(options{Src: root, Version: *version, Allowlist: allowlist})
	if err != nil {
		fmt.Fprintf(stderr, "releasetree: %v\n", err)
		return 1
	}
	if err := writeTree(*out, files); err != nil {
		fmt.Fprintf(stderr, "releasetree: %v\n", err)
		return 1
	}
	sum := treeDigest(files)
	if *digest != "" {
		if err := os.WriteFile(*digest, []byte(sum+"\n"), 0o644); err != nil {
			fmt.Fprintf(stderr, "releasetree: %v\n", err)
			return 1
		}
	}
	if *modes != "" {
		if err := os.WriteFile(*modes, []byte(treeModes(files)), 0o644); err != nil {
			fmt.Fprintf(stderr, "releasetree: %v\n", err)
			return 1
		}
	}
	fmt.Fprintf(stdout, "wrote %d files, version %s, to %s, sha256 %s\n", len(files), *version, *out, sum)
	return 0
}
