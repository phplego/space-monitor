package main

import (
	"io"
	"io/ioutil"
)

// FilterWriter type is Writer with functional filter.
type FilterWriter struct {
	filter func([]byte) []byte
	writer io.Writer
}

func (it *FilterWriter) Write(p []byte) (n int, err error) {
	_, errr := it.writer.Write(it.filter(p))
	return len(p), errr
}

var _ io.Writer = (*FilterWriter)(nil) //FilterWriter is compatible with io.Writer interface

// FilterFunc returns new FilterWriter instance.
func FilterFunc(w io.Writer, filter func([]byte) []byte) *FilterWriter {
	if w == nil {
		w = ioutil.Discard
	}
	return &FilterWriter{filter: filter, writer: w}
}
