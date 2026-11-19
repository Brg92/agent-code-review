package github

import (
	"context"
	"reflect"
	"testing"

	"github.com/Brg92/agent-code-review/internal/core"
	"github.com/Brg92/agent-code-review/internal/testutil/sandbox"
)

func TestCIOnRecordedChecks(t *testing.T) {
	p, _, _ := onReplay(t, "S5")
	ctx := context.Background()

	running := sandbox.PullRequest(t, "S5", "sample-app", 3).HeadSHA
	ci, err := p.CI(ctx, key("sample-app", 3), running)
	if err != nil {
		t.Fatal(err)
	}
	failure, success := "failure", "success"
	want := []core.Check{
		{Name: "lint", Status: "completed", Conclusion: &failure},
		{Name: "test", Status: "completed", Conclusion: &success},
		{Name: "instrumented", Status: "in_progress"},
		{Name: "lint", Status: "completed", Conclusion: &failure},
		{Name: "test", Status: "completed", Conclusion: &success},
		{Name: "instrumented", Status: "in_progress"},
	}
	if ci.State != core.CIFailing || ci.Reason != "" || !reflect.DeepEqual(ci.Checks, want) {
		t.Errorf("CI(%s) = %+v, want failing with %+v", running, ci, want)
	}

	idle := sandbox.PullRequest(t, "S5", "sample-app", 4).HeadSHA
	ci, err = p.CI(ctx, key("sample-app", 4), idle)
	if err != nil || ci.State != core.CINone || ci.Checks == nil || len(ci.Checks) != 0 {
		t.Errorf("CI(%s) = %+v, %v, want none and an empty list of checks", idle, ci, err)
	}
}
