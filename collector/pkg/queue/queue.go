package queue

import (
	"context"
	"errors"
	"sync/atomic"
)

var (
	ErrQueueClosed = errors.New("bounded queue is closed")
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

// Push 阻塞式入队（直到入队成功或 context 取消），天然向上游施加 Backpressure
func (q *BoundedQueue[T]) Push(ctx context.Context, item T) error {
	if q.isClosed.Load() {
		return ErrQueueClosed
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
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

// TryPush 非阻塞入队尝试
func (q *BoundedQueue[T]) TryPush(item T) bool {
	if q.isClosed.Load() {
		return false
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
		return true
	default:
		q.overloadsCount.Add(1)
		return false
	}
}

// Pop 从队列取出元素并计入统计（支持通道关闭后的 drain）
func (q *BoundedQueue[T]) Pop(ctx context.Context) (T, bool) {
	var zero T
	select {
	case <-ctx.Done():
		// ctx 取消后，若 channel 中还有剩余元素，优先尝试 drain 非阻塞消费
		select {
		case item, ok := <-q.ch:
			if ok {
				q.dequeuedCount.Add(1)
			}
			return item, ok
		default:
			return zero, false
		}
	case item, ok := <-q.ch:
		if ok {
			q.dequeuedCount.Add(1)
		}
		return item, ok
	}
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
