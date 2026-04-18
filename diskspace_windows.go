package main

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

func getDiskFreeSpace(path string) string {
	pathPtr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return "Unknown"
	}
	var freeBytes, totalBytes, totalFreeBytes uint64
	err = windows.GetDiskFreeSpaceEx(pathPtr,
		(*uint64)(unsafe.Pointer(&freeBytes)),
		(*uint64)(unsafe.Pointer(&totalBytes)),
		(*uint64)(unsafe.Pointer(&totalFreeBytes)),
	)
	if err != nil {
		return "Unknown"
	}
	return formatBytesShort(freeBytes)
}

func formatBytesShort(b uint64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := uint64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}
