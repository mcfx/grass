package grass

import (
	"bufio"
	"io"
	"sync"
)

type Writer struct {
	W *bufio.Writer
	M *sync.Mutex
}

func NewWriter(w io.Writer) Writer {
	var b Writer
	b.W = bufio.NewWriter(w)
	b.M = &sync.Mutex{}
	return b
}
