package index

import (
	"github.com/balzaczyy/golucene/core/util"
)

type InvertedDocConsumer interface {
	// Abort (called after hitting abort error)
	abort()
}

/*
This class implements InvertedDocConsumer, which is passed each token
produced by the analyzer on each field. It stores these tokens in a
hash table, and allocates separate bytes streams per token. Consumers
of this class, e.g., FreqproxTermsWriter and TermVectorsConsumer,
write their own byte streams under each term.
*/
type TermsHash struct {
	consumer      TermsHashConsumer
	nextTermsHash *TermsHash

	intPool      *util.IntBlockPool
	bytePool     *util.ByteBlockPool
	termBytePool *util.ByteBlockPool
	bytesUsed    util.Counter

	primary  bool
	docState *docState

	trackAllocations bool
}

func newTermsHash(docWriter *DocumentsWriterPerThread,
	consumer TermsHashConsumer, trackAllocations bool,
	nextTermsHash *TermsHash) *TermsHash {

	ans := &TermsHash{
		docState:         docWriter.docState,
		consumer:         consumer,
		trackAllocations: trackAllocations,
		nextTermsHash:    nextTermsHash,
		intPool:          util.NewIntBlockPool(docWriter.intBlockAllocator),
		bytePool:         util.NewByteBlockPool(docWriter.byteBlockAllocator),
	}
	if trackAllocations {
		ans.bytesUsed = docWriter._bytesUsed
	} else {
		ans.bytesUsed = util.NewCounter()
	}
	ans.primary = nextTermsHash != nil
	if ans.primary {
		ans.termBytePool = ans.bytePool
		nextTermsHash.termBytePool = ans.bytePool
	}
	return ans
}

func (hash *TermsHash) abort() {
	hash.reset()
	defer func() {
		if hash.nextTermsHash != nil {
			hash.nextTermsHash.abort()
		}
	}()
	hash.consumer.abort()
}

/* Clear all state */
func (hash *TermsHash) reset() {
	// we don't reuse so we drop everything and don't fill with 0
	hash.intPool.Reset(false, false)
	hash.bytePool.Reset(false, false)
}
