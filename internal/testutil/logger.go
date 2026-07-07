package testutil

import (
	"io"

	"github.com/sirupsen/logrus"
)

// NopLogger returns a logrus logger that discards all output.
func NopLogger() *logrus.Logger {
	l := logrus.New()
	l.SetOutput(io.Discard)
	return l
}
