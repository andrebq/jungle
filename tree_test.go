package jungle

import (
	"runtime"
	"sync/atomic"
	"testing"
	"time"
)

func TestLifecycle(t *testing.T) {
	localRoot := Root().Branch()
	var count int32
	localRoot.BranchFunc(func(branch Tree) error {
		<-branch.Pruned()
		atomic.AddInt32(&count, 1)
		return nil
	})
	localRoot.BranchFunc(func(branch Tree) error {
		<-branch.Pruned()
		time.Sleep(time.Millisecond * 500)
		atomic.AddInt32(&count, 1)
		return nil
	})
	runtime.Gosched()
	// this will prune this tree and all the sub-trees
	localRoot.Prune()
	// this means that this tree has ended its lifecycle and won't branch out
	// neither will be grow again
	<-localRoot.Done()

	if atomic.LoadInt32(&count) != 2 {
		t.Fatalf("count should be 2 but got %v", atomic.LoadInt32(&count))
	}
}
