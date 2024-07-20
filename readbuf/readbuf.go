package readbuf

import (
	// "bufio"
	// "bytes"
	"io"
)

type ReaderBuf struct {
	reader io.Reader

	internalCache *[]byte
	cacheMax      int64
}

func NewReaderBuf(ir io.Reader, cacheSize int64) *ReaderBuf {
	// var cache [cacheSize]byte
	cache := make([]byte, cacheSize) // len(a)=5
	return &ReaderBuf{reader: ir, cacheMax: 0, internalCache: &cache}
}

func (rb *ReaderBuf) ReadAt(p []byte, off int64) (n int, err error) {
	readStart := off
	readEnd := off + int64(len(p))

	if readEnd <= rb.cacheMax {
		ic := *rb.internalCache
		return copy(p, ic[readStart:]), nil
	}

	if readStart < rb.cacheMax {
		ic := *rb.internalCache

		amtOverlap := rb.cacheMax - readStart
		amt := copy(p[:amtOverlap], ic[readStart:rb.cacheMax])

		red, err := rb.reader.Read(p[amtOverlap:])
		copy(ic[rb.cacheMax:], p[amtOverlap:])

		rb.cacheMax += int64(red)
		return amt + red, err
	} else {
		ic := *rb.internalCache
		// after, so first we need to discard
		discarded_bytes, err := rb.reader.Read(ic[rb.cacheMax:readStart])
		// disc, err := rb.reader.Read()
		n, err := rb.reader.Read(p)

		copy(ic[readStart:], p)

		rb.cacheMax += int64(discarded_bytes) + int64(n)
		return n, err
	}
}
