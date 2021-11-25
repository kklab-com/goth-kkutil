package concurrent

import (
	"sync"
	"time"
)

type Mutex struct {
	l    chan int
	ul   chan int
	once sync.Once
	s    sync.Mutex
}

func (m *Mutex) check() {
	m.once.Do(func() {
		m.l = make(chan int, 1)
		m.ul = make(chan int, 1)
		m.l <- 1
	})
}

// TryLockTimeout
// try to get lock with timeout
// it will return `true` when get lock success or `false` on fail
func (m *Mutex) TryLockTimeout(timeout time.Duration) bool {
	m.check()
	select {
	case <-m.l:
		m.ul <- 1
		return true
	case <-time.After(timeout):
		return false
	}
}

func (m *Mutex) TryLock() bool {
	m.check()
	select {
	case <-m.l:
		m.ul <- 1
		return true
	default:
		return false
	}
}

func (m *Mutex) Lock() {
	m.check()
	select {
	case <-m.l:
		m.ul <- 1
		return
	case <-time.After(time.Hour):
		m.Lock()
	}
}

func (m *Mutex) Unlock() {
	m.check()
	select {
	case <-m.ul:
		m.l <- 1
		return
	default:
		panic("concurrent: unlock of unlocked mutex")
	}
}
