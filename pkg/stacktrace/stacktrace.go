package stacktrace

import (
	"fmt"
	"runtime"
	"strings"
)

// The linker uses the magic symbol prefixes "go." and "type."
// https://golang.org/src/cmd/compile/internal/gc/subr.go
var skipFunctionNamePrefixes = []string{"go.", "type."}

const (
	runtimePackage      = "runtime"
	maxStackTraceFrames = 20
)

type Frame struct {
	Line    int    `json:"line"`
	Func    string `json:"func"`
	File    string `json:"file"`
	Package string `json:"package"`
}

func getStackTrace(skipCaller int) []uintptr {
	pc := make([]uintptr, maxStackTraceFrames)
	callers := runtime.Callers(skipCaller, pc)

	return pc[:callers]
}
func NewStackTrace(skipCaller int) []Frame {
	st := getStackTrace(skipCaller)
	callers := runtime.CallersFrames(st)

	var frames []Frame
	for {
		cFrame, more := callers.Next()

		frame := newFrame(cFrame)
		if isProcessableFrame(frame.Package) {
			frames = append(frames, frame)
		}

		if !more {
			break
		}
	}

	return frames
}

func newFrame(frame runtime.Frame) Frame {
	f := frame.Function
	pkg := extractPackage(f)
	fun := strings.TrimPrefix(f, fmt.Sprintf("%s.", pkg))

	return Frame{
		Line:    frame.Line,
		Func:    fun,
		File:    frame.File,
		Package: pkg,
	}
}

func extractPackage(name string) string {
	if isSkipFunctionName(name) {
		return ""
	}

	pathEnd := strings.LastIndex(name, "/")

	if pathEnd < 0 {
		pathEnd = 0
	}

	if i := strings.Index(name[pathEnd:], "."); i != -1 {
		return name[:pathEnd+i]
	}

	return ""
}

func isSkipFunctionName(name string) bool {
	for _, v := range skipFunctionNamePrefixes {
		if strings.HasPrefix(name, v) {
			return true
		}
	}

	return false
}

func isProcessableFrame(packageName string) bool {
	return packageName != runtimePackage
}
