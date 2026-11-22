package base

import (
	"context"
	"reflect"
	"testing"

	"github.com/Brg92/agent-code-review/internal/core"
)

// provider is a ForgeProvider made of a Reader and a Writer.
type provider struct {
	Reader
	Writer
}

type writer struct{}

func (writer) CreateReview(context.Context, core.Key, string, []ReviewComment) error { return nil }
func (writer) CreateComment(context.Context, core.Key, string) error                 { return nil }
func (writer) CreateIssue(context.Context, core.Key, NewIssue) (int, error)          { return 1, nil }
func (writer) UpdateIssue(context.Context, core.Key, int, string, string) error      { return nil }
func (writer) CreateLabel(context.Context, core.Key, core.Label) error               { return nil }
func (writer) UpdateDescription(context.Context, core.Key, string) error             { return nil }

func TestForgeProviderIsAReaderAndAWriter(t *testing.T) {
	var p ForgeProvider = provider{&reader{}, writer{}}
	if p.Capabilities().Search != "" {
		t.Error("the reader is not the one given")
	}
	typ := reflect.TypeFor[ForgeProvider]()
	for _, part := range []reflect.Type{reflect.TypeFor[Reader](), reflect.TypeFor[Writer]()} {
		for i := range part.NumMethod() {
			if _, ok := typ.MethodByName(part.Method(i).Name); !ok {
				t.Errorf("ForgeProvider lacks %s", part.Method(i).Name)
			}
		}
	}
	if got, want := typ.NumMethod(), reflect.TypeFor[Reader]().NumMethod()+reflect.TypeFor[Writer]().NumMethod(); got != want {
		t.Errorf("ForgeProvider has %d methods, want the %d of Reader and Writer", got, want)
	}
}

// TestWriterHasOnlyWhatPublishUses names every write the platform
// abstraction offers. Adding one means changing this test on purpose.
func TestWriterHasOnlyWhatPublishUses(t *testing.T) {
	typ := reflect.TypeFor[Writer]()
	var got []string
	for i := range typ.NumMethod() {
		got = append(got, typ.Method(i).Name)
	}
	want := []string{"CreateComment", "CreateIssue", "CreateLabel", "CreateReview", "UpdateDescription", "UpdateIssue"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Writer methods = %v, want %v", got, want)
	}
}
