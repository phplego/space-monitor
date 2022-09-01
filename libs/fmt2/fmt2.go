// Package is a simple fmt-like Print* functions set
// It allows to set custom output writer

package fmt2

import (
	"fmt"
	"io"
	"os"
)

var OutWriter io.Writer = os.Stdout

func Print(args ...any) {
	fmt.Fprint(OutWriter, args...)
}

func Println(args ...any) {
	fmt.Fprintln(OutWriter, args...)
}

func Printf(format string, args ...any) {
	fmt.Fprintf(OutWriter, format, args...)
}
