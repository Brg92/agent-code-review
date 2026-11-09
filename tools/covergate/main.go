// Command covergate checks statement coverage per package against a
// threshold. It reads a cover profile written by go test -coverprofile,
// prints one row per gated package and exits with 1 when any of them is
// below the threshold.
//
// Usage:
//
//	go test -coverprofile=coverage.out ./...
//	go run ./tools/covergate [-min 90] [-include internal] \
//	    [-exclude internal/testutil,internal/guards] [-gomod go.mod] coverage.out
//
// -include and -exclude take comma-separated directories relative to the
// module root; each one also covers its subdirectories. A package is gated
// when it is under an included directory and not under an excluded one.
// Exit codes: 0 every gated package passes, 1 at least one fails, 2 usage
// or input error.
package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
)

const usage = "usage: covergate [-min percent] [-include dirs] [-exclude dirs] [-gomod file] <cover profile>\n"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("covergate", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprint(stderr, usage)
		fs.PrintDefaults()
	}
	min := fs.Float64("min", 90, "minimum statement coverage per package, in percent")
	include := fs.String("include", "internal", "comma-separated directories whose packages are gated")
	exclude := fs.String("exclude", "internal/testutil,internal/guards", "comma-separated directories whose packages are not gated")
	gomod := fs.String("gomod", "go.mod", "go.mod file that names the module")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return 2
	}
	if *min < 0 || *min > 100 {
		fmt.Fprintf(stderr, "covergate: -min must be between 0 and 100, got %v\n", *min)
		return 2
	}

	mod, err := readModulePath(*gomod)
	if err != nil {
		fmt.Fprintf(stderr, "covergate: %v\n", err)
		return 2
	}
	pkgs, err := readProfile(fs.Arg(0))
	if err != nil {
		fmt.Fprintf(stderr, "covergate: %v\n", err)
		return 2
	}
	gated := selectPackages(pkgs, mod, splitList(*include), splitList(*exclude))
	if len(gated) == 0 {
		fmt.Fprintf(stderr, "covergate: no package of %s matches -include %q and -exclude %q\n", fs.Arg(0), *include, *exclude)
		return 2
	}

	var failed []string
	tw := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "package\tstatements\tcovered\tcoverage\tstatus")
	for _, rel := range gated {
		c := pkgs[mod+"/"+rel]
		status := "ok"
		if !c.passes(*min) {
			status = "FAIL"
			failed = append(failed, rel)
		}
		fmt.Fprintf(tw, "%s\t%d\t%d\t%s\t%s\n", rel, c.statements, c.covered, c.percent(), status)
	}
	if err := tw.Flush(); err != nil {
		fmt.Fprintf(stderr, "covergate: %v\n", err)
		return 2
	}
	if len(failed) > 0 {
		fmt.Fprintf(stderr, "covergate: %d package(s) below %.1f%%: %s\n", len(failed), *min, strings.Join(failed, ", "))
		return 1
	}
	return 0
}

// counts holds the statements of a package and how many of them ran.
type counts struct {
	statements int
	covered    int
}

// passes reports whether the coverage is at least min percent. A package
// without statements always passes.
func (c counts) passes(min float64) bool {
	return float64(c.covered)*100 >= min*float64(c.statements)
}

// percent formats the coverage with one decimal, truncated so that a
// failing package never shows the threshold itself.
func (c counts) percent() string {
	if c.statements == 0 {
		return "-"
	}
	tenths := c.covered * 1000 / c.statements
	return fmt.Sprintf("%d.%d%%", tenths/10, tenths%10)
}

func readProfile(name string) (map[string]counts, error) {
	f, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	pkgs, err := parseProfile(f)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	return pkgs, nil
}

// parseProfile sums the statements of a cover profile per package import
// path. A block listed more than once, as in concatenated profiles, counts
// once and is covered when any of its entries ran.
func parseProfile(r io.Reader) (map[string]counts, error) {
	type block struct {
		pkg        string
		statements int
		covered    bool
	}
	blocks := map[string]*block{}
	var order []string

	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	n := 0
	sawMode := false
	for sc.Scan() {
		n++
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		if mode, ok := strings.CutPrefix(line, "mode: "); ok {
			switch mode {
			case "set", "count", "atomic":
			default:
				return nil, fmt.Errorf("line %d: unknown mode %q", n, mode)
			}
			sawMode = true
			continue
		}
		if !sawMode {
			return nil, errors.New("missing mode line")
		}
		key, pkg, stmts, count, err := parseBlock(line)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", n, err)
		}
		b, ok := blocks[key]
		if !ok {
			b = &block{pkg: pkg, statements: stmts}
			blocks[key] = b
			order = append(order, key)
		}
		b.covered = b.covered || count > 0
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if !sawMode {
		return nil, errors.New("missing mode line")
	}

	pkgs := map[string]counts{}
	for _, key := range order {
		b := blocks[key]
		c := pkgs[b.pkg]
		c.statements += b.statements
		if b.covered {
			c.covered += b.statements
		}
		pkgs[b.pkg] = c
	}
	return pkgs, nil
}

// parseBlock parses "file:start.col,end.col statements count".
func parseBlock(line string) (key, pkg string, statements, count int, err error) {
	fields := strings.Fields(line)
	if len(fields) != 3 {
		return "", "", 0, 0, fmt.Errorf("want 3 fields, got %d", len(fields))
	}
	i := strings.LastIndexByte(fields[0], ':')
	if i < 0 {
		return "", "", 0, 0, errors.New("no file:range")
	}
	file, rng := fields[0][:i], fields[0][i+1:]
	if !validRange(rng) {
		return "", "", 0, 0, fmt.Errorf("bad range %q", rng)
	}
	pkg = path.Dir(file)
	if pkg == "." {
		return "", "", 0, 0, fmt.Errorf("file %q has no package directory", file)
	}
	statements, err1 := strconv.Atoi(fields[1])
	count, err2 := strconv.Atoi(fields[2])
	if err1 != nil || err2 != nil || statements < 0 || count < 0 {
		return "", "", 0, 0, fmt.Errorf("bad counts %q %q", fields[1], fields[2])
	}
	return fields[0], pkg, statements, count, nil
}

// validRange reports whether s has the form line.col,line.col.
func validRange(s string) bool {
	start, end, ok := strings.Cut(s, ",")
	return ok && validPos(start) && validPos(end)
}

func validPos(s string) bool {
	l, c, ok := strings.Cut(s, ".")
	return ok && isDigits(l) && isDigits(c)
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func readModulePath(name string) (string, error) {
	f, err := os.Open(name)
	if err != nil {
		return "", err
	}
	defer f.Close()
	mod, err := modulePath(f)
	if err != nil {
		return "", fmt.Errorf("%s: %w", name, err)
	}
	return mod, nil
}

// modulePath returns the module path declared in a go.mod file.
func modulePath(r io.Reader) (string, error) {
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if i := strings.Index(line, "//"); i >= 0 {
			line = strings.TrimSpace(line[:i])
		}
		if rest, ok := strings.CutPrefix(line, "module "); ok {
			if mod := strings.Trim(strings.TrimSpace(rest), `"`); mod != "" {
				return mod, nil
			}
		}
	}
	if err := sc.Err(); err != nil {
		return "", err
	}
	return "", errors.New("no module line")
}

// selectPackages returns, sorted, the paths relative to mod of the packages
// under an included directory and not under an excluded one.
func selectPackages(pkgs map[string]counts, mod string, include, exclude []string) []string {
	var out []string
	for pkg := range pkgs {
		rel, ok := strings.CutPrefix(pkg, mod+"/")
		if !ok {
			continue
		}
		if under(rel, include) && !under(rel, exclude) {
			out = append(out, rel)
		}
	}
	sort.Strings(out)
	return out
}

// under reports whether rel is one of dirs or inside one of them.
func under(rel string, dirs []string) bool {
	for _, d := range dirs {
		if rel == d || strings.HasPrefix(rel, d+"/") {
			return true
		}
	}
	return false
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.Trim(strings.TrimSpace(p), "/"); p != "" {
			out = append(out, p)
		}
	}
	return out
}
