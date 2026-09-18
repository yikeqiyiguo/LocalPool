//go:build windows

package agent

import "unsafe"

var procGetTickCount = kernel32.NewProc("GetTickCount")

type lastinputinfo struct {
	cbSize uint32
	dwTime uint32
}

// humanIdleSec 返回距离最近一次键盘/鼠标输入的秒数；无法获取时返回 -1
func humanIdleSec() float64 {
	var lii lastinputinfo
	lii.cbSize = uint32(unsafe.Sizeof(lii))
	r1, _, _ := procGetLastInputInfo.Call(uintptr(unsafe.Pointer(&lii)))
	if r1 == 0 {
		return -1
	}
	tick, _, _ := procGetTickCount.Call()
	now := uint32(tick)
	diff := uint32(now - lii.dwTime)
	return float64(diff) / 1000.0
}
