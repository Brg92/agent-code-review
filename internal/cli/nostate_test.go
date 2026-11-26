package cli

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Brg92/agent-code-review/internal/testutil/reviewreplay"
	"github.com/Brg92/agent-code-review/internal/testutil/sandbox"
)

// call is a command in a world of its own: the environment it runs in, its
// arguments, and the temporary folders to hide from the output, which differ
// between two worlds that are otherwise the same.
type call struct {
	e     env
	args  []string
	paths []string
}

// output is what a call prints and returns, with its folders hidden.
func (c call) output(t *testing.T) string {
	t.Helper()
	r := runWith(t, c.e, c.args...)
	text := fmt.Sprintf("code %d\n%s\n--\n%s", r.code, r.stdout, r.stderr)
	for _, p := range c.paths {
		text = strings.ReplaceAll(text, p, "<dir>")
	}
	return text
}

// noRunCall is a command in a world that has no run and no platform.
func noRunCall(args ...string) func(t *testing.T) call {
	return func(t *testing.T) call {
		e, work := reviewEnv(t, sandbox.NewSynthetic(t))
		return call{e, args, []string{work}}
	}
}

func worldCall(setup func(w *world), args ...string) func(t *testing.T) call {
	return func(t *testing.T) call {
		w := newWorld(t)
		setup(w)
		return call{w.e, append([]string{"doctor"}, args...), []string{w.work}}
	}
}

// TestNothingIsKeptBetweenCommands runs every command twice in one process,
// each time in a state different from the one of the call before: the output
// of a command must not depend on what ran before it. A world is built anew
// for each call; what could leak is what the process shares.
func TestNothingIsKeptBetweenCommands(t *testing.T) {
	reviewing := func(t *testing.T) call {
		r := reviewreplay.New(t, "S1")
		reviewreplay.AddBuildFiles(t, r, "S1", "sample-app", 2)
		e, work := reviewEnv(t, r)
		return call{e, []string{"review", s1Root}, []string{work}}
	}
	reviewS1 := func(t *testing.T) call {
		e, work, runDir, judgement := reviewedS1(t)
		copyFile(t, []string{"testdata", "scenarios", "S1", "judgements", "sample-app-2.json"}, judgement)
		return call{e, nil, []string{work, runDir}}
	}
	with := func(base func(t *testing.T) call, args ...string) func(t *testing.T) call {
		return func(t *testing.T) call {
			c := base(t)
			c.args = args
			return c
		}
	}
	finalized := func(t *testing.T) call {
		e, _, runDir := finalizedS1(t)
		return call{e, nil, []string{filepath.Dir(filepath.Dir(runDir)), runDir}}
	}
	planned := func(t *testing.T) call {
		c := finalized(t)
		if r := runWith(t, c.e, "publish", "--plan", "comments"); r.code != exitOK {
			t.Fatalf("publish --plan: code %d\n%s", r.code, r.stderr)
		}
		return c
	}
	queueing := func(t *testing.T) call {
		e, work, _ := queueEnv(t)
		return call{e, []string{"queue"}, []string{work}}
	}
	criteria := func(name string) func(t *testing.T) call {
		return func(t *testing.T) call {
			dir := fixture(t, name)
			e := offline(t)
			e.random = strings.NewReader("\x0a\x1b\x2c\x3d")
			return call{e, []string{"check-criteria", dir}, []string{dir}}
		}
	}
	login := func(helper string) func(t *testing.T) call {
		return func(t *testing.T) call {
			return call{env{git: loginGit(helper), stdin: strings.NewReader(testToken + "\n")}, []string{"auth", "login"}, nil}
		}
	}
	tests := []struct {
		name string
		// a and b are two states of the same command, with different results.
		a, b func(t *testing.T) call
	}{
		{"version", func(t *testing.T) call { return call{env{}, []string{"version"}, nil} },
			func(t *testing.T) call { return call{env{}, []string{"--version"}, nil} }},
		{"review", reviewing, func(t *testing.T) call {
			r := sandbox.NewSynthetic(t)
			r.Add(sandbox.Route{Path: "/repos/o/r/pulls/7", Status: 404, Body: `{"message":"no"}`})
			e, work := reviewEnv(t, r)
			return call{e, []string{"review", "o/r#7"}, []string{work}}
		}},
		{"next", with(reviewS1, "next"), noRunCall("next")},
		{"finalize", with(reviewS1, "finalize"), noRunCall("finalize")},
		{"show", with(finalized, "show"), noRunCall("show")},
		{"publish --plan", with(finalized, "publish", "--plan", "comments"), noRunCall("publish", "--plan", "comments")},
		{"publish --apply", with(planned, "publish", "--apply"), noRunCall("publish", "--apply")},
		{"queue", queueing, func(t *testing.T) call {
			r := sandbox.NewSynthetic(t)
			route := queueRoute(t, "30")
			route.Status, route.Body = 500, `{"message":"no"}`
			r.Add(route)
			e, work := reviewEnv(t, r)
			return call{e, []string{"queue"}, []string{work}}
		}},
		{"doctor", worldCall(func(*world) {}), worldCall(noToken)},
		{"check-criteria", criteria("valid"), criteria("legacy")},
		{"auth status", func(t *testing.T) call {
			w := newWorld(t)
			return call{w.e, []string{"auth", "status"}, []string{w.work}}
		}, func(t *testing.T) call {
			w := newWorld(t)
			noToken(w)
			return call{w.e, []string{"auth", "status"}, []string{w.work}}
		}},
		{"auth login", login("osxkeychain"), login("")},
		{"auth logout", func(t *testing.T) call {
			return call{env{git: &scriptedGit{answers: map[string]gitAnswer{"credential reject": {}}}}, []string{"auth", "logout"}, nil}
		}, func(t *testing.T) call {
			return call{env{git: &scriptedGit{answers: map[string]gitAnswer{"credential reject": {err: errExit1}}}}, []string{"auth", "logout"}, nil}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			first := tt.a(t).output(t)
			other := tt.b(t).output(t)
			second := tt.a(t).output(t)
			otherAgain := tt.b(t).output(t)
			if first != second {
				t.Errorf("the same call gave another answer after a different one:\nfirst:\n%s\nsecond:\n%s", first, second)
			}
			if other != otherAgain {
				t.Errorf("the other call gave another answer the second time:\nfirst:\n%s\nsecond:\n%s", other, otherAgain)
			}
			if tt.name != "version" && first == other {
				t.Errorf("the two states are not different, the test proves nothing:\n%s", first)
			}
		})
	}
}
