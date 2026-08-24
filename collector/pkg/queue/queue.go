package queue

import (
	"context"
	"errors"
	"sync/atomic"
	"time"
)

var (
	ErrQueueFull    = errors.New("bounded queue capacity reached (collector overload)")
	ErrQueueTimeout = errors.New("bounded queue push timed out (consumer stalled)")
	ErrQueueClosed  = errors.New("bounded queue is closed")
)

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
	isClosed       atomic.Bool
}

// NewBoundedQueue 创建指定容量的有界队列
func NewBoundedQueue[T any](capacity int) *BoundedQueue[T] {
	if capacity <= 0 {
		capacity = 200
	}
	return &BoundedQueue[T]{
		ch:       make(chan T, capacity),
		capacity: capacity,
	}
}

// Push 尝试非阻塞入队，若满则返回 ErrQueueFull
func (q *BoundedQueue[T]) Push(item T) error {
	if q.isClosed.Load() {
		return ErrQueueClosed
	}

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
		return ErrQueueFull
	}
}

// PushWithContext 带 context 与超时的阻塞入队，防止静默丢弃
func (q *BoundedQueue[T]) PushWithContext(ctx context.Context, item T, timeout time.Duration) error {
	if q.isClosed.Load() {
		return ErrQueueClosed
	}

	// 先尝试无等待入队
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
	}

	// 队列已满，进入超时等待
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		q.overloadsCount.Add(1)
		return ErrQueueTimeout
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
	}
}

// Pop 从队列取出元素并计入统计
func (q *BoundedQueue[T]) Pop(ctx context.Context) (T, bool) {
	var zero T
	select {
	case <-ctx.Done():
		return zero, false
	case item, ok := <-q.ch:
		if ok {
			q.dequeuedCount.Add(1)
		}
		return item, ok
	}
}

// Channel 返回底层 channel
func (q *BoundedQueue[T]) Channel() <-chan T {
	return q.ch
}

// Close 关闭队列
func (q *BoundedQueue[T]) Close() {
	if q.isClosed.CompareAndSwap(false, true) {
		close(q.ch)
	}
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
