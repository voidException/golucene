package index

import (
	"container/list"
	"github.com/balzaczyy/golucene/core/util"
	"sync"
)

type MergeControl struct {
	sync.Locker
	infoStream util.InfoStream

	readerPool *ReaderPool

	// Holds all SegmentInfo instances currently involved in merges
	mergingSegments map[*SegmentInfoPerCommit]bool

	pendingMerges *list.List
	runningMerges map[*OneMerge]bool
	mergeSignal   *sync.Cond

	stopMerges bool
}

func newMergeControl(infoStream util.InfoStream, readerPool *ReaderPool) *MergeControl {
	return &MergeControl{
		Locker:          &sync.Mutex{},
		infoStream:      infoStream,
		readerPool:      readerPool,
		mergingSegments: make(map[*SegmentInfoPerCommit]bool),
		pendingMerges:   list.New(),
		runningMerges:   make(map[*OneMerge]bool),
	}
}

// L2183
func (mc *MergeControl) abortAllMerges() {
	mc.Lock() // synchronized
	defer mc.Unlock()

	mc.stopMerges = true

	// Abort all pending & running merges:
	for e := mc.pendingMerges.Front(); e != nil; e = e.Next() {
		merge := e.Value.(*OneMerge)
		if mc.infoStream.IsEnabled("IW") {
			mc.infoStream.Message("IW", "now abort pending merge %v",
				mc.readerPool.segmentsToString(merge.segments))
		}
		merge.abort()
		mc.mergeFinish(merge)
	}
	mc.pendingMerges.Init()

	for merge, _ := range mc.runningMerges {
		if mc.infoStream.IsEnabled("IW") {
			mc.infoStream.Message("IW", "now abort running merge %v",
				mc.readerPool.segmentsToString(merge.segments))
		}
		merge.abort()
	}

	// These merges periodically check whether they have
	// been aborted, and stop if so.  We wait here to make
	// sure they all stop.  It should not take very long
	// because the merge threads periodically check if
	// they are aborted.
	for len(mc.runningMerges) > 0 {
		if mc.infoStream.IsEnabled("IW") {
			mc.infoStream.Message("IW", "now wait for %v running merge(s) to abort",
				len(mc.runningMerges))
		}
		mc.mergeSignal.Wait()
	}

	mc.stopMerges = false

	assert(len(mc.mergingSegments) == 0)

	if mc.infoStream.IsEnabled("IW") {
		mc.infoStream.Message("IW", "all running merges have aborted")
	}
}

// L2242
/*
Wait for any currently outstanding merges to finish.

It is guaranteed that any merges started prior to calling this method
will have completed once this method completes.
*/
func (mc *MergeControl) waitForMerges() {
	mc.Lock() // synchronized
	defer mc.Unlock()
	// ensureOpen(false)

	if mc.infoStream.IsEnabled("IW") {
		mc.infoStream.Message("IW", "waitForMerges")
	}

	for mc.pendingMerges.Len() > 0 || len(mc.runningMerges) > 0 {
		mc.mergeSignal.Wait()
	}

	assert(len(mc.mergingSegments) == 0)

	if mc.infoStream.IsEnabled("IW") {
		mc.infoStream.Message("IW", "waitForMerges done")
	}
}

// L3696
/*
Does finishing for a merge, which is fast but holds the synchronized
lock on MergeControl instance.

Note: it must be externally synchronized or used internally.
*/
func (mc *MergeControl) mergeFinish(merge *OneMerge) {
	// forceMerge, addIndexes or abortAllmerges may be waiting on
	// merges to finish
	// notifyAll()

	// It's possible we are called twice, eg if there was an error
	// inside mergeInit()
	if merge.registerDone {
		for _, info := range merge.segments {
			delete(mc.mergingSegments, info)
		}
		merge.registerDone = false
	}

	delete(mc.runningMerges, merge)
	if len(mc.runningMerges) == 0 {
		mc.mergeSignal.Signal()
	}
}
