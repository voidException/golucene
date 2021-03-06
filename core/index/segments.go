package index

import (
	"fmt"
	"github.com/balzaczyy/golucene/core/index/model"
	"github.com/balzaczyy/golucene/core/store"
	"github.com/balzaczyy/golucene/core/util"
	"log"
	"sync/atomic"
)

// index/SegmentReader.java

/**
 * IndexReader implementation over a single segment.
 * <p>
 * Instances pointing to the same segment (but with different deletes, etc)
 * may share the same core data.
 * @lucene.experimental
 */
type SegmentReader struct {
	*AtomicReaderImpl
	si       *SegmentInfoPerCommit
	liveDocs util.Bits
	// Normally set to si.docCount - si.delDocCount, unless we
	// were created as an NRT reader from IW, in which case IW
	// tells us the docCount:
	numDocs int
	core    SegmentCoreReaders
}

/**
 * Constructs a new SegmentReader with a new core.
 * @throws CorruptIndexException if the index is corrupt
 * @throws IOException if there is a low-level IO error
 */
// TODO: why is this public?
func NewSegmentReader(si *SegmentInfoPerCommit, termInfosIndexDivisor int, context store.IOContext) (r *SegmentReader, err error) {
	log.Print("Initializing SegmentReader...")
	r = &SegmentReader{}
	log.Print("Obtaining AtomicReader...")
	r.AtomicReaderImpl = newAtomicReader(r)
	r.ARFieldsReader = r
	r.si = si
	log.Print("Obtaining SegmentCoreReaders...")
	r.core, err = newSegmentCoreReaders(r, si.info.Dir, si, context, termInfosIndexDivisor)
	if err != nil {
		return r, err
	}
	success := false
	defer func() {
		// With lock-less commits, it's entirely possible (and
		// fine) to hit a FileNotFound exception above.  In
		// this case, we want to explicitly close any subset
		// of things that were opened so that we don't have to
		// wait for a GC to do so.
		if !success {
			log.Printf("Failed to initialize SegmentReader.")
			if err != nil {
				log.Print(err)
			}
			r.core.decRef()
		}
	}()

	if si.HasDeletions() {
		panic("not supported yet")
	} else {
		// assert si.getDelCount() == 0
		// r.liveDocs = nil
	}
	r.numDocs = si.info.DocCount() - si.delCount
	success = true
	return r, nil
}

func (r *SegmentReader) LiveDocs() util.Bits {
	r.ensureOpen()
	return r.liveDocs
}

func (r *SegmentReader) doClose() error {
	r.core.decRef()
	return nil
}

func (r *SegmentReader) FieldInfos() model.FieldInfos {
	r.ensureOpen()
	return r.core.fieldInfos
}

// Expert: retrieve thread-private StoredFieldsReader
func (r *SegmentReader) FieldsReader() StoredFieldsReader {
	r.ensureOpen()
	return r.core.fieldsReaderLocal()
}

func (r *SegmentReader) VisitDocument(docID int, visitor StoredFieldVisitor) error {
	r.checkBounds(docID)
	return r.FieldsReader().visitDocument(docID, visitor)
}

func (r *SegmentReader) Fields() Fields {
	r.ensureOpen()
	return r.core.fields
}

func (r *SegmentReader) NumDocs() int {
	// Don't call ensureOpen() here (it could affect performance)
	return r.numDocs
}

func (r *SegmentReader) MaxDoc() int {
	// Don't call ensureOpen() here (it could affect performance)
	return r.si.info.DocCount()
}

func (r *SegmentReader) TermVectorsReader() TermVectorsReader {
	panic("not implemented yet")
}

func (r *SegmentReader) TermVectors(docID int) (fs Fields, err error) {
	panic("not implemented yet")
}

func (r *SegmentReader) checkBounds(docID int) {
	if docID < 0 || docID >= r.MaxDoc() {
		panic(fmt.Sprintf("docID must be >= 0 and < maxDoc=%v (got docID=%v)", r.MaxDoc(), docID))
	}
}

// SegmentReader.java L179
func (r *SegmentReader) String() string {
	// SegmentInfo.toString takes dir and number of
	// *pending* deletions; so we reverse compute that here:
	return r.si.StringOf(r.si.info.Dir, r.si.info.DocCount()-r.numDocs-r.si.delCount)
}

func (r *SegmentReader) SegmentName() string {
	return r.si.info.Name
}

func (r *SegmentReader) SegmentInfos() *SegmentInfoPerCommit {
	return r.si
}

func (r *SegmentReader) Directory() store.Directory {
	// Don't ensureOpen here -- in certain cases, when a
	// cloned/reopened reader needs to commit, it may call
	// this method on the closed original reader
	return r.si.info.Dir
}

func (r *SegmentReader) CoreCacheKey() interface{} {
	return r.core
}

func (r *SegmentReader) CombinedCoreAndDeletesKey() interface{} {
	return r
}

func (r *SegmentReader) TermInfosIndexDivisor() int {
	return r.core.termsIndexDivisor
}

func (r *SegmentReader) NumericDocValues(field string) (v NumericDocValues, err error) {
	r.ensureOpen()
	panic("not implemented yet")
}

func (r *SegmentReader) BinaryDocValues(field string) (v BinaryDocValues, err error) {
	r.ensureOpen()
	panic("not implemented yet")
}

func (r *SegmentReader) SortedDocValues(field string) (v SortedDocValues, err error) {
	r.ensureOpen()
	panic("not implemented yet")
}

func (r *SegmentReader) SortedSetDocValues(field string) (v SortedSetDocValues, err error) {
	r.ensureOpen()
	panic("not implemented yet")
}

func (r *SegmentReader) NormValues(field string) (v NumericDocValues, err error) {
	r.ensureOpen()
	return r.core.normValues(field)
}

type CoreClosedListener interface {
	onClose(r *SegmentReader)
}

type SegmentCoreReaders struct {
	refCount int32 // synchronized

	fieldInfos model.FieldInfos

	fields        FieldsProducer
	dvProducer    DocValuesProducer
	normsProducer DocValuesProducer

	termsIndexDivisor int

	owner *SegmentReader

	fieldsReaderOrig      StoredFieldsReader
	termVectorsReaderOrig TermVectorsReader
	cfsReader             *store.CompoundFileDirectory

	/*
	 Lucene Java use ThreadLocal to serve as thread-level cache, to avoid
	 expensive read actions while limit memory consumption. Since Go doesn't
	 have thread or routine Local, a new object is always returned.

	 TODO redesign when ported to goroutines
	*/
	fieldsReaderLocal func() StoredFieldsReader
	normsLocal        func() map[string]interface{}

	addListener    chan CoreClosedListener
	removeListener chan CoreClosedListener
	notifyListener chan *SegmentReader
}

func newSegmentCoreReaders(owner *SegmentReader, dir store.Directory, si *SegmentInfoPerCommit,
	context store.IOContext, termsIndexDivisor int) (self SegmentCoreReaders, err error) {
	if termsIndexDivisor == 0 {
		panic("indexDivisor must be < 0 (don't load terms index) or greater than 0 (got 0)")
	}
	log.Printf("Initializing SegmentCoreReaders from directory: %v", dir)

	self = SegmentCoreReaders{
		refCount: 1,
		normsLocal: func() map[string]interface{} {
			return make(map[string]interface{})
		},
	}
	self.fieldsReaderLocal = func() StoredFieldsReader {
		return self.fieldsReaderOrig.Clone()
	}

	log.Print("Initializing listeners...")
	self.addListener = make(chan CoreClosedListener)
	self.removeListener = make(chan CoreClosedListener)
	self.notifyListener = make(chan *SegmentReader)
	// TODO re-enable later
	go func() { // ensure listners are synchronized
		coreClosedListeners := make([]CoreClosedListener, 0)
		isRunning := true
		var listener CoreClosedListener
		for isRunning {
			log.Print("Listening for events...")
			select {
			case listener = <-self.addListener:
				coreClosedListeners = append(coreClosedListeners, listener)
			case listener = <-self.removeListener:
				n := len(coreClosedListeners)
				for i, v := range coreClosedListeners {
					if v == listener {
						newListeners := make([]CoreClosedListener, 0, n-1)
						newListeners = append(newListeners, coreClosedListeners[0:i]...)
						newListeners = append(newListeners, coreClosedListeners[i+1:]...)
						coreClosedListeners = newListeners
						break
					}
				}
			case owner := <-self.notifyListener:
				log.Print("Shutting down SegmentCoreReaders...")
				isRunning = false
				for _, v := range coreClosedListeners {
					v.onClose(owner)
				}
			}
		}
		log.Print("Listeners are done.")
	}()

	success := false
	defer func() {
		if !success {
			log.Print("Failed to initialize SegmentCoreReaders.")
			self.decRef()
		}
	}()

	codec := si.info.Codec().(Codec)
	log.Print("Obtaining CFS Directory...")
	var cfsDir store.Directory // confusing name: if (cfs) its the cfsdir, otherwise its the segment's directory.
	if si.info.IsCompoundFile() {
		log.Print("Detected CompoundFile.")
		name := util.SegmentFileName(si.info.Name, "", store.COMPOUND_FILE_EXTENSION)
		self.cfsReader, err = store.NewCompoundFileDirectory(dir, name, context, false)
		if err != nil {
			return self, err
		}
		log.Printf("CompoundFileDirectory: %v", self.cfsReader)
		cfsDir = self.cfsReader
	} else {
		cfsDir = dir
	}
	log.Printf("CFS Directory: %v", cfsDir)
	log.Print("Reading FieldInfos...")
	self.fieldInfos, err = codec.FieldInfosFormat().FieldInfosReader()(cfsDir, si.info.Name, store.IO_CONTEXT_READONCE)
	if err != nil {
		return self, err
	}

	self.termsIndexDivisor = termsIndexDivisor
	format := codec.PostingsFormat()

	log.Print("Obtaining SegmentReadState...")
	segmentReadState := newSegmentReadState(cfsDir, si.info, self.fieldInfos, context, termsIndexDivisor)
	// Ask codec for its Fields
	log.Print("Obtaining FieldsProducer...")
	self.fields, err = format.FieldsProducer(segmentReadState)
	if err != nil {
		return self, err
	}
	assert(self.fields != nil)
	// ask codec for its Norms:
	// TODO: since we don't write any norms file if there are no norms,
	// kinda jaky to assume the codec handles the case of no norms file at all gracefully?!

	if self.fieldInfos.HasDocValues {
		log.Print("Obtaining DocValuesProducer...")
		self.dvProducer, err = codec.DocValuesFormat().FieldsProducer(segmentReadState)
		if err != nil {
			return self, err
		}
		assert(self.dvProducer != nil)
	} else {
		// self.dvProducer = nil
	}

	if self.fieldInfos.HasNorms {
		log.Print("Obtaining NormsDocValuesProducer...")
		self.normsProducer, err = codec.NormsFormat().NormsProducer(segmentReadState)
		if err != nil {
			return self, err
		}
		assert(self.normsProducer != nil)
	} else {
		// self.normsProducer = nil
	}

	log.Print("Obtaining StoredFieldsReader...")
	self.fieldsReaderOrig, err = si.info.Codec().(Codec).StoredFieldsFormat().FieldsReader(cfsDir, si.info, self.fieldInfos, context)
	if err != nil {
		return self, err
	}

	if self.fieldInfos.HasVectors { // open term vector files only as needed
		log.Print("Obtaining TermVectorsReader...")
		self.termVectorsReaderOrig, err = si.info.Codec().(Codec).TermVectorsFormat().VectorsReader(cfsDir, si.info, self.fieldInfos, context)
		if err != nil {
			return self, err
		}
	} else {
		// self.termVectorsReaderOrig = nil
	}

	log.Print("Success")
	success = true

	// Must assign this at the end -- if we hit an
	// exception above core, we don't want to attempt to
	// purge the FieldCache (will hit NPE because core is
	// not assigned yet).
	self.owner = owner
	return self, nil
}

func (r *SegmentCoreReaders) normValues(field string) (ndv NumericDocValues, err error) {
	if fi := r.fieldInfos.FieldInfoByName(field); fi.Name != "" {
		if fi.HasNorms() {
			assert(r.normsProducer != nil)

			if norms, ok := r.normsLocal()[field]; ok {
				ndv = norms.(NumericDocValues)
			} else if ndv, err = r.normsProducer.Numeric(fi); err == nil {
				r.normsLocal()[field] = norms
			}
		}
	} // else Field does not exist
	return
}

func (r *SegmentCoreReaders) decRef() {
	if atomic.AddInt32(&r.refCount, -1) == 0 {
		util.Close( /*self.termVectorsLocal, self.fieldsReaderLocal, docValuesLocal, r.normsLocal,*/
			r.fields, r.dvProducer, r.termVectorsReaderOrig, r.fieldsReaderOrig,
			r.cfsReader, r.normsProducer)
		r.notifyListener <- r.owner
	}
}

// type NumericDocValues interface {
// 	Value(docID int) int64
// }
type NumericDocValues func(docID int) int64

type BinaryDocValues interface {
	value(docID int, result []byte)
}
type SortedDocValues interface {
	BinaryDocValues
	ord(docID int) int
	lookupOrd(ord int, result []byte)
	valueCount() int
}
type SortedSetDocValues interface {
	nextOrd() int64
	setDocument(docID int)
	lookupOrd(ord int64, result []byte)
	valueCount() int64
}

type StoredFieldVisitor interface {
	binaryField(fi model.FieldInfo, value []byte) error
	stringField(fi model.FieldInfo, value string) error
	intField(fi model.FieldInfo, value int) error
	longField(fi model.FieldInfo, value int64) error
	floatField(fi model.FieldInfo, value float32) error
	doubleField(fi model.FieldInfo, value float64) error
	needsField(fi model.FieldInfo) (StoredFieldVisitorStatus, error)
}

type StoredFieldVisitorAdapter struct{}

func (va *StoredFieldVisitorAdapter) binaryField(fi model.FieldInfo, value []byte) error  { return nil }
func (va *StoredFieldVisitorAdapter) stringField(fi model.FieldInfo, value string) error  { return nil }
func (va *StoredFieldVisitorAdapter) intField(fi model.FieldInfo, value int) error        { return nil }
func (va *StoredFieldVisitorAdapter) longField(fi model.FieldInfo, value int64) error     { return nil }
func (va *StoredFieldVisitorAdapter) floatField(fi model.FieldInfo, value float32) error  { return nil }
func (va *StoredFieldVisitorAdapter) doubleField(fi model.FieldInfo, value float64) error { return nil }

type StoredFieldVisitorStatus int

const (
	STORED_FIELD_VISITOR_STATUS_YES  = StoredFieldVisitorStatus(1)
	STORED_FIELD_VISITOR_STATUS_NO   = StoredFieldVisitorStatus(2)
	STORED_FIELD_VISITOR_STATUS_STOP = StoredFieldVisitorStatus(3)
)
