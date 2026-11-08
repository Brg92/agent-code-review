package refs

import (
	"reflect"
	"testing"

	"github.com/Brg92/agent-code-review/internal/core"
)

func TestNormalize(t *testing.T) {
	k := func(n int) core.Key {
		return core.Key{Forge: core.GitHub, Host: "github.com", Project: "Acme/App", Number: n}
	}
	lower := k(1)
	lower.Project = "acme/app"
	in := []Ref{{Key: k(3), Pos: 20}, {Key: k(1), Pos: 5, URL: true}, {Key: lower, Pos: 9}, {Key: k(2), Pos: 12}, {Key: k(3), Pos: 30}}
	want := []Ref{{Key: k(1), Pos: 5, URL: true}, {Key: k(2), Pos: 12}, {Key: k(3), Pos: 20}}
	if got := Normalize(in); !reflect.DeepEqual(got, want) {
		t.Errorf("Normalize() = %+v, want %+v", got, want)
	}
	if got := Normalize(nil); got != nil {
		t.Errorf("Normalize(nil) = %+v", got)
	}
}
