package concurrent

import (
	"fmt"
	"sync/atomic"
	"time"
)

const (
	stateWait = iota
	stateSuccess
	stateCancel
	stateFail
)

type ChainFuture interface {
	Future
	Parent() ChainFuture
}

type Future interface {
	Immutable
	Completable() Completable
	Immutable() Immutable
	Chainable() ChainFuture
	Then(fn func(parent Future) interface{}) (future Future)
	ThenAsync(fn func(parent Future) interface{}) (future Future)
}

type Immutable interface {
	Get() interface{}
	GetTimeout(timeout time.Duration) interface{}
	GetNow() interface{}
	Done() <-chan struct{}
	Await() Future
	AwaitTimeout(timeout time.Duration) Future
	IsDone() bool
	IsSuccess() bool
	IsCancelled() bool
	IsFail() bool
	Error() error
	AddListener(listener FutureListener) Future
}

type Completable interface {
	Complete(obj interface{}) bool
	Cancel() bool
	Fail(err error) bool
}

type Settable interface {
	Set(obj interface{})
}

func NewFuture() Future {
	return newDefaultFuture()
}

func NewCompletedFuture(obj interface{}) Future {
	f := NewFuture()
	f.Completable().Complete(obj)
	return f
}

func NewCancelledFuture() Future {
	f := NewFuture()
	f.Completable().Cancel()
	return f
}

func NewFailedFuture(err error) Future {
	f := NewFuture()
	f.Completable().Fail(err)
	return f
}

func NewChainFuture(future ChainFuture) ChainFuture {
	f := newDefaultFuture()
	f.parent = future
	return f
}

type DefaultFuture struct {
	m         Mutex
	obj       interface{}
	state     int32
	err       error
	listeners []FutureListener
	parent    ChainFuture
}

func newDefaultFuture() *DefaultFuture {
	var f = &DefaultFuture{
		listeners: []FutureListener{},
	}

	f.m.Lock()
	return f
}

func (f *DefaultFuture) Parent() ChainFuture {
	return f.parent
}

func (f *DefaultFuture) self() Future {
	return f
}

func (f *DefaultFuture) Completable() Completable {
	return f.self().(Completable)
}

func (f *DefaultFuture) Immutable() Immutable {
	return f.self().(Immutable)
}

func (f *DefaultFuture) Chainable() ChainFuture {
	return f.self().(ChainFuture)
}

func (f *DefaultFuture) _waitJudge(timeout time.Duration) (done bool) {
	if timeout == 0 {
		<-f.m.Unlocked()
		return true
	} else {
		select {
		case <-f.m.Unlocked():
			return true
		case <-time.After(timeout):
			return false
		}
	}
}

func (f *DefaultFuture) Get() interface{} {
	return f.GetTimeout(0)
}

func (f *DefaultFuture) GetTimeout(timeout time.Duration) interface{} {
	if f.IsDone() {
		return f.obj
	}

	if f._waitJudge(timeout) {
		return f.obj
	}

	return nil
}

func (f *DefaultFuture) GetNow() interface{} {
	return f.obj
}

func (f *DefaultFuture) Done() <-chan struct{} {
	return f.m.Unlocked()
}

func (f *DefaultFuture) Await() Future {
	<-f.m.Unlocked()
	return f
}

func (f *DefaultFuture) AwaitTimeout(timeout time.Duration) Future {
	f.GetTimeout(timeout)
	return f
}

func (f *DefaultFuture) IsDone() bool {
	return atomic.LoadInt32(&f.state) != stateWait
}

func (f *DefaultFuture) IsSuccess() bool {
	return atomic.LoadInt32(&f.state) == stateSuccess
}

func (f *DefaultFuture) IsCancelled() bool {
	return atomic.LoadInt32(&f.state) == stateCancel
}

func (f *DefaultFuture) IsFail() bool {
	return atomic.LoadInt32(&f.state) == stateFail
}

func (f *DefaultFuture) Error() error {
	return f.err
}

func (f *DefaultFuture) AddListener(listener FutureListener) Future {
	if listener == nil {
		return f
	}

	if f.IsDone() {
		<-f.m.Unlocked()
		listener.OperationCompleted(f)
		return f
	}

	f.listeners = append(f.listeners, listener)
	return f
}

func (f *DefaultFuture) Complete(obj interface{}) bool {
	if atomic.CompareAndSwapInt32(&f.state, stateWait, stateSuccess) {
		if obj != nil {
			f.obj = obj
		}

		f.m.Unlock()
		f.callListener()
		return true
	}

	return false
}

func (f *DefaultFuture) Cancel() bool {
	if atomic.CompareAndSwapInt32(&f.state, stateWait, stateCancel) {
		f.m.Unlock()
		f.callListener()
		return true
	}

	return false
}

func (f *DefaultFuture) Fail(err error) bool {
	if atomic.CompareAndSwapInt32(&f.state, stateWait, stateFail) {
		f.err = err
		f.m.Unlock()
		f.callListener()
		return true
	}

	return false
}

func (f *DefaultFuture) Set(obj interface{}) {
	f.obj = obj
}

func (f *DefaultFuture) callListener() {
	if f.listeners == nil {
		return
	}

	for _, listener := range f.listeners {
		if listener == nil {
			continue
		}

		listener.OperationCompleted(f)
	}
}

func (f *DefaultFuture) Then(fn func(parent Future) interface{}) (future Future) {
	cf := f
	future = NewChainFuture(cf)
	lfn := fn
	f.AddListener(NewFutureListener(func(f Future) {
		if cf.IsCancelled() {
			future.Completable().Cancel()
			return
		}

		if future.IsDone() {
			return
		}

		var rtn interface{}
		defer func() {
			if e := recover(); e != nil {
				if err, ok := e.(error); ok {
					future.Completable().Fail(err)
				} else {
					future.Completable().Fail(fmt.Errorf("%v", e))
				}
			} else {
				future.Completable().Complete(rtn)
			}
		}()

		rtn = lfn(cf)
	}))

	return future
}

func (f *DefaultFuture) ThenAsync(fn func(parent Future) interface{}) (future Future) {
	cf := f
	future = NewChainFuture(cf)
	lfn := fn
	f.AddListener(NewFutureListener(func(f Future) {
		if cf.IsCancelled() {
			future.Completable().Cancel()
			return
		}

		if future.IsDone() {
			return
		}

		go func(cf Future, future Future, lfn func(parent Future) interface{}) {
			var rtn interface{}
			defer func() {
				if e := recover(); e != nil {
					if err, ok := e.(error); ok {
						future.Completable().Fail(err)
					} else {
						future.Completable().Fail(fmt.Errorf("%v", e))
					}
				} else {
					future.Completable().Complete(rtn)
				}
			}()

			rtn = lfn(cf)
		}(cf, future, lfn)
	}))

	return future
}

type FutureListener interface {
	OperationCompleted(f Future)
}

type _FutureListener struct {
	f func(f Future)
}

func (l *_FutureListener) OperationCompleted(f Future) {
	l.f(f)
}

func NewFutureListener(f func(f Future)) FutureListener {
	lf := f
	return &_FutureListener{
		f: lf,
	}
}

func Do(f func() interface{}) (future Future) {
	future = NewFuture()
	var rtn interface{}
	defer func() {
		if e := recover(); e != nil {
			if err, ok := e.(error); ok {
				future.Completable().Fail(err)
			} else {
				future.Completable().Fail(fmt.Errorf("%v", e))
			}
		} else {
			future.Completable().Complete(rtn)
		}
	}()

	rtn = f()
	return future
}

func DoAsync(f func() interface{}) (future Future) {
	future = NewFuture()
	go func() {
		var rtn interface{}
		defer func() {
			if e := recover(); e != nil {
				if err, ok := e.(error); ok {
					future.Completable().Fail(err)
				} else {
					future.Completable().Fail(fmt.Errorf("%v", e))
				}
			} else {
				future.Completable().Complete(rtn)
			}
		}()

		rtn = f()
	}()

	return future
}
