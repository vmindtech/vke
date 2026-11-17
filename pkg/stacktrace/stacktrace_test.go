package stacktrace

import (
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFrameExtraction(t *testing.T) {
	for _, tc := range []struct{ name, pkg string }{
		{"github.com/vmindtech/vke/pkg/stacktrace.TestFrameExtraction", "github.com/vmindtech/vke/pkg/stacktrace"},
		{"net/http.(*Client).Do", "net/http"}, {"main.main", "main"},
		{"plain", ""}, {"go.shape.string", ""}, {"type.eq.example", ""},
	} {
		assert.Equal(t, tc.pkg, extractPackage(tc.name), tc.name)
	}
	assert.Equal(t, Frame{Line: 42, File: "test.go", Package: "net/http", Func: "(*Client).Do"}, newFrame(runtime.Frame{Line: 42, File: "test.go", Function: "net/http.(*Client).Do"}))
	assert.False(t, isProcessableFrame("runtime"))
	assert.True(t, isProcessableFrame("main"))
}

func TestNewStackTrace(t *testing.T) {
	frames := NewStackTrace(0)
	require.NotEmpty(t, frames)
	found := false
	for _, f := range frames {
		assert.NotEqual(t, "runtime", f.Package)
		if f.Func == "TestNewStackTrace" {
			found = true
			assert.Positive(t, f.Line)
			assert.Contains(t, f.File, "stacktrace_test.go")
		}
	}
	assert.True(t, found, "the caller must be included")
}
