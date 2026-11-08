package rules

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

// TestLegacySignals runs the legacy signals, as expressed in the example
// catalog, on synthetic sources. Each case lists the lines the rule
// signals. Cases named "improvement" are matched here but were missed by
// the legacy script.
func TestLegacySignals(t *testing.T) {
	c, err := LoadCatalog(readFile(t, filepath.Join(contractDir, "valid", "legacy-signals.json")))
	if err != nil {
		t.Fatal(err)
	}
	const kt, java, kts = "app/src/main/A.kt", "app/src/main/A.java", "app/build.gradle.kts"
	long := "val s = \"" + strings.Repeat("x", 100) + "\"\n"
	tests := []struct {
		name, rule, path, src string
		added                 []int
		want                  string
	}{
		{"strict pin in a build script", "strict-version-pin", kts,
			"implementation(\"a:b:1.0!!\")\nversion { strictly(\"1.0\") }\n// \"a:b:1.0!!\"\nimplementation(\"a:b:1.0\")\n", nil, "1 2"},
		{"strict pin outside build files", "strict-version-pin", kt, "f(\"a:b:1.0!!\")\n", nil, ""},
		{"LiveData", "new-live-data", kt,
			"val a = MutableLiveData<Int>()\nval b: LiveData<Int> = a\nval c = MediatorLiveData<X>()\nval d = liveDataOf()\n", nil, "1 2 3"},
		{"LiveData in java", "new-live-data", java, "LiveData<String> x;\n", nil, "1"},
		{"broadcasts", "local-broadcast-manager", kt,
			"LocalBroadcastManager.getInstance(c)\nclass R : BroadcastReceiver() {}\nregisterReceiver(r, f)\nval r: BroadcastReceiver = x\n", nil, "1 2 3"},
		{"improvement: java receiver subclass", "local-broadcast-manager", java, "class R extends BroadcastReceiver {}\n", nil, "1"},
		{"findViewById", "find-view-by-id", kt, "findViewById<TextView>(R.id.a)\nfindViewById(R.id.b)\nval findViewById = 1\n", nil, "1 2"},
		{"GlobalScope", "global-scope", kt,
			"import kotlinx.coroutines.GlobalScope\nGlobalScope.launch {}\nval s = \"GlobalScope\"\n// GlobalScope\n", nil, "2"},
		{"launchWhen", "launch-when-started", kt,
			"scope.launchWhenStarted {}\nscope.launchWhenResumed {}\nscope.launchWhenCreated {}\nscope.launchWhenIdle {}\n", nil, "1 2 3"},
		{"AsyncTask", "async-task", kt, "class T : AsyncTask<A, B, C>()\n", nil, "1"},
		{"lateinit binding", "lateinit-binding", kt,
			"lateinit var binding: FooBinding\nlateinit var viewBinding: X\nlateinit var name: String\n", nil, "1 2"},
		{"full refresh", "full-list-refresh", kt, "adapter.notifyDataSetChanged()\nadapter.notifyItemChanged(1)\n", nil, "1"},
		{"runBlocking", "run-blocking-prod", kt, "fun f() = runBlocking { g() }\n", nil, "1"},
		{"runBlocking in tests", "run-blocking-prod", "app/src/test/java/ATest.kt", "fun f() = runBlocking { g() }\n", nil, ""},
		{"generic catch", "generic-exception", kt,
			"try {} catch (e: Exception) {}\ntry {} catch (t: Throwable) {}\ntry {} catch (e: IOException) {}\n", nil, "1 2"},
		{"improvement: qualified kotlin exception", "generic-exception", kt, "try {} catch (e: kotlin.Exception) {}\n", nil, "1"},
		{"generic catch in java", "generic-exception", java,
			"try {} catch (Exception e) {}\ntry {} catch (final Throwable t) {}\ntry {} catch (IOException e) {}\n", nil, "1 2"},
		{"improvement: qualified java exception", "generic-exception", java, "try {} catch (java.lang.Exception e) {}\n", nil, "1"},
		{"improvement: java multi-catch", "generic-exception", java,
			"try {} catch (IOException | Exception e) {}\ntry {} catch (final Exception | Error e) {}\ntry {} catch (IOException | IllegalStateException e) {}\n", nil, "1 2"},
		{"hard-coded dispatcher", "hardcoded-dispatcher", kt,
			"withContext(Dispatchers.IO) {}\nval m = Dispatchers.Main.immediate\nval d = dispatchers.io\n", nil, "1 2"},
		{"not-null assertion", "no-double-bang", kt,
			"val a = b!!\nprivate val binding get() = _binding!!\nval s = \"text!!\"\n", nil, "1"},
		{"not-null assertion in a build script", "no-double-bang", kts, "val a = b!!\n", nil, ""},
		{"empty catch", "empty-catch", kt,
			"try {} catch (e: E) {}\ntry {} catch (e: E) { /* ignored */ }\ntry {\n} catch (e: E) {\n}\ntry {} catch (e: E) { log(e) }\n", nil, "1 2 4"},
		{"empty catch in java", "empty-catch", java, "try {} catch (E e) { // nothing\n}\ntry {} catch (E e) { log(e); }\n", nil, "1"},
		{"long added line", "line-too-long", kt, long + long, []int{2}, "2"},
		{"field injection", "field-injection", kt,
			"class A {\n    @Inject lateinit var a: A\n    @Inject\n    @Named(\"x\")\n    lateinit var b: B\n}\nclass C @Inject constructor(private var d: D)\n", nil, "2 5"},
		{"field injection in a Hilt entry point", "field-injection", kt,
			"@AndroidEntryPoint\nclass F : Fragment() {\n    @Inject lateinit var a: A\n}\n", nil, ""},
		{"field injection in java", "field-injection", java,
			"class A {\n  @Inject Foo foo;\n  @Inject @Named(\"x\") Provider<Foo> bar;\n  @Inject void set(Foo f) {}\n  @Inject public A(Foo f) {}\n}\n", nil, "2 3"},
		{"missing KDoc on added lines", "missing-kdoc", kt, "fun a() {}\n/** Doc. */\nfun b() {}\nfun c() {}\n", []int{3, 4}, "4"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := kotlinFile(t, tt.src, tt.added...)
			f.Path = tt.path
			f.Test = strings.Contains(tt.path, "/test/")
			if strings.HasSuffix(tt.path, ".java") {
				f.Language, f.Table = "java", packTable(t, "java")
			}
			res := Run(c.Rules, f, Options{})
			if len(res.NotVerified) != 0 {
				t.Fatalf("not verified: %+v", res.NotVerified)
			}
			var got []string
			for _, s := range res.Signals {
				if s.RuleID == tt.rule {
					got = append(got, fmt.Sprint(s.Line))
				}
			}
			if strings.Join(got, " ") != tt.want {
				t.Errorf("%s on lines %q, want %q", tt.rule, strings.Join(got, " "), tt.want)
			}
		})
	}
}
