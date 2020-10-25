package jungle

import "sync/atomic"

type (
	// Tree is the starting point of a process tree
	Tree interface {
		// Branch a new tree from this one
		Branch() Tree

		// BranchFunc creates a new Branch from this tree but instead of simply
		// starting the lifecycle it will also execute the given function.
		//
		// As soon as the function returns, regardless of error conditions,
		// the tree will start the Prune process to stop all of its children.
		//
		// At the moment there are not guarantees regarding the status of Tree
		// when this function returns. The only safe assumption is that
		// Done/Prune/Pruned/Branch/BranchFunc are safe to call, without
		// risking a deadlock.
		BranchFunc(func(Tree) error) Tree
		Pruned() <-chan Signal
		Done() <-chan struct{}
		Prune()
	}

	// Signal is just an alias to an empty struct
	Signal struct{}

	processFunc func(Tree) error

	tree struct {
		pid        uint64
		parent     *tree
		prune      chan Signal
		startPrune chan Signal
		done       chan struct{}
		process    chan processFunc
		newBranch  chan *tree
	}

	subtrees []*tree
)

var (
	rootTree *tree
	pid      uint64
)

func init() {
	rootTree = newTree(nil, nil)
	go rootTree.lifecycle()
}

func newTree(parent *tree, fn processFunc) *tree {
	atomic.AddUint64(&pid, 1)
	branch := &tree{
		pid:        atomic.LoadUint64(&pid),
		parent:     parent,
		done:       make(chan struct{}),
		prune:      make(chan Signal),
		newBranch:  make(chan *tree),
		startPrune: make(chan Signal),
	}
	if fn != nil {
		branch.process = make(chan processFunc, 1)
		branch.process <- fn
	}
	return branch
}

// Root return the single root (aka parent) of all sub-trees
func Root() Tree {
	return rootTree
}

func (t *tree) Branch() Tree {
	return t.BranchFunc(nil)
}

func (t *tree) BranchFunc(fn func(Tree) error) Tree {
	branch := newTree(t, fn)
	t.newBranch <- branch
	// should we wait until fn is recieved to return ????
	// is there a problem not waiting ????
	// for now, this will be undefined behaviour
	return branch
}

func (t *tree) lifecycle() {
	branches := &subtrees{}
	defer func() {
		println("done with: ", t.pid)
		close(t.done)
	}()
	var pruned bool
	selfErr := make(chan error)
	popChildren := make(chan *tree)
	waitSelfProc := make(chan Signal)

	// process function should not be a channel because
	// if there is a process to wait we need to know
	// and wait until that process is complted
	// before we can say that ourselves are done
	//
	// this is because the self-process is kind of a children process
	// itself.

	for !pruned {
		select {
		case c := <-popChildren:
			branches.pop(c)
		case c := <-t.newBranch:
			branches.append(c)
			go func(c *tree) {
				defer func() {
					select {
					case <-t.prune:
					case popChildren <- c:
					}
				}()
				c.lifecycle()
			}(c)
		case fn := <-t.process:
			go func(fn processFunc) {
				err := fn(t)
				selfErr <- err
				close(waitSelfProc)
				// Prune should never be called directly from lifecycle
				// otherwise it will deadlock
				t.Prune()
			}(fn)
		case <-t.startPrune:
			close(t.prune)
			for _, c := range *branches {
				c.Prune()
			}
			pruned = true
			break
		}
	}

	// wait for all children
	for _, c := range *branches {
		// should add a timeout of some sort here
		// but lets not worry about it for now
		<-c.Done()
	}
	return
}

// Done implements the context.Context#Done method and indicates when
// a process tree has finished its processing (and any of its children).
//
// Note that due to undeterministc nature of distributed processing,
// the actual children might outlive their parent if they fail to terminate.
//
// Currently this does not happen because there children have to run in
// the same address space as their parent, but this is behavior is just
// an implementation detail not a semantic guarantee.
func (t *tree) Done() <-chan struct{} {
	return t.done
}

// Prune is used to start the prune process on which this tree will notify
// all of its children that they should stop (aka the children are Pruned).
//
// Prune returns as soon as the signal is captured by the tree,
// use Done to wait until this tree and all of its
// children have terminated.
//
// It is safe to call prune multiple times, only the first one will actually
// have an impact in the system.
func (t *tree) Prune() {
	select {
	case <-t.prune:
		// prune already started as the channel is closed
		return
	case t.startPrune <- Signal{}:
		// prune didn't start, so lets wait until the tree
		// receives the signal
		return
	}
}

// Pruned indicates if this tree has received the signal to be pruned
func (t *tree) Pruned() <-chan Signal {
	return t.prune
}

func (s *subtrees) append(c *tree) {
	*s = append(*s, c)
}

func (s *subtrees) pop(c *tree) {
	changed := -1
	for i, v := range *s {
		if v == c {
			(*s)[i] = nil
			changed = i
			break
		}
	}
	if changed >= 0 {
		head := (*s)[:changed]
		tail := (*s)[changed+1:]
		*s = append(head, tail...)
	}
}
