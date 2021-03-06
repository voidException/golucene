package index

import (
	"github.com/balzaczyy/golucene/core/util/packed"
	"math"
)

// codecs/lucene41/ForUtil.java

const (
	/**
	 * Special number of bits per value used whenever all values to encode are equal.
	 */
	ALL_VALUES_EQUAL = 0
	/**
	 * Upper limit of the number of bytes that might be required to stored
	 * <code>BLOCK_SIZE</code> encoded values.
	 */
	MAX_ENCODED_SIZE = LUCENE41_BLOCK_SIZE * 4
)

/**
 * Upper limit of the number of values that might be decoded in a single call to
 * {@link #readBlock(IndexInput, byte[], int[])}. Although values after
 * <code>BLOCK_SIZE</code> are garbage, it is necessary to allocate value buffers
 * whose size is >= MAX_DATA_SIZE to avoid {@link ArrayIndexOutOfBoundsException}s.
 */
var MAX_DATA_SIZE int = computeMaxDataSize()

func computeMaxDataSize() int {
	maxDataSize := 0
	// for each version
	for version := packed.PACKED_VERSION_START; version <= packed.PACKED_VERSION_CURRENT; version++ {
		// for each packed format
		for format := packed.PACKED; format <= packed.PACKED_SINGLE_BLOCK; format++ {
			// for each bit-per-value
			for bpv := uint32(1); bpv <= 32; bpv++ {
				if !packed.PackedFormat(format).IsSupported(bpv) {
					continue
				}
				decoder := packed.GetPackedIntsDecoder(packed.PackedFormat(format), int32(version), bpv)
				iterations := int(computeIterations(decoder))
				if n := iterations * decoder.ByteValueCount(); n > maxDataSize {
					maxDataSize = n
				}
			}
		}
	}
	return maxDataSize
}

/**
 * Encode all values in normal area with fixed bit width,
 * which is determined by the max value in this block.
 */
type ForUtil struct {
	encodedSizes []int32
	encoders     []packed.PackedIntsEncoder
	decoders     []packed.PackedIntsDecoder
	iterations   []int32
}

type DataInput interface {
	ReadVInt() (n int32, err error)
}

func NewForUtil(in DataInput) (fu ForUtil, err error) {
	self := ForUtil{}
	packedIntsVersion, err := in.ReadVInt()
	if err != nil {
		return self, err
	}
	packed.CheckVersion(packedIntsVersion)
	self.encodedSizes = make([]int32, 33)
	self.encoders = make([]packed.PackedIntsEncoder, 33)
	self.decoders = make([]packed.PackedIntsDecoder, 33)
	self.iterations = make([]int32, 33)

	for bpv := 1; bpv <= 32; bpv++ {
		code, err := in.ReadVInt()
		if err != nil {
			return self, err
		}
		formatId := uint32(code) >> 5
		bitsPerValue := (uint32(code) & 31) + 1

		format := packed.PackedFormat(formatId)
		// assert format.isSupported(bitsPerValue)
		self.encodedSizes[bpv] = encodedSize(format, packedIntsVersion, bitsPerValue)
		self.encoders[bpv] = packed.GetPackedIntsEncoder(format, packedIntsVersion, bitsPerValue)
		self.decoders[bpv] = packed.GetPackedIntsDecoder(format, packedIntsVersion, bitsPerValue)
		self.iterations[bpv] = computeIterations(self.decoders[bpv])
	}
	return self, nil
}

func encodedSize(format packed.PackedFormat, packedIntsVersion int32, bitsPerValue uint32) int32 {
	byteCount := format.ByteCount(packedIntsVersion, LUCENE41_BLOCK_SIZE, bitsPerValue)
	// assert byteCount >= 0 && byteCount <= math.MaxInt32()
	return int32(byteCount)
}

func computeIterations(decoder packed.PackedIntsDecoder) int32 {
	return int32(math.Ceil(float64(LUCENE41_BLOCK_SIZE) / float64(decoder.ByteValueCount())))
}
