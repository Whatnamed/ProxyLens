package queue

import (
	"errors"
	"sync"
	"sync/atomic"
)

var ErrQueueFull = errors.New("bounded queue capacity reached (collector overload)")

// Metrics 记录队列统计信息
type Metrics struct {
	Capacity       int   `json:"capacity"`
	CurrentDepth   int   `json:"currentDepth"`
	PeakDepth      int   `json:"peakDepth"`
	EnqueuedCount  int64 `json:"enqueuedCount"`
	DequeuedCount  int64 `json:"dequeuedCount"`
	OverloadsCount int64 `json:"overloadsCount"`
}

// BoundedQueue 是带背压保护的有界队列
type BoundedQueue[T any] struct {
	ch             chan T
	capacity       int
	enqueuedCount  atomic.Int64
	dequeuedCount  atomic.Int64
	overloadsCount atomic.Int64
	peakDepth      atomic.Int64
	mu             sync.Mutex
	onOverload     func()
}

// NewBoundedQueue 创建指定容量的有界队列
func NewBoundedQueue[T any](capacity int, onOverload func()) *BoundedQueue[T] {
	if capacity <= 0 {
		capacity = 100
	}
	return &BoundedQueue[T]{
		ch:         make(chan T, capacity),
		capacity:   capacity,
		onOverload: onOverload,
	}
}

// Push 尝试将元素放入队列，若满则触发过载保护并不静默丢弃
func (q *BoundedQueue[T]) Push(item T) error {
	select {
	case q.ch <- item:
		q.enqueuedCount.Add(1)
		cur := int64(len(q.ch))
		for {
			peak := q.peakDepth.Load()
			if cur <= peak || q.peakDepth.CompareAndSwap(peak, cur) {
				break
			}
		}
		return nil
	default:
		q.overloadsCount.Add(1)
		if q.onOverload != nil {
			q.onOverload()
		}
		return ErrQueueFull
	}
}

// Pop 从队列取出元素
func (q *BoundedQueue[T]) Pop() (T, bool) {
	item, ok := <-q.ch
	if ok {
		q.dequeuedCount.Add(1)
	}
	return item, ok
}

// Channel 返回底层只读 channel
func (q *BoundedQueue[T]) Channel() <-chan T {
	return q.ch
}

// Close 关闭队列
func (q *BoundedQueue[T]) Close() {
	close(q.ch)
}

// GetMetrics 获取当前队列指标
func (q *BoundedQueue[T]) GetMetrics() Metrics {
	return Metrics{
		Capacity:       q.capacity,
		CurrentDepth:   len(q.ch),
		PeakDepth:      int(q.peakDepth.Load()),
		EnqueuedCount:  q.enqueuedCount.Load(),
		DequeuedCount:  q.dequeuedCount.Load(),
		OverloadsCount: q.overloadsCount.Load(),
	}
}
