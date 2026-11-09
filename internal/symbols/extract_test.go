package symbols

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Brg92/agent-code-review/internal/lexer"
)

// table loads a lexer table of the android pack.
func table(t testing.TB, language string) *lexer.Table {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "packs", "android", "lexer", language+".json"))
	if err != nil {
		t.Fatal(err)
	}
	tab, err := lexer.LoadTable(data)
	if err != nil {
		t.Fatal(err)
	}
	return tab
}

// describe writes a declaration as kind, identity, parameters (a default
// marked with =), position and visibility.
func describe(d Decl) string {
	s := string(d.Kind) + " " + d.ID().String()
	if d.Kind == Function {
		var ps []string
		for _, p := range d.Params {
			if p.Default {
				ps = append(ps, p.Name+"=")
			} else {
				ps = append(ps, p.Name)
			}
		}
		s += "(" + strings.Join(ps, ", ") + ")"
	}
	s += fmt.Sprintf(" %d:%d", d.Line, d.Column)
	if !d.Public {
		s += " hidden"
	}
	return s
}

func extract(t *testing.T, spec, language, src string) []string {
	t.Helper()
	var out []string
	for _, d := range Extract(mustSpec(t, spec), table(t, language), src) {
		out = append(out, describe(d))
	}
	return out
}

func check(t *testing.T, got, want []string) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("declarations:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestExtractKotlinDeclarations(t *testing.T) {
	src := `package com.example.core

import kotlinx.coroutines.Dispatchers

/** Formats a value. */
fun topLevel(a: Int): String = "x"

val version: String = "1"

private fun hidden() {}

internal class InternalThing {
    fun inner() {}
}

class Outer internal constructor(private val store: Store) {
    private val cache = mutableMapOf<String, Int>()

    fun visible(name: String, retries: Int = 3): Int {
        val local = 1
        fun localFun() {}
        return local
    }

    protected open fun forSubclasses() {}

    class Nested {
        fun deep() {}
    }

    companion object {
        val instance: Outer by lazy { Outer(Store()) }
        fun create(): Outer = Outer(Store())
    }
}
`
	check(t, extract(t, kotlinSpec, "kotlin", src), []string{
		"function com.example.core.topLevel(a) 6:5",
		"property com.example.core.version 8:5",
		"function com.example.core.hidden() 10:13 hidden",
		"type com.example.core.InternalThing 12:16 hidden",
		"function com.example.core.InternalThing.inner() 13:9 hidden",
		"type com.example.core.Outer 16:7",
		"property com.example.core.Outer.store 16:46 hidden",
		"property com.example.core.Outer.cache 17:17 hidden",
		"function com.example.core.Outer.visible(name, retries=) 19:9",
		"function com.example.core.Outer.forSubclasses() 25:24",
		"type com.example.core.Outer.Nested 27:11",
		"function com.example.core.Outer.Nested.deep() 28:13",
		"property com.example.core.Outer.object.instance 32:13",
		"function com.example.core.Outer.object.create() 33:13",
	})
}

func TestExtractKotlinSignatures(t *testing.T) {
	src := `package ext

fun String.shout(): String = uppercase()
fun <T> List<T>.second(): T = this[1]
fun String?.orEmptyText(): String = this ?: ""
val String.size2: Int get() = length

fun request(
    url: String,
    headers: Map<String, List<String>> = emptyMap(),
    onDone: (Result<String>, Int) -> Unit,
    log: (String, Int) -> Unit = { message, level -> println("$message $level") },
): Call {
    TODO()
}

@Deprecated("Use request", ReplaceWith("request(url)"))
fun legacy(@Suppress("x") vararg urls: String) = request(urls[0]) {}

fun interface Callback { fun call(value: Int) }

expect fun platformName(): String
public actual class Clock actual constructor() { actual fun now(): Long = 0 }
`
	check(t, extract(t, kotlinSpec, "kotlin", src), []string{
		"function ext.shout() 3:12",
		"function ext.second() 4:17",
		"function ext.orEmptyText() 5:13",
		"property ext.size2 6:12",
		"function ext.request(url, headers=, onDone, log=) 8:5",
		"function ext.legacy(urls) 18:5",
		"type ext.Callback 20:15",
		"function ext.Callback.call(value) 20:30",
		"function ext.platformName() 22:12",
		"type ext.Clock 23:21",
		"function ext.Clock.now() 23:61",
	})
	decls := Extract(mustSpec(t, kotlinSpec), table(t, "kotlin"), src)
	if d := decls[4]; d.StartLine != 8 || d.EndLine != 13 {
		t.Errorf("request spans lines %d-%d, want 8-13", d.StartLine, d.EndLine)
	}
	if d := decls[5]; d.StartLine != 17 || d.EndLine != 18 {
		t.Errorf("legacy spans lines %d-%d, want 17-18 with its annotation", d.StartLine, d.EndLine)
	}
	if d := decls[8]; !reflect.DeepEqual(d.Modifiers, []string{"expect"}) {
		t.Errorf("platformName modifiers %q", d.Modifiers)
	}
	if d := decls[9]; !reflect.DeepEqual(d.Modifiers, []string{"public", "actual"}) {
		t.Errorf("Clock modifiers %q", d.Modifiers)
	}
}

func TestExtractJavaDeclarations(t *testing.T) {
	src := `package com.example.legacy;

import java.util.List;

public class Client {
    public static final String VERSION = "1";
    private int count;
    String packagePrivate;

    public Client(String url) {}

    public <T> List<T> fetch(Class<T> type, int limit) { return null; }

    protected void onClose() {}

    void hiddenMethod() {}

    public interface Listener {
        void onEvent(@Deprecated String name);
    }

    static class Helper {
        public void help() {}
    }

    public Map<String, List<Integer>> nested(Map<String, List<Integer>> in, String... rest) {
        return in;
    }

    static {
        System.out.println("loaded");
    }
}

class PackagePrivate {
    public void x() {}
}
`
	check(t, extract(t, javaSpec, "java", src), []string{
		"type com.example.legacy.Client 5:14",
		"property com.example.legacy.Client.VERSION 6:32",
		"property com.example.legacy.Client.count 7:17 hidden",
		"property com.example.legacy.Client.packagePrivate 8:12 hidden",
		"function com.example.legacy.Client.Client(url) 10:12",
		"function com.example.legacy.Client.fetch(type, limit) 12:24",
		"function com.example.legacy.Client.onClose() 14:20",
		"function com.example.legacy.Client.hiddenMethod() 16:10 hidden",
		"type com.example.legacy.Client.Listener 18:22",
		"function com.example.legacy.Client.Listener.onEvent(name) 19:14",
		"type com.example.legacy.Client.Helper 22:18 hidden",
		"function com.example.legacy.Client.Helper.help() 23:21 hidden",
		"function com.example.legacy.Client.nested(in, rest) 26:39",
		"type com.example.legacy.PackagePrivate 35:7 hidden",
		"function com.example.legacy.PackagePrivate.x() 36:17 hidden",
	})
}

func TestExtractWithoutPackageOrCode(t *testing.T) {
	check(t, extract(t, kotlinSpec, "kotlin", "fun main() {}\n"), []string{"function main() 1:5"})
	check(t, extract(t, kotlinSpec, "kotlin", ""), nil)
	check(t, extract(t, kotlinSpec, "kotlin", "// fun commented() {}\n/* class Gone */\n"), nil)
	// Unterminated code yields what can be read.
	check(t, extract(t, kotlinSpec, "kotlin", "class A {\n    fun b(x: Int"), []string{"type A 1:7", "function A.b(x) 2:9"})
}

func TestExtractKotlinConstructorProperties(t *testing.T) {
	src := `package p

data class User(val id: String, var name: String = "", private val secret: String, internal val tag: Int, age: Int)

class Session internal constructor(
    @Json(name = "t") val token: String,
    override val expiresAt: Long,
    retries: Int = 3,
) : Base(retries) {
    fun refresh() {}
}

private class Hidden(val x: Int)

class Plain
fun after(y: Int) {}
`
	check(t, extract(t, kotlinSpec, "kotlin", src), []string{
		"type p.User 3:12",
		"property p.User.id 3:21",
		"property p.User.name 3:37",
		"property p.User.secret 3:68 hidden",
		"property p.User.tag 3:97 hidden",
		"type p.Session 5:7",
		"property p.Session.token 6:27",
		"property p.Session.expiresAt 7:18",
		"function p.Session.refresh() 10:9",
		"type p.Hidden 13:15 hidden",
		"property p.Hidden.x 13:26 hidden",
		"type p.Plain 15:7",
		"function p.after(y) 16:5",
	})
	// Java record components are not properties.
	check(t, extract(t, javaSpec, "java", "package p;\n\npublic record Point(int x, int y) {}\n"), []string{"type p.Point 3:15"})
}
