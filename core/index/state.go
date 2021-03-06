package index

import (
	"github.com/balzaczyy/golucene/core/index/model"
	"github.com/balzaczyy/golucene/core/store"
	"github.com/balzaczyy/golucene/core/util"
)

// index/SegmentReadState.java

type SegmentReadState struct {
	dir               store.Directory
	segmentInfo       *model.SegmentInfo
	fieldInfos        model.FieldInfos
	context           store.IOContext
	termsIndexDivisor int
	segmentSuffix     string
}

func newSegmentReadState(dir store.Directory,
	info *model.SegmentInfo, fieldInfos model.FieldInfos,
	context store.IOContext, termsIndexDivisor int) SegmentReadState {

	return SegmentReadState{dir, info, fieldInfos, context, termsIndexDivisor, ""}
}

// index/SegmentWriteState.java

/* Holder class for common parameters used during write. */
type SegmentWriteState struct {
	infoStream        util.InfoStream
	directory         store.Directory
	segmentInfo       *model.SegmentInfo
	fieldInfos        model.FieldInfos
	delCountOnFlush   int
	segDeletes        *BufferedDeletes
	liveDocs          util.MutableBits
	segmentSuffix     string
	termIndexInternal int
	context           store.IOContext
}

func newSegmentWriteState(infoStream util.InfoStream,
	dir store.Directory, segmentInfo *model.SegmentInfo,
	fieldInfos model.FieldInfos, termIndexInterval int,
	segDeletes *BufferedDeletes, ctx store.IOContext) SegmentWriteState {

	return SegmentWriteState{
		infoStream, dir, segmentInfo, fieldInfos, 0, segDeletes, nil, "",
		termIndexInterval, ctx}
}
