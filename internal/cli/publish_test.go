package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Brg92/agent-code-review/internal/forges/base"
	"github.com/Brg92/agent-code-review/internal/forges/github"
	"github.com/Brg92/agent-code-review/internal/runlog"
	"github.com/Brg92/agent-code-review/internal/state"
	"github.com/Brg92/agent-code-review/internal/testutil/ghserver"
	"github.com/Brg92/agent-code-review/internal/testutil/reviewreplay"
)

const s1Head = "76ae5f13ca1575b65fe1567c471e8a7b51a00af4"

// finalizedS1 is a finalized run of scenario S1 and a synthetic GitHub that
// has its pull request, which the environment publishes to.
func finalizedS1(t *testing.T) (env, *ghserver.Server, string) {
	t.Helper()
	e, _, runDir, judgement := reviewedS1(t)
	copyFile(t, []string{"testdata", "scenarios", "S1", "judgements", "sample-app-2.json"}, judgement)
	if r := runWith(t, e, "finalize"); r.code != exitOK {
		t.Fatalf("finalize: code %d\n%s", r.code, r.stderr)
	}
	srv := ghserver.New(t)
	srv.Token = reviewreplay.Canary
	srv.AddRepo("acr-plugin-sandbox/sample-app", "sandbox-author").AddPR(2, "sandbox-author", s1Head)
	clock := &instantClock{at: clockNow}
	e.newReader = func(host string, tokens github.Resolver, opts github.Options) (base.ForgeProvider, error) {
		opts.Transport.Now, opts.Transport.Sleep = clock.Now, clock.Sleep
		return github.NewLoopback(host, srv.URL(), tokens, opts)
	}
	return e, srv, runDir
}

func TestPublishNeedsToKnowWhatToDo(t *testing.T) {
	e, srv, _ := finalizedS1(t)
	for _, c := range []struct {
		name string
		args []string
		want string
	}{
		{"nothing", []string{"publish"}, "give --plan"},
		{"plan without what", []string{"publish", "--plan"}, "comments, issues or both"},
		{"plan of nothing known", []string{"publish", "--plan", "all"}, "comments, issues or both, not \"all\""},
		{"plan of nothing", []string{"publish", "--plan", ""}, "give --plan"},
		{"both flags", []string{"publish", "--plan", "both", "--apply"}, "not both"},
		{"an argument", []string{"publish", "--apply", "now"}, "takes no argument"},
		{"unknown flag", []string{"publish", "--post"}, "unknown flag"},
		{"a run that is not an id", []string{"publish", "--apply", "--run", "x"}, "not a run id"},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := runWith(t, e, c.args...)
			if r.code != exitUsage || r.stdout != "" || !strings.Contains(r.stderr, c.want) {
				t.Errorf("code %d\nstdout %q\nstderr %s", r.code, r.stdout, r.stderr)
			}
		})
	}
	if len(srv.Requests()) != 0 {
		t.Errorf("a usage error reached the platform: %v", srv.Requests())
	}
}

func TestPublishPlanThenApplyOnS1(t *testing.T) {
	e, srv, runDir := finalizedS1(t)
	plan := runWith(t, e, "publish", "--plan", "comments")
	if plan.code != exitOK || plan.stderr != "" {
		t.Fatalf("plan: code %d\n%s\n%s", plan.code, plan.stderr, plan.stdout)
	}
	for _, want := range []string{"Plan for run " + s1RunID + " (comments). Nothing was written to GitHub.", "acr-plugin-sandbox/sample-app#2",
		"3 inline comments: ", "run-blocking-prod", "global-scope", "logic-defect", "plan.json", "agent-code-review publish --apply"} {
		if !strings.Contains(plan.stdout, want) {
			t.Errorf("the plan prints no %q:\n%s", want, plan.stdout)
		}
	}
	if got := srv.Writes(); len(got) != 0 {
		t.Fatalf("--plan wrote %v", got)
	}
	st, err := state.Load(runDir)
	if err != nil || st.Phase != state.Planned || st.ChangeRequests[0].PlanSHA256 == "" {
		t.Fatalf("state %+v, %v", st, err)
	}
	noCanary(t, "plan", plan.stdout)

	apply := runWith(t, e, "publish", "--apply")
	if apply.code != exitOK || apply.stderr != "" || !strings.Contains(apply.stdout, "Published run "+s1RunID+" to GitHub.") ||
		strings.Count(apply.stdout, "posted  inline comment") != 3 {
		t.Fatalf("apply: code %d\n%s\n%s", apply.code, apply.stderr, apply.stdout)
	}
	if got := srv.Writes(); len(got) != 1 || !strings.HasSuffix(got[0].Path, "/pulls/2/reviews") || !strings.Contains(got[0].Body, `"event":"COMMENT"`) {
		t.Errorf("writes = %v", got)
	}
	if st, _ := state.Load(runDir); st.Phase != state.Published {
		t.Errorf("phase %s", st.Phase)
	}
	// A published run is not applied again, nor planned again.
	for _, args := range [][]string{{"publish", "--apply", "--run", s1RunID}, {"publish", "--plan", "both", "--run", s1RunID}} {
		if r := runWith(t, e, args...); r.code != exitState || r.stdout != "" || !strings.Contains(r.stderr, "already published") {
			t.Errorf("%v: code %d\n%s", args, r.code, r.stderr)
		}
	}
	if got := len(srv.Writes()); got != 1 {
		t.Errorf("%d writes", got)
	}
	noCanary(t, "apply", apply.stdout)
	log, err := os.ReadFile(filepath.Join(runDir, runlog.FileName))
	if err != nil {
		t.Fatal(err)
	}
	noCanary(t, "run.log", string(log))
	for _, want := range []string{"\tpublish\t"} {
		if !strings.Contains(string(log), want) {
			t.Errorf("run.log has no %q line:\n%s", want, log)
		}
	}
}

func TestPublishApplyWithoutAPlanIsAnInvalidState(t *testing.T) {
	e, srv, _ := finalizedS1(t)
	r := runWith(t, e, "publish", "--apply")
	if r.code != exitState || !strings.Contains(r.stderr, "has no plan") || !strings.Contains(r.stderr, "publish --plan") {
		t.Errorf("code %d\n%s", r.code, r.stderr)
	}
	if len(srv.Requests()) != 0 {
		t.Errorf("the platform was asked %v", srv.Requests())
	}
}

func TestPublishTamperedPlanIsAnInvalidState(t *testing.T) {
	e, srv, runDir := finalizedS1(t)
	if r := runWith(t, e, "publish", "--plan", "both"); r.code != exitOK {
		t.Fatalf("plan: %s", r.stderr)
	}
	path := filepath.Join(runDir, "acr-plugin-sandbox_sample-app-2-76ae5f13", state.PlanFile)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.Replace(string(data), "BLOCKER", "MINOR", 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	asked := len(srv.Requests())
	r := runWith(t, e, "publish", "--apply")
	if r.code != exitState || !strings.Contains(r.stderr, "not the one that was made") || strings.Contains(r.stderr, reviewreplay.Canary) {
		t.Errorf("code %d\n%s", r.code, r.stderr)
	}
	if len(srv.Requests()) != asked || len(srv.Writes()) != 0 {
		t.Errorf("the platform was reached after the plan changed: %v", srv.Requests()[asked:])
	}
}

func TestPublishHeadChangedIsExitFour(t *testing.T) {
	e, srv, runDir := finalizedS1(t)
	if r := runWith(t, e, "publish", "--plan", "both"); r.code != exitOK {
		t.Fatalf("plan: %s", r.stderr)
	}
	srv.Repo("acr-plugin-sandbox/sample-app").PRs[2].Head = strings.Repeat("e", 40)
	r := runWith(t, e, "publish", "--apply")
	if r.code != exitHeadChanged || exitHeadChanged != 4 {
		t.Fatalf("code %d\n%s", r.code, r.stderr)
	}
	for _, want := range []string{"nothing was written: head-changed"} {
		if !strings.Contains(r.stdout, want) {
			t.Errorf("stdout lacks %q:\n%s", want, r.stdout)
		}
	}
	for _, want := range []string{"1 change request changed since they were reviewed: nothing is written for it", "acr-plugin-sandbox/sample-app#2", "76ae5f13", "eeeeeeee", "review <pull request>"} {
		if !strings.Contains(r.stderr, want) {
			t.Errorf("stderr lacks %q:\n%s", want, r.stderr)
		}
	}
	if len(srv.Writes()) != 0 {
		t.Errorf("wrote %v", srv.Writes())
	}
	if st, _ := state.Load(runDir); st.Phase != state.Published {
		t.Errorf("phase %s: every action has a final outcome", st.Phase)
	}
	// The plan of a change request whose head changed is exit 4 as well.
	e2, srv2, _ := finalizedS1(t)
	srv2.Repo("acr-plugin-sandbox/sample-app").PRs[2].Head = strings.Repeat("e", 40)
	if p := runWith(t, e2, "publish", "--plan", "both"); p.code != exitHeadChanged || !strings.Contains(p.stdout, "skip   head-changed") {
		t.Errorf("plan: code %d\n%s\n%s", p.code, p.stdout, p.stderr)
	}
}

func TestPublishPlatformFailuresAreExitSevenAndSix(t *testing.T) {
	for _, c := range []struct {
		name  string
		fault ghserver.Fault
		code  int
	}{
		{"server error", ghserver.Fault{Status: 502, Body: `{}`, Times: 1}, exitPlatform},
		{"no permission", ghserver.Fault{Status: 403, Body: `{"message":"Resource not accessible"}`, Times: 1}, exitAuth},
	} {
		t.Run(c.name, func(t *testing.T) {
			e, srv, runDir := finalizedS1(t)
			if r := runWith(t, e, "publish", "--plan", "comments"); r.code != exitOK {
				t.Fatalf("plan: %s", r.stderr)
			}
			srv.Fail("POST", `/reviews$`, c.fault)
			r := runWith(t, e, "publish", "--apply")
			if r.code != c.code || !strings.Contains(r.stdout, "only partly published") || !strings.Contains(r.stdout, "to do  inline comment") || !strings.Contains(r.stderr, "what to do:") {
				t.Fatalf("code %d, want %d\nstdout %s\nstderr %s", r.code, c.code, r.stdout, r.stderr)
			}
			if st, _ := state.Load(runDir); st.Phase != state.Planned {
				t.Errorf("phase %s", st.Phase)
			}
			again := runWith(t, e, "publish", "--apply")
			if again.code != exitOK || !strings.Contains(again.stdout, "Published run") {
				t.Errorf("resume: code %d\n%s\n%s", again.code, again.stderr, again.stdout)
			}
			if got := len(srv.Repo("acr-plugin-sandbox/sample-app").PRs[2].Inlines); got != 3 {
				t.Errorf("%d comments posted", got)
			}
		})
	}
}

func TestPublishUsageListsTheCommand(t *testing.T) {
	var out, errOut strings.Builder
	if code := run(nil, env{stdout: &out, stderr: &errOut}); code != exitUsage || !strings.Contains(errOut.String(), "publish --plan <comments|issues|both>") || !strings.Contains(errOut.String(), "publish --apply") {
		t.Errorf("code %d\n%s", code, errOut.String())
	}
}
