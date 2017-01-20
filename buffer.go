package gohttptun

import (
	"bytes"
	"sync"
	"io"
)

type SyncBuffer struct {
	b bytes.Buffer
	m sync.Mutex
}

func (b *SyncBuffer) Write(p []byte) (int, error) {
	b.m.Lock()
	defer b.m.Unlock()
	return b.b.Write(p)
}

func (b *SyncBuffer) Read(p []byte) (int, error) {
	b.m.Lock()
	defer b.m.Unlock()
	return b.b.Read(p)
}

func (b *SyncBuffer) WriteTo(w io.Writer) (int64, error) {
	b.m.Lock()
	defer b.m.Unlock()
	if b.b.Len() == 0 {
		return 0, nil
	}
	buf := b.b.Bytes()
	n, err := w.Write(buf)
	b.b.Reset()
	return int64(n), err
}
