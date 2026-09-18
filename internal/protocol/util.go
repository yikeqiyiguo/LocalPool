package protocol

import (
	"crypto/rand"
	"encoding/hex"
	"sync/atomic"
	"time"
)

// NowMs 毫秒时间戳
func NowMs() int64 { return time.Now().UnixMilli() }

var idSeq int64

// NewID 生成带前缀的唯一 ID：前缀 + 时间戳 + 随机字节 + 序列
func NewID(prefix string) string {
	var b [4]byte
	_, _ = rand.Read(b[:])
	n := atomic.AddInt64(&idSeq, 1) % 1000
	return prefix + "_" + time.Now().Format("20060102150405") + "_" + hex.EncodeToString(b[:]) + "_" + itoa(n)
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	var buf [8]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

// AppendIfMissing 去重追加
func AppendIfMissing(list []string, v string) []string {
	for _, x := range list {
		if x == v {
			return list
		}
	}
	return append(list, v)
}

// RemoveVal 移除指定值
func RemoveVal(list []string, v string) []string {
	out := list[:0]
	for _, x := range list {
		if x != v {
			out = append(out, x)
		}
	}
	return out
}
