package cli

import (
	"os"
	"testing"

	"github.com/Brg92/agent-code-review/internal/testutil/credhelper"
)

// TestMain lets the test binary act as a git credential helper, for the
// tests that run a real git.
func TestMain(m *testing.M) {
	credhelper.Main()
	os.Exit(m.Run())
}
