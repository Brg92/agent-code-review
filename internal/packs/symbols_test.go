package packs

import (
	"reflect"
	"strings"
	"testing"

	"github.com/Brg92/agent-code-review/internal/rules"
	"github.com/Brg92/agent-code-review/internal/symbols"
)

// publicAPI returns the identities of the public declarations of a
// source, with the parameters of functions.
func publicAPI(l Language, src string) []string {
	var out []string
	for _, d := range symbols.Extract(l.Symbols, l.Table, src) {
		if !d.Public {
			continue
		}
		s := d.ID().String()
		if d.Kind == symbols.Function {
			var ps []string
			for _, p := range d.Params {
				ps = append(ps, p.Name)
			}
			s += "(" + strings.Join(ps, ", ") + ")"
		}
		out = append(out, s)
	}
	return out
}

func language(t *testing.T, p *Pack, id string) Language {
	t.Helper()
	for _, l := range p.Languages {
		if l.ID == id {
			return l
		}
	}
	t.Fatalf("pack %s has no %s", p.ID, id)
	return Language{}
}

func TestAndroidPackSymbols(t *testing.T) {
	p := loadAndroid(t)
	for _, l := range p.Languages {
		if has := l.Symbols != nil; has != (l.ID == "kotlin" || l.ID == "java") {
			t.Errorf("%s: symbols %v", l.ID, has)
		}
	}
	kotlin := `package com.example.core

class Session internal constructor(private val store: Store) {
    fun token(scope: String = "all"): String? = null
    internal fun reset() {}
    companion object { fun create(): Session = Session(Store()) }
}

private fun helper() {}
fun String.masked(): String = "***"
`
	if got, want := publicAPI(language(t, p, "kotlin"), kotlin), []string{
		"com.example.core.Session", "com.example.core.Session.token(scope)", "com.example.core.Session.object.create()",
		"com.example.core.masked()",
	}; !reflect.DeepEqual(got, want) {
		t.Errorf("kotlin API = %q, want %q", got, want)
	}
	java := `package com.example.core;

public class Session {
    public static final String VERSION = "1";
    public String token(String scope) { return null; }
    void reset() {}
    public interface Listener { void onChange(Session s); }
}
`
	if got, want := publicAPI(language(t, p, "java"), java), []string{
		"com.example.core.Session", "com.example.core.Session.VERSION", "com.example.core.Session.token(scope)",
		"com.example.core.Session.Listener", "com.example.core.Session.Listener.onChange(s)",
	}; !reflect.DeepEqual(got, want) {
		t.Errorf("java API = %q, want %q", got, want)
	}
}

func TestKMPSymbolsAreTheAndroidOnes(t *testing.T) {
	if a, k := language(t, loadAndroid(t), "kotlin").Symbols, language(t, loadKMP(t), "kotlin").Symbols; k == nil || !reflect.DeepEqual(a, k) {
		t.Errorf("kotlin symbols differ: android %+v, kotlin-multiplatform %+v", a, k)
	}
}

// search finds the lines of a fixed set of files that contain a term.
type search map[string]string

func (s search) Search(project, ref, term string) ([]rules.Match, error) {
	var out []rules.Match
	for path, src := range s {
		for i, line := range strings.Split(src, "\n") {
			if strings.Contains(line, term) {
				out = append(out, rules.Match{Path: path, Line: i + 1})
			}
		}
	}
	return out, nil
}
