package symbols

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// kt is a changed Kotlin file.
func kt(t *testing.T, path, base, head string) File {
	t.Helper()
	return File{Path: path, Spec: mustSpec(t, kotlinSpec), Table: table(t, "kotlin"), Base: base, Head: head}
}

// summary writes a change as kind, identity, where it is and the new
// required parameters.
func summary(c Change) string {
	s := string(c.Kind) + " " + c.ID.String()
	for _, at := range []struct {
		side string
		l    *Located
	}{{"base", c.Base}, {"head", c.Head}} {
		if at.l != nil {
			s += fmt.Sprintf(" %s %s:%d", at.side, at.l.Path, at.l.Line)
		}
	}
	if c.NewRequired != nil {
		s += " required " + strings.Join(c.NewRequired, ",")
	}
	return s
}

func index(files ...File) []string {
	var out []string
	for _, c := range IndexOf(files) {
		out = append(out, summary(c))
	}
	return out
}

func checkIndex(t *testing.T, got, want []string) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("changes:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

const sessionBase = `package com.example.core.session

class SessionManager internal constructor(private val store: SessionStore) {
    private var session: Session? = null

    fun current(): Session? = session

    /** Raw token. */
    @Deprecated("Use an authenticated client instead.")
    fun legacyToken(): String? = session?.token

    fun refresh(force: Boolean) {}

    companion object {
        val instance: SessionManager by lazy { SessionManager(InMemorySessionStore()) }
    }
}
`

func TestIndexOfRemovedAndAdded(t *testing.T) {
	head := strings.Replace(sessionBase, `    /** Raw token. */
    @Deprecated("Use an authenticated client instead.")
    fun legacyToken(): String? = session?.token
`, `    fun expiresAt(): Long? = session?.expiresAtMillis
`, 1)
	head = strings.Replace(head, "session = null", "session = Session.EMPTY", 1)
	checkIndex(t, index(kt(t, "core/SessionManager.kt", sessionBase, head)), []string{
		"added com.example.core.session.SessionManager.expiresAt head core/SessionManager.kt:8",
		"removed com.example.core.session.SessionManager.legacyToken base core/SessionManager.kt:10",
	})
}

func TestIndexOfSignatures(t *testing.T) {
	tests := []struct {
		name, from, to string
		want           []string
	}{
		{"new required parameter", "fun refresh(force: Boolean)", "fun refresh(force: Boolean, reason: String)", []string{
			"signature-changed com.example.core.session.SessionManager.refresh base f.kt:12 head f.kt:12 required reason"}},
		{"new parameter with a default", "fun refresh(force: Boolean)", "fun refresh(force: Boolean, reason: String = \"\")", []string{
			"signature-changed com.example.core.session.SessionManager.refresh base f.kt:12 head f.kt:12"}},
		{"default removed", "fun refresh(force: Boolean)", "fun refresh(force: Boolean = false)", []string{
			"signature-changed com.example.core.session.SessionManager.refresh base f.kt:12 head f.kt:12"}},
		{"renamed parameter", "fun refresh(force: Boolean)", "fun refresh(hard: Boolean)", []string{
			"signature-changed com.example.core.session.SessionManager.refresh base f.kt:12 head f.kt:12 required hard"}},
		{"multi-line signature", "fun refresh(force: Boolean)", "fun refresh(\n        force: Boolean,\n        reason: String,\n    )", []string{
			"signature-changed com.example.core.session.SessionManager.refresh base f.kt:12 head f.kt:12 required reason"}},
		{"body only", "fun current(): Session? = session", "fun current(): Session? = session ?: store.load()", nil},
		{"made internal", "fun current()", "internal fun current()", []string{
			"removed com.example.core.session.SessionManager.current base f.kt:6"}},
		{"private made public", "private var session", "var session", []string{
			"added com.example.core.session.SessionManager.session head f.kt:4"}},
		{"companion member removed", "        val instance: SessionManager by lazy { SessionManager(InMemorySessionStore()) }\n", "", []string{
			"removed com.example.core.session.SessionManager.object.instance base f.kt:15"}},
		{"overload added", "fun refresh(force: Boolean) {}", "fun refresh(force: Boolean) {}\n    fun refresh() {}", []string{
			"signature-changed com.example.core.session.SessionManager.refresh base f.kt:12 head f.kt:12"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !strings.Contains(sessionBase, tt.from) {
				t.Fatalf("no %q in the base", tt.from)
			}
			head := strings.Replace(sessionBase, tt.from, tt.to, 1)
			checkIndex(t, index(kt(t, "f.kt", sessionBase, head)), tt.want)
		})
	}
}

func TestIndexOfOverloads(t *testing.T) {
	base := "package p\n\nfun send(to: String) {}\nfun send(to: String, body: String) {}\n"
	head := "package p\n\nfun send(to: String) {}\nfun send(to: String, body: String, urgent: Boolean) {}\n"
	// With several overloads, the new parameter is not attributed to one
	// of them: the signature changed, and judgement decides.
	checkIndex(t, index(kt(t, "p.kt", base, head)), []string{"signature-changed p.send base p.kt:3 head p.kt:3"})
	checkIndex(t, index(kt(t, "p.kt", base, base)), nil)
	reordered := "package p\n\nfun send(to: String, body: String) {}\nfun send(to: String) {}\n"
	checkIndex(t, index(kt(t, "p.kt", base, reordered)), nil)
}

func TestIndexOfWholeChangeRequest(t *testing.T) {
	moved := "package p\n\nfun helper(a: Int) {}\n"
	rest := "package p\n\nfun other() {}\n"
	// A symbol moved to another file of the same package is not removed.
	checkIndex(t, index(kt(t, "a.kt", moved+"fun other() {}\n", rest), kt(t, "b.kt", "", moved)), nil)
	// A removed file removes its symbols; an added one adds them.
	checkIndex(t, index(kt(t, "a.kt", moved, ""), kt(t, "c.kt", "", "package p\n\nclass Fresh\n")), []string{
		"removed p.helper base a.kt:3",
		"added p.Fresh head c.kt:3",
	})
	// A symbol moved to another package is removed and added.
	checkIndex(t, index(kt(t, "a.kt", moved, strings.Replace(moved, "package p", "package q", 1))), []string{
		"removed p.helper base a.kt:3",
		"added q.helper head a.kt:3",
	})
	// A file whose language has no symbols is not compared.
	none := File{Path: "build.gradle", Base: "fun x() {}", Head: ""}
	checkIndex(t, index(none), nil)
}
