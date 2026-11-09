package scenario

import "testing"

func TestReview(t *testing.T) {
	r := Review(t, "core-lib", 1, "S2", "S3")
	if r.CR.String() != "github:github.com/acr-plugin-sandbox/core-lib#1" || r.HeadSHA != "1bb2c03af22c5360e04698de42d0833c0387b499" || len(r.Files) != 6 {
		t.Fatalf("Review = %s %s, %d files", r.CR, r.HeadSHA, len(r.Files))
	}
	for _, f := range r.Files {
		switch f.Diff.Path {
		case "src/main/java/com/example/core/session/codec/TokenCodec.kt":
			// The base version of a renamed file is read at its old path.
			if len(f.Head) == 0 || len(f.Base) == 0 || f.Base[0] != "package com.example.core.session" {
				t.Errorf("TokenCodec.kt: head %d lines, base %q", len(f.Head), f.Base)
			}
		case "src/main/java/com/example/core/session/SessionExpiry.kt":
			if f.Base != nil || len(f.Head) != 11 {
				t.Errorf("SessionExpiry.kt: base %q, %d head lines", f.Base, len(f.Head))
			}
		}
	}
	for _, id := range []string{"uses-removed-api", "logic-defect", "no-double-bang"} {
		if _, ok := r.Rules[id]; !ok {
			t.Errorf("rule %s missing", id)
		}
	}
}

func TestJudgement(t *testing.T) {
	j := Judgement(t, "S3", "core-lib", 1)
	if j.CR != "github:github.com/acr-plugin-sandbox/core-lib#1" || len(j.Findings) != 2 {
		t.Errorf("Judgement = %+v", j)
	}
}
