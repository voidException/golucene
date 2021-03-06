package index

import (
	"errors"
	"fmt"
	"github.com/balzaczyy/golucene/core/codec"
	"github.com/balzaczyy/golucene/core/codec/compressing"
	"github.com/balzaczyy/golucene/core/codec/lucene40"
	"github.com/balzaczyy/golucene/core/index/model"
	"github.com/balzaczyy/golucene/core/store"
	"github.com/balzaczyy/golucene/core/util"
	"github.com/balzaczyy/golucene/core/util/packed"
	"log"
	"reflect"
)

// codecs/lucene41/Lucene41PostingsFormat.java

type Lucene41PostingsFormat struct {
}

func (f *Lucene41PostingsFormat) Name() string {
	return "Lucene41"
}

func (f *Lucene41PostingsFormat) String() {
	panic("not implemented yet")
}

func (f *Lucene41PostingsFormat) FieldsConsumer(state SegmentWriteState) (FieldsConsumer, error) {
	panic("not implemented yet")
}

func (f *Lucene41PostingsFormat) FieldsProducer(state SegmentReadState) (FieldsProducer, error) {
	postingsReader, err := NewLucene41PostingsReader(state.dir,
		state.fieldInfos,
		state.segmentInfo,
		state.context,
		state.segmentSuffix)
	if err != nil {
		return nil, err
	}
	success := false
	defer func() {
		if !success {
			log.Printf("Failed to load FieldsProducer for %v.", f.Name())
			if err != nil {
				log.Print("DEBUG ", err)
			}
			util.CloseWhileSuppressingError(postingsReader)
		}
	}()

	fp, err := newBlockTreeTermsReader(state.dir,
		state.fieldInfos,
		state.segmentInfo,
		postingsReader,
		state.context,
		state.segmentSuffix,
		state.termsIndexDivisor)
	if err != nil {
		log.Print("DEBUG: ", err)
		return fp, err
	}
	success = true
	return fp, nil
}

// Lucene41PostingsReader.java

const (
	LUCENE41_DOC_EXTENSION = "doc"
	LUCENE41_POS_EXTENSION = "pos"
	LUCENE41_PAY_EXTENSION = "pay"

	LUCENE41_BLOCK_SIZE = 128

	LUCENE41_TERMS_CODEC = "Lucene41PostingsWriterTerms"
	LUCENE41_DOC_CODEC   = "Lucene41PostingsWriterDoc"
	LUCENE41_POS_CODEC   = "Lucene41PostingsWriterPos"
	LUCENE41_PAY_CODEC   = "Lucene41PostingsWriterPay"

	LUCENE41_VERSION_START   = 0
	LUCENE41_VERSION_CURRENT = LUCENE41_VERSION_START
)

/*
Concrete class that reads docId (maybe frq,pos,offset,payload) list
with postings format.
*/
type Lucene41PostingsReader struct {
	docIn   store.IndexInput
	posIn   store.IndexInput
	payIn   store.IndexInput
	forUtil ForUtil
}

func NewLucene41PostingsReader(dir store.Directory,
	fis model.FieldInfos, si *model.SegmentInfo,
	ctx store.IOContext, segmentSuffix string) (r PostingsReaderBase, err error) {

	log.Print("Initializing Lucene41PostingsReader...")
	success := false
	var docIn, posIn, payIn store.IndexInput = nil, nil, nil
	defer func() {
		if !success {
			log.Print("Failed to initialize Lucene41PostingsReader.")
			if err != nil {
				log.Print("DEBUG ", err)
			}
			util.CloseWhileSuppressingError(docIn, posIn, payIn)
		}
	}()

	docIn, err = dir.OpenInput(util.SegmentFileName(si.Name, segmentSuffix, LUCENE41_DOC_EXTENSION), ctx)
	if err != nil {
		return r, err
	}
	_, err = codec.CheckHeader(docIn, LUCENE41_DOC_CODEC, LUCENE41_VERSION_CURRENT, LUCENE41_VERSION_CURRENT)
	if err != nil {
		return r, err
	}
	forUtil, err := NewForUtil(docIn)
	if err != nil {
		return r, err
	}

	if fis.HasProx {
		posIn, err = dir.OpenInput(util.SegmentFileName(si.Name, segmentSuffix, LUCENE41_POS_EXTENSION), ctx)
		if err != nil {
			return r, err
		}
		_, err = codec.CheckHeader(posIn, LUCENE41_POS_CODEC, LUCENE41_VERSION_CURRENT, LUCENE41_VERSION_CURRENT)
		if err != nil {
			return r, err
		}

		if fis.HasPayloads || fis.HasOffsets {
			payIn, err = dir.OpenInput(util.SegmentFileName(si.Name, segmentSuffix, LUCENE41_PAY_EXTENSION), ctx)
			if err != nil {
				return r, err
			}
			_, err = codec.CheckHeader(payIn, LUCENE41_PAY_CODEC, LUCENE41_VERSION_CURRENT, LUCENE41_VERSION_CURRENT)
			if err != nil {
				return r, err
			}
		}
	}

	success = true
	return &Lucene41PostingsReader{docIn, posIn, payIn, forUtil}, nil
}

func (r *Lucene41PostingsReader) Init(termsIn store.IndexInput) error {
	log.Printf("Initializing from: %v", termsIn)
	// Make sure we are talking to the matching postings writer
	_, err := codec.CheckHeader(termsIn, LUCENE41_TERMS_CODEC, LUCENE41_VERSION_START, LUCENE41_VERSION_CURRENT)
	if err != nil {
		return err
	}
	indexBlockSize, err := termsIn.ReadVInt()
	if err != nil {
		return err
	}
	log.Printf("Index block size: %v", indexBlockSize)
	if indexBlockSize != LUCENE41_BLOCK_SIZE {
		panic(fmt.Sprintf("index-time BLOCK_SIZE (%v) != read-time BLOCK_SIZE (%v)", indexBlockSize, LUCENE41_BLOCK_SIZE))
	}
	return nil
}

/**
 * Read values that have been written using variable-length encoding instead of bit-packing.
 */
func readVIntBlock(docIn store.IndexInput, docBuffer []int,
	freqBuffer []int, num int, indexHasFreq bool) (err error) {
	if indexHasFreq {
		for i := 0; i < num; i++ {
			code, err := asInt(docIn.ReadVInt())
			if err != nil {
				return err
			}
			docBuffer[i] = int(uint(code) >> 1)
			if (code & 1) != 0 {
				freqBuffer[i] = 1
			} else {
				freqBuffer[i], err = asInt(docIn.ReadVInt())
				if err != nil {
					return err
				}
			}
		}
	} else {
		for i := 0; i < num; i++ {
			docBuffer[i], err = asInt(docIn.ReadVInt())
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func (r *Lucene41PostingsReader) NewTermState() *BlockTermState {
	return newIntBlockTermState().BlockTermState
}

func (r *Lucene41PostingsReader) Close() error {
	return util.Close(r.docIn, r.posIn, r.payIn)
}

/* Reads but does not decode the byte[] blob holding
   metadata for the current terms block */
func (r *Lucene41PostingsReader) ReadTermsBlock(termsIn store.IndexInput,
	fieldInfo model.FieldInfo, _termState *BlockTermState) (err error) {

	termState := _termState.Self.(*intBlockTermState)
	numBytes, err := asInt(termsIn.ReadVInt())
	if err != nil {
		return err
	}

	if termState.bytes == nil {
		// TODO over-allocate
		termState.bytes = make([]byte, numBytes)
		termState.bytesReader = store.NewEmptyByteArrayDataInput()
	} else if len(termState.bytes) < numBytes {
		// TODO over-allocate
		termState.bytes = make([]byte, numBytes)
	}

	err = termsIn.ReadBytes(termState.bytes)
	if err != nil {
		return err
	}
	termState.bytesReader.Reset(termState.bytes)
	return nil
}

func (r *Lucene41PostingsReader) nextTerm(fieldInfo model.FieldInfo,
	_termState *BlockTermState) (err error) {

	termState := _termState.Self.(*intBlockTermState)
	isFirstTerm := termState.termBlockOrd == 0
	fieldHasPositions := fieldInfo.IndexOptions() >= model.INDEX_OPT_DOCS_AND_FREQS_AND_POSITIONS
	fieldHasOffsets := fieldInfo.IndexOptions() >= model.INDEX_OPT_DOCS_AND_FREQS_AND_POSITIONS_AND_OFFSETS
	fieldHasPayloads := fieldInfo.HasPayloads()

	in := termState.bytesReader
	if isFirstTerm {
		if termState.docFreq == 1 {
			termState.singletonDocID, err = asInt(in.ReadVInt())
			if err != nil {
				return err
			}
			termState.docStartFP = 0
		} else {
			termState.singletonDocID = -1
			termState.docStartFP, err = in.ReadVLong()
			if err != nil {
				return err
			}
		}
		if fieldHasPositions {
			termState.posStartFP, err = in.ReadVLong()
			if err != nil {
				return err
			}
			if termState.totalTermFreq > LUCENE41_BLOCK_SIZE {
				termState.lastPosBlockOffset, err = in.ReadVLong()
				if err != nil {
					return err
				}
			} else {
				termState.lastPosBlockOffset = -1
			}
			if (fieldHasPayloads || fieldHasOffsets) && termState.totalTermFreq >= LUCENE41_BLOCK_SIZE {
				termState.payStartFP, err = in.ReadVLong()
				if err != nil {
					return err
				}
			} else {
				termState.payStartFP = -1
			}
		}
	} else {
		if termState.docFreq == 1 {
			termState.singletonDocID, err = asInt(in.ReadVInt())
			if err != nil {
				return err
			}
		} else {
			termState.singletonDocID = -1
			delta, err := in.ReadVLong()
			if err != nil {
				return err
			}
			termState.docStartFP += delta
		}
		if fieldHasPositions {
			delta, err := in.ReadVLong()
			if err != nil {
				return err
			}
			termState.posStartFP += delta
			if termState.totalTermFreq > LUCENE41_BLOCK_SIZE {
				termState.lastPosBlockOffset, err = in.ReadVLong()
				if err != nil {
					return err
				}
			} else {
				termState.lastPosBlockOffset = -1
			}
			if (fieldHasPayloads || fieldHasOffsets) && termState.totalTermFreq >= LUCENE41_BLOCK_SIZE {
				delta, err = in.ReadVLong()
				if err != nil {
					return err
				}
				if termState.payStartFP == -1 {
					termState.payStartFP = delta
				} else {
					termState.payStartFP += delta
				}
			}
		}
	}

	if termState.docFreq > LUCENE41_BLOCK_SIZE {
		termState.skipOffset, err = in.ReadVLong()
		if err != nil {
			return err
		}
	} else {
		termState.skipOffset = -1
	}

	return nil
}

func (r *Lucene41PostingsReader) docs(fieldInfo model.FieldInfo,
	termState *BlockTermState, liveDocs util.Bits,
	reuse DocsEnum, flags int) (de DocsEnum, err error) {

	var docsEnum *blockDocsEnum
	if v, ok := reuse.(*blockDocsEnum); ok {
		docsEnum = v
		if !docsEnum.canReuse(r.docIn, fieldInfo) {
			docsEnum = newBlockDocsEnum(r, fieldInfo)
		}
	} else {
		docsEnum = newBlockDocsEnum(r, fieldInfo)
	}
	return docsEnum.reset(liveDocs, termState.Self.(*intBlockTermState), flags)
}

type blockDocsEnum struct {
	*Lucene41PostingsReader // embedded struct

	encoded []byte

	docDeltaBuffer []int
	freqBuffer     []int

	docBufferUpto int

	// skipper Lucene41SkipReader
	skipped bool

	startDocIn store.IndexInput

	docIn            store.IndexInput
	indexHasFreq     bool
	indexHasPos      bool
	indexHasOffsets  bool
	indexHasPayloads bool

	docFreq       int
	totalTermFreq int64
	docUpto       int
	doc           int
	accum         int
	freq          int

	// Where this term's postings start in the .doc file:
	docTermStartFP int64

	// Where this term's skip data starts (after
	// docTermStartFP) in the .doc file (or -1 if there is
	// no skip data for this term):
	skipOffset int64

	// docID for next skip point, we won't use skipper if
	// target docID is not larger than this
	nextSkipDoc int

	liveDocs util.Bits

	needsFreq      bool
	singletonDocID int
}

func newBlockDocsEnum(owner *Lucene41PostingsReader,
	fieldInfo model.FieldInfo) *blockDocsEnum {

	return &blockDocsEnum{
		Lucene41PostingsReader: owner,
		docDeltaBuffer:         make([]int, MAX_DATA_SIZE),
		freqBuffer:             make([]int, MAX_DATA_SIZE),
		startDocIn:             owner.docIn,
		docIn:                  nil,
		indexHasFreq:           fieldInfo.IndexOptions() >= model.INDEX_OPT_DOCS_AND_FREQS,
		indexHasPos:            fieldInfo.IndexOptions() >= model.INDEX_OPT_DOCS_AND_FREQS_AND_POSITIONS,
		indexHasOffsets:        fieldInfo.IndexOptions() >= model.INDEX_OPT_DOCS_AND_FREQS_AND_POSITIONS,
		indexHasPayloads:       fieldInfo.HasPayloads(),
		encoded:                make([]byte, MAX_ENCODED_SIZE),
	}
}

func (de *blockDocsEnum) canReuse(docIn store.IndexInput, fieldInfo model.FieldInfo) bool {
	return docIn == de.startDocIn &&
		de.indexHasFreq == (fieldInfo.IndexOptions() >= model.INDEX_OPT_DOCS_AND_FREQS) &&
		de.indexHasPos == (fieldInfo.IndexOptions() >= model.INDEX_OPT_DOCS_AND_FREQS_AND_POSITIONS) &&
		de.indexHasPayloads == fieldInfo.HasPayloads()
}

func (de *blockDocsEnum) reset(liveDocs util.Bits, termState *intBlockTermState, flags int) (ret DocsEnum, err error) {
	de.liveDocs = liveDocs
	log.Printf("  FPR.reset: termState=%v", termState)
	de.docFreq = termState.docFreq
	if de.indexHasFreq {
		de.totalTermFreq = termState.totalTermFreq
	} else {
		de.totalTermFreq = int64(de.docFreq)
	}
	de.docTermStartFP = termState.docStartFP // <---- docTermStartFP should be 178 instead of 0
	de.skipOffset = termState.skipOffset
	de.singletonDocID = termState.singletonDocID
	if de.docFreq > 1 {
		if de.docIn == nil {
			// lazy init
			de.docIn = de.startDocIn.Clone()
		}
		err = de.docIn.Seek(de.docTermStartFP)
		if err != nil {
			return nil, err
		}
	}

	de.doc = -1
	de.needsFreq = (flags & DOCS_ENUM_FLAG_FREQS) != 0
	if !de.indexHasFreq {
		for i, _ := range de.freqBuffer {
			de.freqBuffer[i] = 1
		}
	}
	de.accum = 0
	de.docUpto = 0
	de.nextSkipDoc = LUCENE41_BLOCK_SIZE - 1 // we won't skip if target is found in first block
	de.docBufferUpto = LUCENE41_BLOCK_SIZE
	de.skipped = false
	return de, nil
}

func (de *blockDocsEnum) Freq() (n int, err error) {
	return de.freq, nil
}

func (de *blockDocsEnum) DocId() int {
	return de.doc
}

func (de *blockDocsEnum) refillDocs() (err error) {
	left := de.docFreq - de.docUpto
	assert(left > 0)

	if left >= LUCENE41_BLOCK_SIZE {
		log.Printf("    fill doc block from fp=%v", de.docIn.FilePointer())
		panic("not implemented yet")
	} else if de.docFreq == 1 {
		de.docDeltaBuffer[0] = de.singletonDocID
		de.freqBuffer[0] = int(de.totalTermFreq)
	} else {
		// Read vInts:
		log.Printf("    fill last vInt block from fp=%v", de.docIn.FilePointer())
		err = readVIntBlock(de.docIn, de.docDeltaBuffer, de.freqBuffer, left, de.indexHasFreq)
	}
	de.docBufferUpto = 0
	return
}

func (de *blockDocsEnum) NextDoc() (n int, err error) {
	log.Println("FPR.nextDoc")
	for {
		log.Printf("  docUpto=%v (of df=%v) docBufferUpto=%v", de.docUpto, de.docFreq, de.docBufferUpto)

		if de.docUpto == de.docFreq {
			log.Println("  return doc=END")
			de.doc = NO_MORE_DOCS
			return de.doc, nil
		}

		if de.docBufferUpto == LUCENE41_BLOCK_SIZE {
			err = de.refillDocs()
			if err != nil {
				return 0, err
			}
		}

		log.Printf("    accum=%v docDeltaBuffer[%v]=%v", de.accum, de.docBufferUpto, de.docDeltaBuffer[de.docBufferUpto])
		de.accum += de.docDeltaBuffer[de.docBufferUpto]
		de.docUpto++

		if de.liveDocs == nil || de.liveDocs.At(de.accum) {
			de.doc = de.accum
			de.freq = de.freqBuffer[de.docBufferUpto]
			de.docBufferUpto++
			log.Printf("  return doc=%v freq=%v", de.doc, de.freq)
			return de.doc, nil
		}
		log.Printf("  doc=%v is deleted; try next doc", de.accum)
		de.docBufferUpto++
	}
}

type intBlockTermState struct {
	*BlockTermState
	docStartFP         int64
	posStartFP         int64
	payStartFP         int64
	skipOffset         int64
	lastPosBlockOffset int64
	// docid when there is a single pulsed posting, otherwise -1
	// freq is always implicitly totalTermFreq in this case.
	singletonDocID int

	// Only used by the "primary" TermState -- clones don't
	// copy this (basically they are "transient"):
	bytesReader *store.ByteArrayDataInput
	bytes       []byte
}

func newIntBlockTermState() *intBlockTermState {
	ts := &intBlockTermState{}
	parent := NewBlockTermState()
	ts.BlockTermState, parent.Self = parent, ts
	return ts
}

func (ts *intBlockTermState) Clone() TermState {
	clone := newIntBlockTermState()
	clone.CopyFrom(ts)
	return clone
}

func (ts *intBlockTermState) CopyFrom(other TermState) {
	assert(other != nil)
	if ots, ok := other.(*intBlockTermState); ok {
		ts.BlockTermState.internalCopyFrom(ots.BlockTermState)
		ts.docStartFP = ots.docStartFP
		ts.posStartFP = ots.posStartFP
		ts.payStartFP = ots.payStartFP
		ts.lastPosBlockOffset = ots.lastPosBlockOffset
		ts.skipOffset = ots.skipOffset
		ts.singletonDocID = ots.singletonDocID

		// Do not copy bytes, bytesReader (else TermState is
		// very heavy, ie drags around the entire block's
		// byte[]).  On seek back, if next() is in fact used
		// (rare!), they will be re-read from disk.
	} else if ots, ok := other.(*BlockTermState); ok && ots.Self != nil {
		// try copy from other's sub class
		ts.CopyFrom(ots.Self)
	} else {
		panic(fmt.Sprintf("Can not copy from %v", reflect.TypeOf(other).Name()))
	}
}

func (ts *intBlockTermState) String() string {
	return fmt.Sprintf("%v docStartFP=%v posStartFP=%v payStartFP=%v lastPosBlockOffset=%v skipOffset=%v singletonDocID=%v",
		ts.BlockTermState.toString(), ts.docStartFP, ts.posStartFP, ts.payStartFP, ts.lastPosBlockOffset, ts.skipOffset, ts.singletonDocID)
}

// lucene41/Lucene41StoredFieldsFormat.java

/*
Lucene 4.1 stored fields format.

Principle

This StoredFieldsFormat compresses blocks of 16KB of documents in
order to improve the compression ratio compared to document-level
compression. It uses the LZ4 compression algorithm, which is fast to
compress and very fast to decompress dta. Although the compression
method that is used focuses more on speed than on compression ratio,
it should provide interesting compression ratios for redundant inputs
(such as log files, HTML or plain text).

File formats

Stored fields are represented by two files:

1. field_data

A fields data file (extension .fdt). This file stores a compact
representation of documents in compressed blocks of 16KB or more.
When writing a segment, documents are appended to an in-memory []byte
buffer. When its size reaches 16KB or more, some metadata about the
documents is flushed to disk, immediately followed by a compressed
representation of the buffer using the [LZ4](http://codec.google.com/p/lz4/)
[compression format](http://fastcompression.blogspot.ru/2011/05/lz4-explained.html)

Here is a more detailed description of the field data fiel format:

- FieldData (.dft) --> <Header>, packedIntsVersion, <Chunk>^ChunkCount
- Header --> CodecHeader
- PackedIntsVersion --> PackedInts.VERSION_CURRENT as a VInt
- ChunkCount is not known in advance and is the number of chunks
nucessary to store all document of the segment
- Chunk --> DocBase, ChunkDocs, DocFieldCounts, DocLengths, <CompressedDoc>
- DocBase --> the ID of the first document of the chunk as a VInt
- ChunkDocs --> the number of the documents in the chunk as a VInt
- DocFieldCounts --> the number of stored fields or every document
in the chunk,  encoded as followed:
  - if hunkDocs=1, the unique value is encoded as a VInt
  - else read VInt (let's call it bitsRequired)
    - if bitsRequired is 0 then all values are equal, and the common
    value is the following VInt
    - else bitsRequired is the number of bits required to store any
    value, and values are stored in a packed array where every value
    is stored on exactly bitsRequired bits
- DocLenghts --> the lengths of all documents in the chunk, encodedwith the same method as DocFieldCounts
- CompressedDocs --> a compressed representation of <Docs> using
the LZ4 compression format
- Docs --> <Doc>^ChunkDocs
- Doc --> <FieldNumAndType, Value>^DocFieldCount
- FieldNumAndType --> a VLong, whose 3 last bits are Type and other
bits are FieldNum
- Type -->
  - 0: Value is string
  - 1: Value is BinaryValue
  - 2: Value is int
  - 3: Value is float32
  - 4: Value is int64
  - 5: Value is float64
  - 6, 7: unused
- FieldNum --> an ID of the field
- Value --> string | BinaryValue | int | float32 | int64 | float64
dpending on Type
- BinaryValue --> ValueLength <Byte>&ValueLength

Notes

- If documents are larger than 16KB then chunks will likely contain
only one document. However, documents can never spread across several
chunks (all fields of a single document are in the same chunk).
- When at least one document in a chunk is large enough so that the
chunk is larger than 32KB, then chunk will actually be compressed in
several LZ4 blocks of 16KB. This allows StoredFieldsVisitors which
are only interested in the first fields of a document to not have to
decompress 10MB of data if the document is 10MB, but only 16KB.
- Given that the original lengths are written in the metadata of the
chunk, the decompressorcan leverage this information to stop decoding
as soon as enough data has been decompressed.
- In case documents are incompressible, CompressedDocs will be less
than 0.5% larger than Docs.

2. field_index

A fields index file (extension .fdx).

- FieldsIndex (.fdx) --> <Header>, <ChunkINdex>
- Header --> CodecHeader
- ChunkIndex: See CompressingStoredFieldsInexWriter

Known limitations

This StoredFieldsFormat does not support individual documents larger
than (2^32 - 2^14) bytes. In case this is a problem, you should use
another format, such as Lucene40StoredFieldsFormat.
*/
type Lucene41StoredFieldsFormat struct {
	*CompressingStoredFieldsFormat
}

func newLucene41StoredFieldsFormat() *Lucene41StoredFieldsFormat {
	return &Lucene41StoredFieldsFormat{
		newCompressingStoredFieldsFormat("Lucene41StoredFields", "", compressing.COMPRESSION_MODE_FAST, 1<<14),
	}
}

type Lucene41StoredFieldsReader struct {
	*CompressingStoredFieldsReader
}

func newLucene41StoredFieldsReader(d store.Directory,
	si *model.SegmentInfo, fn model.FieldInfos,
	ctx store.IOContext) (r StoredFieldsReader, err error) {

	formatName := "Lucene41StoredFields"
	compressionMode := compressing.COMPRESSION_MODE_FAST
	// chunkSize := 1 << 14
	p, err := newCompressingStoredFieldsReader(d, si, "", fn, ctx, formatName, compressionMode)
	if err == nil {
		r = &Lucene41StoredFieldsReader{p}
	}
	return r, nil
}

// codec/compressing/CompressingStoredFieldsReader.java

const (
	STRING         = 0
	BYTE_ARR       = 1
	NUMERIC_INT    = 2
	NUMERIC_FLOAT  = 3
	NUMERIC_LONG   = 4
	NUMERIC_DOUBLE = 5
)

var (
	TYPE_BITS = packed.BitsRequired(NUMERIC_DOUBLE)
	TYPE_MASK = int(packed.MaxValue(TYPE_BITS))
)

const (
	CSFR_VERSION_BIG_CHUNKS = 1

	// Do not reuse the decompression buffer when there is more than 32kb to decompress
	BUFFER_REUSE_THRESHOLD = 1 << 15
)

const (
	CODEC_SFX_IDX             = "Index"
	CODEC_SFX_DAT             = "Data"
	CODEC_SFX_VERSION_START   = 0
	CODEC_SFX_VERSION_CURRENT = CODEC_SFX_VERSION_START
)

// StoredFieldsReader impl for CompressingStoredFieldsFormat
type CompressingStoredFieldsReader struct {
	version           int
	fieldInfos        model.FieldInfos
	indexReader       *CompressingStoredFieldsIndexReader
	fieldsStream      store.IndexInput
	chunkSize         int
	packedIntsVersion int
	compressionMode   compressing.CompressionMode
	decompressor      compressing.Decompressor
	bytes             []byte
	numDocs           int
	closed            bool
}

// used by clone
func newCompressingStoredFieldsReaderFrom(reader *CompressingStoredFieldsReader) *CompressingStoredFieldsReader {
	return &CompressingStoredFieldsReader{
		version:           reader.version,
		fieldInfos:        reader.fieldInfos,
		fieldsStream:      reader.fieldsStream.Clone(),
		indexReader:       reader.indexReader.Clone(),
		chunkSize:         reader.chunkSize,
		packedIntsVersion: reader.packedIntsVersion,
		compressionMode:   reader.compressionMode,
		decompressor:      reader.compressionMode.NewDecompressor(),
		numDocs:           reader.numDocs,
		bytes:             make([]byte, len(reader.bytes)),
		closed:            false,
	}
}

// Sole constructor
func newCompressingStoredFieldsReader(d store.Directory,
	si *model.SegmentInfo, segmentSuffix string,
	fn model.FieldInfos, ctx store.IOContext, formatName string,
	compressionMode compressing.CompressionMode) (r *CompressingStoredFieldsReader, err error) {

	r = &CompressingStoredFieldsReader{}
	r.compressionMode = compressionMode
	segment := si.Name
	r.fieldInfos = fn
	r.numDocs = si.DocCount()

	var indexStream store.IndexInput
	success := false
	defer func() {
		if !success {
			log.Println("Failed to initialize CompressionStoredFieldsReader.")
			if err != nil {
				log.Print(err)
			}
			util.Close(r, indexStream)
		}
	}()

	// Load the index into memory
	indexStreamFN := util.SegmentFileName(segment, segmentSuffix, lucene40.FIELDS_INDEX_EXTENSION)
	indexStream, err = d.OpenInput(indexStreamFN, ctx)
	if err != nil {
		return nil, err
	}
	codecNameIdx := formatName + CODEC_SFX_IDX
	codec.CheckHeader(indexStream, codecNameIdx, CODEC_SFX_VERSION_START, CODEC_SFX_VERSION_CURRENT)
	if int64(codec.HeaderLength(codecNameIdx)) != indexStream.FilePointer() {
		panic("assert fail")
	}
	r.indexReader, err = newCompressingStoredFieldsIndexReader(indexStream, si)
	if err != nil {
		return nil, err
	}
	err = indexStream.Close()
	if err != nil {
		return nil, err
	}
	indexStream = nil

	// Open the data file and read metadata
	fieldsStreamFN := util.SegmentFileName(segment, segmentSuffix, lucene40.FIELDS_EXTENSION)
	r.fieldsStream, err = d.OpenInput(fieldsStreamFN, ctx)
	if err != nil {
		return nil, err
	}
	codecNameDat := formatName + CODEC_SFX_DAT
	codec.CheckHeader(r.fieldsStream, codecNameDat, CODEC_SFX_VERSION_START, CODEC_SFX_VERSION_CURRENT)
	if int64(codec.HeaderLength(codecNameDat)) != r.fieldsStream.FilePointer() {
		panic("assert fail")
	}

	n, err := r.fieldsStream.ReadVInt()
	if err != nil {
		return nil, err
	}
	r.packedIntsVersion = int(n)
	r.decompressor = compressionMode.NewDecompressor()
	r.bytes = make([]byte, 0)

	success = true
	return r, nil
}

func (r *CompressingStoredFieldsReader) ensureOpen() {
	assert2(!r.closed, "this FieldsReader is closed")
}

// Close the underlying IndexInputs
func (r *CompressingStoredFieldsReader) Close() (err error) {
	if !r.closed {
		if err = util.Close(r.fieldsStream); err == nil {
			r.closed = true
		}
	}
	return
}

func (r *CompressingStoredFieldsReader) readField(in util.DataInput,
	visitor StoredFieldVisitor, info model.FieldInfo, bits int) error {
	switch bits & TYPE_MASK {
	case BYTE_ARR:
		panic("not implemented yet")
	case STRING:
		length, err := asInt(in.ReadVInt())
		if err != nil {
			return err
		}
		data := make([]byte, length)
		err = in.ReadBytes(data)
		if err != nil {
			return err
		}
		visitor.stringField(info, string(data))
	case NUMERIC_INT:
		panic("not implemented yet")
	case NUMERIC_FLOAT:
		panic("not implemented yet")
	case NUMERIC_LONG:
		panic("not implemented yet")
	case NUMERIC_DOUBLE:
		panic("not implemented yet")
	default:
		panic(fmt.Sprintf("Unknown type flag: %x", bits))
	}
	return nil
}

func (r *CompressingStoredFieldsReader) visitDocument(docID int, visitor StoredFieldVisitor) error {
	err := r.fieldsStream.Seek(r.indexReader.startPointer(docID))
	if err != nil {
		return err
	}

	docBase, err := asInt(r.fieldsStream.ReadVInt())
	if err != nil {
		return err
	}
	chunkDocs, err := asInt(r.fieldsStream.ReadVInt())
	if err != nil {
		return err
	}
	if docID < docBase ||
		docID >= docBase+chunkDocs ||
		docBase+chunkDocs > r.numDocs {
		return errors.New(fmt.Sprintf(
			"Corrupted: docID=%v, docBase=%v, chunkDocs=%v, numDocs=%v (resource=%v)",
			docID, docBase, chunkDocs, r.numDocs, r.fieldsStream))
	}

	var numStoredFields, offset, length, totalLength int
	if chunkDocs == 1 {
		panic("not implemented yet")
	} else {
		bitsPerStoredFields, err := asInt(r.fieldsStream.ReadVInt())
		if err != nil {
			return err
		}
		if bitsPerStoredFields == 0 {
			numStoredFields, err = asInt(r.fieldsStream.ReadVInt())
			if err != nil {
				return err
			}
		} else if bitsPerStoredFields > 31 {
			return errors.New(fmt.Sprintf("bitsPerStoredFields=%v (resource=%v)",
				bitsPerStoredFields, r.fieldsStream))
		} else {
			panic("not implemented yet")
		}

		bitsPerLength, err := asInt(r.fieldsStream.ReadVInt())
		if err != nil {
			return err
		}
		if bitsPerLength == 0 {
			panic("not implemented yet")
		} else if bitsPerLength > 31 {
			return errors.New(fmt.Sprintf("bitsPerLength=%v (resource=%v)",
				bitsPerLength, r.fieldsStream))
		} else {
			it := packed.ReaderIteratorNoHeader(
				r.fieldsStream, packed.PackedFormat(packed.PACKED), r.packedIntsVersion,
				chunkDocs, bitsPerLength, 1)
			var n int64
			off := 0
			for i := 0; i < docID-docBase; i++ {
				if n, err = it.Next(); err != nil {
					return err
				}
				off += int(n)
			}
			offset = off
			if n, err = it.Next(); err != nil {
				return err
			}
			length = int(n)
			off += length
			for i := docID - docBase + 1; i < chunkDocs; i++ {
				if n, err = it.Next(); err != nil {
					return err
				}
				off += int(n)
			}
			totalLength = off
		}
	}

	if (length == 0) != (numStoredFields == 0) {
		return errors.New(fmt.Sprintf(
			"length=%v, numStoredFields=%v (resource=%v)",
			length, numStoredFields, r.fieldsStream))
	}
	if numStoredFields == 0 {
		// nothing to do
		return nil
	}

	var documentInput util.DataInput
	if r.version >= CSFR_VERSION_BIG_CHUNKS && totalLength >= 2*r.chunkSize {
		panic("not implemented yet")
	} else {
		var bytes []byte
		if totalLength <= BUFFER_REUSE_THRESHOLD {
			bytes = r.bytes
		} else {
			bytes = make([]byte, 0)
		}
		bytes, err = r.decompressor(r.fieldsStream, totalLength, offset, length, bytes)
		if err != nil {
			return err
		}
		assert(len(bytes) == length)
		documentInput = store.NewByteArrayDataInput(bytes)
	}

	for fieldIDX := 0; fieldIDX < numStoredFields; fieldIDX++ {
		infoAndBits, err := documentInput.ReadVLong()
		if err != nil {
			return err
		}
		fieldNumber := int(uint64(infoAndBits) >> uint64(TYPE_BITS))
		fieldInfo := r.fieldInfos.FieldInfoByNumber(fieldNumber)

		bits := int(infoAndBits & int64(TYPE_MASK))
		assertWithMessage(bits <= NUMERIC_DOUBLE, fmt.Sprintf("bits=%x", bits))

		status, err := visitor.needsField(fieldInfo)
		if err != nil {
			return err
		}
		switch status {
		case STORED_FIELD_VISITOR_STATUS_YES:
			r.readField(documentInput, visitor, fieldInfo, bits)
		case STORED_FIELD_VISITOR_STATUS_NO:
			panic("not implemented yet")
		case STORED_FIELD_VISITOR_STATUS_STOP:
			return nil
		}
	}

	return nil
}

func assertWithMessage(ok bool, msg string) {
	if !ok {
		panic(msg)
	}
}

func (r *CompressingStoredFieldsReader) Clone() StoredFieldsReader {
	r.ensureOpen()
	return newCompressingStoredFieldsReaderFrom(r)
}

// codec/compressing/CompressingStoredFieldsIndexReader.java

func moveLowOrderBitsToSign(n int64) int64 {
	return int64(uint64(n)>>1) ^ -(n & 1)
}

// Random-access reader for CompressingStoredFieldsIndexWriter
type CompressingStoredFieldsIndexReader struct {
	maxDoc              int
	docBases            []int
	startPointers       []int64
	avgChunkDocs        []int
	avgChunkSizes       []int64
	docBasesDeltas      []packed.PackedIntsReader
	startPointersDeltas []packed.PackedIntsReader
}

func newCompressingStoredFieldsIndexReader(fieldsIndexIn store.IndexInput,
	si *model.SegmentInfo) (r *CompressingStoredFieldsIndexReader, err error) {

	r = &CompressingStoredFieldsIndexReader{}
	r.maxDoc = si.DocCount()
	r.docBases = make([]int, 0, 16)
	r.startPointers = make([]int64, 0, 16)
	r.avgChunkDocs = make([]int, 0, 16)
	r.avgChunkSizes = make([]int64, 0, 16)
	r.docBasesDeltas = make([]packed.PackedIntsReader, 0, 16)
	r.startPointersDeltas = make([]packed.PackedIntsReader, 0, 16)

	packedIntsVersion, err := fieldsIndexIn.ReadVInt()
	if err != nil {
		return nil, err
	}

	for blockCount := 0; ; blockCount++ {
		numChunks, err := fieldsIndexIn.ReadVInt()
		if err != nil {
			return nil, err
		}
		if numChunks == 0 {
			break
		}

		{ // doc bases
			n, err := fieldsIndexIn.ReadVInt()
			if err != nil {
				return nil, err
			}
			r.docBases = append(r.docBases, int(n))
			n, err = fieldsIndexIn.ReadVInt()
			if err != nil {
				return nil, err
			}
			r.avgChunkDocs = append(r.avgChunkDocs, int(n))
			bitsPerDocBase, err := fieldsIndexIn.ReadVInt()
			if err != nil {
				return nil, err
			}
			if bitsPerDocBase > 32 {
				return nil, errors.New(fmt.Sprintf("Corrupted bitsPerDocBase (resource=%v)", fieldsIndexIn))
			}
			pr, err := packed.NewPackedReaderNoHeader(fieldsIndexIn, packed.PACKED, packedIntsVersion, numChunks, uint32(bitsPerDocBase))
			if err != nil {
				return nil, err
			}
			r.docBasesDeltas = append(r.docBasesDeltas, pr)
		}

		{ // start pointers
			n, err := fieldsIndexIn.ReadVLong()
			if err != nil {
				return nil, err
			}
			r.startPointers = append(r.startPointers, n)
			n, err = fieldsIndexIn.ReadVLong()
			if err != nil {
				return nil, err
			}
			r.avgChunkSizes = append(r.avgChunkSizes, n)
			bitsPerStartPointer, err := fieldsIndexIn.ReadVInt()
			if err != nil {
				return nil, err
			}
			if bitsPerStartPointer > 64 {
				return nil, errors.New(fmt.Sprintf("Corrupted bitsPerStartPonter (resource=%v)", fieldsIndexIn))
			}
			pr, err := packed.NewPackedReaderNoHeader(fieldsIndexIn, packed.PACKED, packedIntsVersion, numChunks, uint32(bitsPerStartPointer))
			if err != nil {
				return nil, err
			}
			r.startPointersDeltas = append(r.startPointersDeltas, pr)
		}
	}

	return r, nil
}

func (r *CompressingStoredFieldsIndexReader) block(docID int) int {
	lo, hi := 0, len(r.docBases)-1
	for lo <= hi {
		mid := int(uint(lo+hi) >> 1)
		midValue := r.docBases[mid]
		if midValue == docID {
			return mid
		} else if midValue < docID {
			lo = mid + 1
		} else {
			hi = mid - 1
		}
	}
	return hi
}

func (r *CompressingStoredFieldsIndexReader) relativeDocBase(block, relativeChunk int) int {
	expected := r.avgChunkDocs[block] * relativeChunk
	delta := moveLowOrderBitsToSign(r.docBasesDeltas[block].Get(relativeChunk))
	return expected + int(delta)
}

func (r *CompressingStoredFieldsIndexReader) relativeStartPointer(block, relativeChunk int) int64 {
	expected := r.avgChunkSizes[block] * int64(relativeChunk)
	delta := moveLowOrderBitsToSign(r.startPointersDeltas[block].Get(relativeChunk))
	return expected + delta
}

func (r *CompressingStoredFieldsIndexReader) relativeChunk(block, relativeDoc int) int {
	lo, hi := 0, int(r.docBasesDeltas[block].Size())-1
	for lo <= hi {
		mid := int(uint(lo+hi) >> 1)
		midValue := r.relativeDocBase(block, mid)
		if midValue == relativeDoc {
			return mid
		} else if midValue < relativeDoc {
			lo = mid + 1
		} else {
			hi = mid - 1
		}
	}
	return hi
}

func (r *CompressingStoredFieldsIndexReader) startPointer(docID int) int64 {
	if docID < 0 || docID >= r.maxDoc {
		panic(fmt.Sprintf("docID out of range [0-%v]: %v", r.maxDoc, docID))
	}
	block := r.block(docID)
	relativeChunk := r.relativeChunk(block, docID-r.docBases[block])
	return r.startPointers[block] + r.relativeStartPointer(block, relativeChunk)
}

func (r *CompressingStoredFieldsIndexReader) Clone() *CompressingStoredFieldsIndexReader {
	return r
}
