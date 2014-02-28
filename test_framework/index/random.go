package index

import (
	. "github.com/balzaczyy/golucene/core/index"
	"math/rand"
)

// index/MockRandomMergePolicy.java

// MergePolicy that makes random decisions for testing.
type MockRandomMergePolicy struct {
	*MergePolicyImpl
	random *rand.Rand
}

func NewMockRandomMergePolicy(r *rand.Rand) *MockRandomMergePolicy {
	// fork a private random, since we are called unpredicatably from threads:
	res := &MockRandomMergePolicy{
		random: rand.New(rand.NewSource(r.Int63())),
	}
	res.MergePolicyImpl = NewDefaultMergePolicyImpl(res)
	return res
}

func (p *MockRandomMergePolicy) FindMerges(mergeTrigger *MergeTrigger, segmentInfos *SegmentInfos) {
	panic("not implemented yet")
}

func (p *MockRandomMergePolicy) Close() error { return nil }

func (p *MockRandomMergePolicy) UseCompoundFile(infos *SegmentInfos, mergedInfo *SegmentInfoPerCommit) (bool, error) {
	// 80% of the time we create CFS:
	return p.random.Intn(5) != 1, nil
}

// index/AlcoholicMergePolicy.java

/*
Merge policy for testing, it is like an alcoholic. It drinks (merges)
at night, and randomly decides what to drink. During the daytime it
sleeps.

If tests pass with this, then they are likely to pass with any
bizarro merge policy users might write.

It is a fine bottle of champagne (Ordered by Martijn)
*/
type AlcoholicMergePolicy struct {
	*LogMergePolicy
}

func NewAlcoholicMergePolicy( /*tz TimeZone, */ r *rand.Rand) *AlcoholicMergePolicy {
	panic("not implemented yet")
}
