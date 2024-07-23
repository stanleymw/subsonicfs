package readbuf

import (
	"io"
)

type ReaderBuf struct {
	Reader *io.Reader

	InternalCache *[]byte
	ReadPosition  int64
}

func NewReaderBufWithPreallocatedCache(ir *io.Reader, cache *[]byte) *ReaderBuf {
	return &ReaderBuf{Reader: ir, InternalCache: cache, ReadPosition: 0}
}

func NewReaderBuf(ir *io.Reader, cacheSize int64) *ReaderBuf {
	// var cache [cacheSize]byte
	cache := make([]byte, cacheSize) // len(a)=5
	return &ReaderBuf{Reader: ir, InternalCache: &cache, ReadPosition: 0}
}

func (rb *ReaderBuf) EnsureCached(readStart int64, readEnd int64) (n int, err error) {
	if readEnd >= rb.ReadPosition {
		// not cached || partially cached

		// cache up all the way to where we want
		var amt int
		for rb.ReadPosition < readEnd {
			temp_amt, _ := (*rb.Reader).Read((*rb.InternalCache)[rb.ReadPosition:readEnd])

			amt += temp_amt
			rb.ReadPosition += int64(temp_amt)
		}
		// if err != nil {
		// 	fmt.Println(amt, err)
		// 	return amt, err
		// }
		// ok then lets read from the cache

		return amt, nil
	} else {
		//fully cached
		return int(readEnd) - int(readStart), nil
	}

}

func (rb *ReaderBuf) ReadAt(p *[]byte, off int64) (n int, err error) {
	readStart := off
	readEnd := min(off+int64(len(*p)), int64(len(*rb.InternalCache)))

	if readStart >= rb.ReadPosition {
		// not cached AT ALL

		// cache up all the way to where we want
		amt, err := (*rb.Reader).Read((*rb.InternalCache)[rb.ReadPosition:readEnd])
		rb.ReadPosition += int64(amt)

		if err != nil {
			return amt, err
		}

		// ok then lets read from the cache
		amt = copy(*p, (*rb.InternalCache)[readStart:readEnd])
		return amt, nil
	} else {
		// (partially?) already cached
		if readEnd > rb.ReadPosition {
			// just cache up to the end
			amt, err := (*rb.Reader).Read((*rb.InternalCache)[rb.ReadPosition:readEnd])
			rb.ReadPosition += int64(amt)

			if err != nil {
				return amt, err
			}

			// ok then lets read from the cache
			amt = copy(*p, (*rb.InternalCache)[readStart:readEnd])
			return amt, nil
		} else {
			//fully cached
			amt := copy(*p, (*rb.InternalCache)[readStart:readEnd])
			return amt, nil
		}
	}

	// if readEnd <= rb.readPosition {
	// 	ic := *rb.internalCache
	// 	return copy(*p, ic[readStart:]), nil
	// }
	//
	// if readStart < rb.readPosition {
	// 	ic := *rb.internalCache
	//
	// 	amtOverlap := rb.readPosition - readStart
	// 	amt := copy(p[:amtOverlap], ic[readStart:rb.cacheMax])
	//
	// 	red, err := rb.reader.Read(p[amtOverlap:])
	// 	copy(ic[rb.cacheMax:], p[amtOverlap:])
	//
	// 	rb.cacheMax += int64(red)
	// 	return amt + red, err
	// } else {
	// 	ic := *rb.internalCache
	// 	// after, so first we need to discard
	// 	discarded_bytes, err := rb.reader.Read(ic[rb.cacheMax:readStart])
	// 	// disc, err := rb.reader.Read()
	// 	n, err := rb.reader.Read(p)
	//
	// 	copy(ic[readStart:], p)
	//
	// 	rb.cacheMax += int64(discarded_bytes) + int64(n)
	// 	return n, err
	// }
}
