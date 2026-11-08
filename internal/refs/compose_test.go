package refs

import (
	"reflect"
	"testing"

	"github.com/Brg92/agent-code-review/internal/core"
)

type fixed struct {
	forge core.Forge
	found []Ref
}

func (f fixed) Forge() core.Forge            { return f.forge }
func (f fixed) Parse(string, core.Key) []Ref { return append([]Ref(nil), f.found...) }

func TestCompose(t *testing.T) {
	gh := func(n int) core.Key {
		return core.Key{Forge: core.GitHub, Host: "github.com", Project: "acme/app", Number: n}
	}
	gl := func(n int) core.Key {
		return core.Key{Forge: core.GitLab, Host: "gitlab.com", Project: "acme/app", Number: n}
	}
	primary := fixed{core.GitHub, []Ref{{Key: gh(1), Pos: 5}}}
	other := fixed{core.GitLab, []Ref{{Key: gl(2), Pos: 0, URL: true}, {Key: gl(3), Pos: 10}}}
	sameForge := fixed{core.GitHub, []Ref{{Key: gh(9), Pos: 20, URL: true}}}

	r := Compose(primary, other, sameForge)
	if r.Forge() != core.GitHub {
		t.Errorf("Forge() = %q, want the primary's", r.Forge())
	}
	got := r.Parse("line", gh(7))
	want := []Ref{{Key: gl(2), Pos: 0, URL: true}, {Key: gh(1), Pos: 5}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Parse() = %+v, want %+v: only the links of other platforms are added", got, want)
	}
}
