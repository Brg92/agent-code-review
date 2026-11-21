package reviewreplay

import (
	"context"
	"strings"
	"testing"

	"github.com/Brg92/agent-code-review/internal/testutil/sandbox"
)

func TestTokensResolveTheCanary(t *testing.T) {
	tok, err := Tokens{}.Resolve(context.Background(), "github.com")
	if err != nil || tok.Value() != Canary {
		t.Errorf("token %v, err %v", tok, err)
	}
	if _, err := (Tokens{}).Resolve(context.Background(), "not a host"); err == nil {
		t.Error("an invalid host resolved")
	}
}

func TestS2ServesWhatAReviewReads(t *testing.T) {
	r := S2(t)
	if r.URL() == "" {
		t.Fatal("no URL")
	}
	pull := sandbox.PullRequest(t, "S4", "feature-lib", 1)
	if pull.HeadSHA == "" || strings.Contains(r.URL(), "api.github.com") {
		t.Errorf("pull %+v, url %s", pull, r.URL())
	}
}
