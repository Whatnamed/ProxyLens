package storage

import (
	"math/big"
	"time"
)

// IntervalAllocator 实现无溢出、绝对可加守恒的时间区间分摊器
// F(t): 累计流量分摊函数
// - F(t) = 0 (t <= start)
// - F(t) = floor(total * (t - start) / (end - start)) (start < t < end)
// - F(t) = total (t >= end)
//
// 任意半开区间 [a, b) 的分摊量严格定义为:
// Alloc([a, b)) = F(b) - F(a)
//
// 该数学构造严格保证:
// 1. 单调非负性: 任意 a <= b => Alloc([a, b)) >= 0 (永不为负)
// 2. 严格可加性 (Additive Invariant): 对任意分割点 c, Alloc([a, c)) + Alloc([c, b)) = Alloc([a, b))
// 3. 总体守恒性 (Total Conservation): Alloc([start, end)) = total
// 4. 128 位 / big.Int 精度计算: 杜绝大数值 (如 10GB / MaxInt64) 纳秒相乘时的整数溢出
type IntervalAllocator struct {
	start time.Time
	end   time.Time
	total int64
}

// NewIntervalAllocator 创建分摊器实例
func NewIntervalAllocator(start, end time.Time, total int64) *IntervalAllocator {
	return &IntervalAllocator{
		start: start.UTC(),
		end:   end.UTC(),
		total: total,
	}
}

// F 计算在时刻 t 的累计分摊字节数
func (a *IntervalAllocator) F(t time.Time) int64 {
	t = t.UTC()
	if !t.After(a.start) {
		return 0
	}
	if !t.Before(a.end) {
		return a.total
	}

	totalDurNs := a.end.Sub(a.start).Nanoseconds()
	if totalDurNs <= 0 || a.total <= 0 {
		return 0
	}

	elapsedNs := t.Sub(a.start).Nanoseconds()
	if elapsedNs <= 0 {
		return 0
	}
	if elapsedNs >= totalDurNs {
		return a.total
	}

	// 128 位无溢出高精度计算: floor((total * elapsedNs) / totalDurNs)
	bigTotal := big.NewInt(a.total)
	bigElapsed := big.NewInt(elapsedNs)
	bigDur := big.NewInt(totalDurNs)

	prod := new(big.Int).Mul(bigTotal, bigElapsed)
	div := new(big.Int).Div(prod, bigDur)

	return div.Int64()
}

// Allocate 计算落在 [winStart, winEnd) 窗口内的分摊字节数: F(winEnd) - F(winStart)
func (a *IntervalAllocator) Allocate(winStart, winEnd time.Time) int64 {
	winStart = winStart.UTC()
	winEnd = winEnd.UTC()
	if !winEnd.After(winStart) {
		return 0
	}
	val := a.F(winEnd) - a.F(winStart)
	if val < 0 {
		return 0
	}
	return val
}
