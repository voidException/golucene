package index

import (
	"fmt"
	"github.com/balzaczyy/golucene/store"
)

const (
	FST_FILE_FORMAT_NAME    = "FST"
	FST_VERSION_PACKED      = 3
	FST_VERSION_VINT_TARGET = 4

	FST_DEFAULT_MAX_BLOCK_BITS = 28 // 30 for 64 bit int
)

type InputType int

const (
	INPUT_TYPE_BYTE1 = 1
	INPUT_TYPE_BYTE2 = 2
	INPUT_TYPE_BYTE4 = 3
)

type FST struct {
	inputType          InputType
	bytes              *BytesStore
	startNode          int64
	outputs            Outputs
	NO_OUTPUT          interface{}
	nodeCount          int64
	arcCount           int64
	arcWithOutputCount int64
	packed             bool
	nodeRefToAddress   PackedReader
	version            int
	emptyOutput        interface{}
}

func loadFST(in *store.DataInput, outputs Outputs) (fst FST, err error) {
	return loadFST3(in, outputs, FST_DEFAULT_MAX_BLOCK_BITS)
}

func loadFST3(in *store.DataInput, outputs Outputs, maxBlockBits uint32) (fst FST, err error) {
	fst = FST{outputs: outputs, startNode: -1}

	if maxBlockBits < 1 || maxBlockBits > 30 {
		panic(fmt.Sprintf("maxBlockBits should 1..30; got %v", maxBlockBits))
	}

	// NOTE: only reads most recent format; we don't have
	// back-compat promise for FSTs (they are experimental):
	fst.version, err = store.CheckHeader(in, FST_FILE_FORMAT_NAME, FST_VERSION_PACKED, FST_VERSION_VINT_TARGET)
	if err != nil {
		return fst, err
	}
	defer store.CatchIOError(&err)
	fst.packed = (in.ReadByte() == 1)
	if in.ReadByte() == 1 {
		// accepts empty string
		// 1 KB blocks:
		emptyBytes := newBytesStoreFromBits(10)
		numBytes, err := in.ReadVInt()
		if err != nil {
			return fst, err
		}
		emptyBytes.CopyBytes(in, int64(numBytes))

		// De-serialize empty-string output:
		var reader *BytesReader
		if fst.packed {
			reader = emptyBytes.forwardReader()
		} else {
			reader = emptyBytes.reverseReader()
			// NoOutputs uses 0 bytes when writing its output,
			// so we have to check here else BytesStore gets
			// angry:
			if numBytes > 0 {
				reader.setPosition(int64(numBytes - 1))
			}
		}
		fst.emptyOutput = outputs.readFinalOutput(reader.DataInput)
	} // else emptyOutput = nil
	t := in.ReadByte()
	switch t {
	case 0:
		fst.inputType = INPUT_TYPE_BYTE1
	case 1:
		fst.inputType = INPUT_TYPE_BYTE2
	case 2:
		fst.inputType = INPUT_TYPE_BYTE4
	default:
		panic(fmt.Sprintf("invalid input type %v", t))
	}
	if fst.packed {
		fst.nodeRefToAddress, err = newPackedReader(in)
		if err != nil {
			return fst, err
		}
	} // else nodeRefToAddress = nil
	fst.startNode, err = in.ReadVLong()
	if err != nil {
		return fst, err
	}
	fst.nodeCount, err = in.ReadVLong()
	if err != nil {
		return fst, err
	}
	fst.arcCount, err = in.ReadVLong()
	if err != nil {
		return fst, err
	}
	fst.arcWithOutputCount, err = in.ReadVLong()
	if err != nil {
		return fst, err
	}

	numBytes, err := in.ReadVLong()
	if err != nil {
		return fst, err
	}
	fst.bytes, err = newBytesStoreFromInput(in, numBytes, 1<<maxBlockBits)
	if err != nil {
		return fst, err
	}

	fst.NO_OUTPUT = outputs.noOutput()

	fst.cacheRootArcs()

	// NOTE: bogus because this is only used during
	// building; we need to break out mutable FST from
	// immutable
	fst.allowArrayArcs = false
}

type Outputs interface {
	read(in *store.DataInput) interface{}
	readFinalOutput(in *store.DataInput) interface{}
}

type abstractOutputs struct {
	Outputs
}

func (out *abstractOutputs) readFinalOutput(in *store.DataInput) interface{} {
	return out.Outputs.read(in)
}

type BytesReader struct {
	*store.DataInput
	getPosition func() int64
	setPosition func(pos int64)
	reversed    func() bool
	skipBytes   func(count int32)
}