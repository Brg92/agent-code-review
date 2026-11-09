package packs

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/Brg92/agent-code-review/internal/rules"
)

// ruleCase runs one rule of the android catalog on a synthetic source and
// lists the lines it signals.
type ruleCase struct {
	name, rule, path, src string
	added                 []int
	want                  string
}

const (
	kt     = "app/src/main/java/a/A.kt"
	kts    = "app/build.gradle.kts"
	groovy = "app/build.gradle"
	toml   = "gradle/libs.versions.toml"
	java   = "app/src/main/java/a/A.java"
)

var androidRuleCases = []ruleCase{
	{"strict shorthand and block in a kotlin script", "strict-version-pin", kts,
		"implementation(\"a:b:1.0!!\")\nversion { strictly(\"1.0\") }\n// \"a:b:1.0!!\"\nimplementation(\"a:b:1.0\")\n", nil, "1 2"},
	{"strict pins in a groovy script", "strict-version-pin", groovy,
		"implementation 'a:b:1.0!!'\nversion { strictly('1.0') }\nversion { strictly '1.0' }\n// strictly '1.0'\nimplementation 'a:b:1.0'\n", nil, "1 2 3"},
	{"strict pins in a version catalog", "strict-version-pin", toml,
		"lib = { module = \"a:b\", version = { strictly = \"1.0\" } }\nother = \"a:b:1.0!!\"\n# strictly = \"1.0\"\nok = { module = \"a:b\", version = \"1.0\" }\n", nil, "1 2"},
	{"strict pin outside build files", "strict-version-pin", kt, "f(\"a:b:1.0!!\")\n", nil, ""},
	{"strict pin in a properties file", "strict-version-pin", "gradle.properties", "lib=1.0!!\n", nil, ""},

	{"LiveData in kotlin", "new-live-data", kt,
		"val a = MutableLiveData<Int>()\nval b: LiveData<Int> = a\nval c = MediatorLiveData<X>()\nval d = liveDataOf()\n", nil, "1 2 3"},
	{"LiveData in java", "new-live-data", java, "LiveData<String> x;\nString liveData;\n", nil, "1"},

	{"broadcasts in kotlin", "local-broadcast-manager", kt,
		"LocalBroadcastManager.getInstance(c)\nclass R : BroadcastReceiver() {}\nregisterReceiver(r, f)\nval r: BroadcastReceiver = x\nunregisterReceiver(r)\n", nil, "1 2 3"},
	{"broadcasts in java", "local-broadcast-manager", java,
		"LocalBroadcastManager.getInstance(c);\nclass R extends BroadcastReceiver {}\nregisterReceiver(r, f);\nBroadcastReceiver r;\n", nil, "1 2 3"},

	{"findViewById in kotlin", "find-view-by-id", kt, "findViewById<TextView>(R.id.a)\nfindViewById(R.id.b)\nval findViewById = 1\n", nil, "1 2"},
	{"findViewById in java", "find-view-by-id", java, "View v = findViewById(R.id.a);\nView r = binding.getRoot();\n", nil, "1"},

	{"GlobalScope in kotlin", "global-scope", kt,
		"import kotlinx.coroutines.GlobalScope\nGlobalScope.launch {}\nval s = \"GlobalScope\"\n// GlobalScope\nviewModelScope.launch {}\n", nil, "2"},
	{"GlobalScope in java", "global-scope", java, "GlobalScope.INSTANCE.launch(c);\nscope.launch(c);\n", nil, "1"},

	{"launchWhen in kotlin", "launch-when-started", kt,
		"scope.launchWhenStarted {}\nscope.launchWhenResumed {}\nscope.launchWhenCreated {}\nscope.launchWhenIdle {}\nscope.launch {}\n", nil, "1 2 3"},
	{"launchWhen in java", "launch-when-started", java, "scope.launchWhenStarted(b);\nscope.launch(b);\n", nil, "1"},

	{"AsyncTask in kotlin", "async-task", kt, "class T : AsyncTask<A, B, C>()\nclass U : Task()\n", nil, "1"},
	{"AsyncTask in java", "async-task", java, "class T extends AsyncTask<A, B, C> {}\nclass U extends Thread {}\n", nil, "1"},

	{"lateinit binding in kotlin", "lateinit-binding", kt,
		"lateinit var binding: FooBinding\nlateinit var viewBinding: X\nlateinit var name: String\nprivate var _binding: FooBinding? = null\n", nil, "1 2"},
	{"lateinit binding in java", "lateinit-binding", java, "private FooBinding binding;\n", nil, ""},

	{"full refresh in kotlin", "full-list-refresh", kt, "adapter.notifyDataSetChanged()\nadapter.notifyItemChanged(1)\n", nil, "1"},
	{"full refresh in java", "full-list-refresh", java, "adapter.notifyDataSetChanged();\nadapter.notifyItemChanged(1);\n", nil, "1"},

	{"runBlocking in kotlin", "run-blocking-prod", kt, "fun f() = runBlocking { g() }\nfun h() = runBlockingTest { g() }\n", nil, "1"},
	{"runBlocking in tests", "run-blocking-prod", "app/src/test/java/a/ATest.kt", "fun f() = runBlocking { g() }\n", nil, ""},
	{"runBlocking in instrumented tests", "run-blocking-prod", "app/src/androidTest/java/a/ATest.kt", "fun f() = runBlocking { g() }\n", nil, ""},
	{"runBlocking in java", "run-blocking-prod", java, "BuildersKt.runBlocking(c, f);\nrun(c);\n", nil, "1"},

	{"generic catch in kotlin", "generic-exception", kt,
		"try {} catch (e: Exception) {}\ntry {} catch (t: Throwable) {}\ntry {} catch (e: IOException) {}\ntry {} catch (e: kotlin.Exception) {}\n", nil, "1 2 4"},
	{"generic catch in java", "generic-exception", java,
		"try {} catch (Exception e) {}\ntry {} catch (final Throwable t) {}\ntry {} catch (IOException e) {}\ntry {} catch (java.lang.Exception e) {}\n" +
			"try {} catch (IOException | Exception e) {}\ntry {} catch (IOException | IllegalStateException e) {}\n", nil, "1 2 4 5"},

	{"hard-coded dispatcher in kotlin", "hardcoded-dispatcher", kt,
		"withContext(Dispatchers.IO) {}\nval m = Dispatchers.Main.immediate\nval d = dispatchers.io\nwithContext(dispatchers.io) {}\n", nil, "1 2"},
	{"hard-coded dispatcher in java", "hardcoded-dispatcher", java, "var d = Dispatchers.Default;\nvar e = dispatchers.io;\n", nil, "1"},

	{"not-null assertion", "no-double-bang", kt,
		"val a = b!!\nprivate val binding get() = _binding!!\nval s = \"text!!\"\nval c = d != null\n", nil, "1"},
	{"not-null assertion in a build script", "no-double-bang", kts, "val a = b!!\n", nil, ""},
	{"not-null assertion has no java form", "no-double-bang", java, "boolean a = b != null;\n", nil, ""},

	{"empty catch in kotlin", "empty-catch", kt,
		"try {} catch (e: E) {}\ntry {} catch (e: E) { /* ignored */ }\ntry {\n} catch (e: E) {\n}\ntry {} catch (e: E) { log(e) }\n", nil, "1 2 4"},
	{"empty catch in java", "empty-catch", java, "try {} catch (E e) { // nothing\n}\ntry {} catch (E e) { log(e); }\n", nil, "1"},

	{"long added lines in kotlin", "line-too-long", kt, long + long, []int{2}, "2"},
	{"long line in java", "line-too-long", java, strings.Repeat("x", 101) + "\n" + strings.Repeat("x", 100) + "\n", []int{1, 2}, "1"},

	{"field injection in kotlin", "field-injection", kt,
		"class A {\n    @Inject lateinit var a: A\n    @Inject\n    @Named(\"x\")\n    lateinit var b: B\n}\nclass C @Inject constructor(private var d: D)\n", nil, "2 5"},
	{"field injection in a Hilt entry point", "field-injection", kt, "@AndroidEntryPoint\nclass F : Fragment() {\n    @Inject lateinit var a: A\n}\n", nil, ""},
	{"field injection in java", "field-injection", java,
		"class A {\n  @Inject Foo foo;\n  @Inject @Named(\"x\") Provider<Foo> bar;\n  @Inject void set(Foo f) {}\n  @Inject public A(Foo f) {}\n}\n", nil, "2 3"},

	{"missing KDoc on added lines", "missing-kdoc", kt, "fun a() {}\n/** Doc. */\nfun b() {}\nfun c() {}\nprivate fun d() {}\n", []int{3, 4, 5}, "4"},
	{"KDoc is not checked in java", "missing-kdoc", java, "public void a() {}\n", []int{1}, ""},
}

var long = "val s = \"" + strings.Repeat("x", 100) + "\"\n"

func runAndroidRule(t *testing.T, set *Set, c ruleCase) (lines string, sigs []rules.Signal) {
	t.Helper()
	p := set.Packs[0]
	a := set.Assign(set.Packs, c.path)
	if a.NotVerified != nil {
		t.Fatalf("%s is not verified: %+v", c.path, a.NotVerified)
	}
	added := map[int]bool{}
	for _, l := range c.added {
		added[l] = true
	}
	res := rules.Run(p.Rules.Rules, a.File(c.src, added), rules.Options{})
	if len(res.NotVerified) != 0 {
		t.Fatalf("not verified: %+v", res.NotVerified)
	}
	var got []string
	for _, s := range res.Signals {
		if s.RuleID == c.rule {
			got = append(got, fmt.Sprint(s.Line))
			sigs = append(sigs, s)
		}
	}
	return strings.Join(got, " "), sigs
}

func TestAndroidMechanicalRules(t *testing.T) {
	set, err := Load(packsDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range androidRuleCases {
		t.Run(c.name, func(t *testing.T) {
			if got, _ := runAndroidRule(t, set, c); got != c.want {
				t.Errorf("%s signals lines %q, want %q", c.rule, got, c.want)
			}
		})
	}
}

// TestAndroidMechanicalRulesAreCovered checks that every rule with a
// match on one file has a case that signals and a case that does not, and
// a java case when it applies to java.
func TestAndroidMechanicalRulesAreCovered(t *testing.T) {
	p := loadAndroid(t)
	positive, negative, javaCase := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, c := range androidRuleCases {
		wanted := len(strings.Fields(c.want))
		if wanted > 0 {
			positive[c.rule] = true
		}
		// A source with lines the rule leaves alone is a negative case too.
		if wanted == 0 || strings.Count(c.src, "\n") > wanted {
			negative[c.rule] = true
		}
		if strings.HasSuffix(c.path, ".java") {
			javaCase[c.rule] = true
		}
	}
	matched := 0
	for _, r := range p.Rules.Rules {
		if !r.Matched() || r.OnChangeRequest() {
			continue
		}
		matched++
		if !positive[r.ID] || !negative[r.ID] {
			t.Errorf("%s: needs a case that signals and a case that does not", r.ID)
		}
		for _, l := range r.Languages {
			if l == "java" && !javaCase[r.ID] {
				t.Errorf("%s: applies to java but has no java case", r.ID)
			}
		}
	}
	if matched != 17 {
		t.Errorf("%d rules have a match on one file, want 17", matched)
	}
}

func TestAndroidJudgedSignalsCarryNoSeverity(t *testing.T) {
	set, err := Load(packsDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, rule := range []string{"no-double-bang", "missing-kdoc"} {
		var c ruleCase
		for _, rc := range androidRuleCases {
			if rc.rule == rule && rc.want != "" {
				c = rc
				break
			}
		}
		_, sigs := runAndroidRule(t, set, c)
		if len(sigs) == 0 || sigs[0].Severity != "" || !sigs[0].NeedsJudgement {
			t.Errorf("%s: signals %+v, want ones that need judgement and carry no severity", rule, sigs)
		}
	}
}

// TestAndroidLineLengthIsOverridable checks that a repository may set the
// line length between 60 and 200, the bounds of the legacy lineLength,
// and that the text of the rule follows the value in use.
func TestAndroidLineLengthIsOverridable(t *testing.T) {
	var rule *rules.Rule
	for _, r := range loadAndroid(t).Rules.Rules {
		if r.ID == "line-too-long" {
			rule = r
		}
	}
	if rule == nil {
		t.Fatal("no line-too-long rule")
	}
	if want := []rules.Option{{Name: "max", Min: 60, Max: 200, Default: 100}}; !reflect.DeepEqual(rule.Options(), want) {
		t.Errorf("options = %+v, want %+v", rule.Options(), want)
	}
	wider, err := rule.WithOptions(map[string]int{"max": 120})
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{wider.Description, wider.Why, wider.Fix} {
		if !strings.Contains(text, "120") || strings.Contains(text, "100") {
			t.Errorf("text with max 120: %q", text)
		}
	}
}
