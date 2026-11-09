package symbols

import "testing"

// FuzzExtract checks that any source, however malformed, yields
// declarations without failing, each with a position in the source.
func FuzzExtract(f *testing.F) {
	for _, seed := range []string{
		"package a.b\nclass A<T>(val x: Int) : B() {\n fun <R> T.f(a: Map<String, List<R>> = mapOf(), b: () -> Unit = {}): R\n}\n",
		"public class A { public <T> List<T> f(Map<String, List<Integer>> m, int... r) { return null; } int a, b; }",
		"@A(x = 1) @B.C fun", "fun (", "class <", "fun f(a: Int = (", ">>>>", "@", "} } {", "package", "fun String?.",
	} {
		f.Add(seed)
	}
	kotlin, java := mustSpec(f, kotlinSpec), mustSpec(f, javaSpec)
	kt, jt := table(f, "kotlin"), table(f, "java")
	f.Fuzz(func(t *testing.T, src string) {
		for _, d := range append(Extract(kotlin, kt, src), Extract(java, jt, src)...) {
			if d.Name == "" || d.Line < 1 || d.Column < 1 || d.StartLine > d.Line || d.EndLine < d.Line {
				t.Fatalf("declaration %+v", d)
			}
		}
	})
}
