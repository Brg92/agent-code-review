package github

import (
	"context"
	"testing"

	"github.com/Brg92/agent-code-review/internal/cascade"
	"github.com/Brg92/agent-code-review/internal/core"
)

// Synthetic: the sandbox has no change request that links another host.

func TestCascadeSkipsAnotherHostAsOtherPlatformWithoutReadingIt(t *testing.T) {
	s := onSynthetic(t, nil)
	body := "Does a thing.\\n\\n## Related Changes\\n\\n- [ ] https://ghe.example.com/acme/other/pull/4\\n- [ ] https://github.com/acme/lib/pull/2\\n- [ ] https://example.com/acme/lib/pull/3"
	s.route(200, repo+"/pulls/7", nil, nil, `{"title":"Root","body":"`+body+`","head":{"sha":"`+sha+`"},"base":{"sha":"b","ref":"main"},"state":"open","user":{"login":"u"}}`)
	s.route(200, "/repos/acme/lib/pulls/2", nil, nil, `{"title":"Lib","body":"","head":{"sha":"`+sha+`"},"base":{"sha":"b","ref":"main"},"state":"open","user":{"login":"u"}}`)

	res, err := cascade.Resolve(context.Background(), s.Changes(), Registry{}, synthKey, core.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	skipped := map[string]core.SkipReason{}
	for _, e := range res.Entries {
		if e.Status == core.Skipped {
			skipped[e.CR.String()] = e.SkipReason
		}
	}
	other := "github:ghe.example.com/acme/other#4"
	if skipped[other] != core.OtherPlatform {
		t.Errorf("skipped = %v, want %s as other-platform", skipped, other)
	}
	if !res.Incomplete() {
		t.Error("the cascade is complete although a change request was not reviewed")
	}
	for _, r := range s.Replay.Requests() {
		if r != "GET "+repo+"/pulls/7" && r != "GET /repos/acme/lib/pulls/2" {
			t.Errorf("unexpected request %q: another host must never be contacted", r)
		}
	}
	if got := s.Tokens.asked.Load(); got != 2 {
		t.Errorf("the token was asked for %d times, want 2 (the two reads on github.com)", got)
	}
}
