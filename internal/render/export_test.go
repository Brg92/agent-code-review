package render

import "testing"

// Golden compares got with testdata/render/<name>.txt, or rewrites the file
// with -update, for the tests of package render_test.
func Golden(t *testing.T, name, got string) { golden(t, name, got) }
