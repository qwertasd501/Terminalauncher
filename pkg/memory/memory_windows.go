//go:build windows

package memory

import (
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

// The working set of a process is reported by GetProcessMemoryInfo, which lives in psapi.dll. The
// counter structure is only read, never written, so the layout has to match Windows' exactly: two
// uint32 fields followed by nine size_t fields.
type processMemoryCounters struct {
	Size                       uint32
	PageFaultCount             uint32
	PeakWorkingSetSize         uintptr
	WorkingSetSize             uintptr
	QuotaPeakPagedPoolUsage    uintptr
	QuotaPagedPoolUsage        uintptr
	QuotaPeakNonPagedPoolUsage uintptr
	QuotaNonPagedPoolUsage     uintptr
	PagefileUsage              uintptr
	PeakPagefileUsage          uintptr
}

var (
	// Resolved on first use, so that a system without psapi.dll still runs the launcher.
	psapi                = windows.NewLazySystemDLL("psapi.dll")
	getProcessMemoryInfo = psapi.NewProc("GetProcessMemoryInfo")

	processMemoryCountersSize = uint32(unsafe.Sizeof(processMemoryCounters{}))
	processEntry32Size        = uint32(unsafe.Sizeof(windows.ProcessEntry32{}))

	// The documented way of asking for the smallest working set the system can manage right now.
	trimWorkingSetMinimum = ^uintptr(0)
)

// workingSet reports how many bytes of a process currently sit in physical memory.
func workingSet(handle windows.Handle) (int64, bool) {
	var counters processMemoryCounters
	counters.Size = processMemoryCountersSize

	result, _, _ := getProcessMemoryInfo.Call(
		uintptr(handle),
		uintptr(unsafe.Pointer(&counters)),
		uintptr(counters.Size),
	)
	if result == 0 {
		return 0, false
	}
	return int64(counters.WorkingSetSize), true
}

// trim moves the pages a process is not using right now out of physical memory, and reports how
// many bytes that was worth.
//
// The call is the documented trick of asking for a working set of (SIZE_T)-1 bytes: the system
// takes that as "as small as you can make it right now". A process that cannot be opened is left
// alone - that covers the system's own processes, which no user program may touch.
func trim(pid uint32) (int64, bool) {
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_INFORMATION|windows.PROCESS_SET_QUOTA, false, pid)
	if err != nil {
		return 0, false
	}
	defer windows.CloseHandle(handle)

	before, ok := workingSet(handle)
	if !ok {
		return 0, false
	}
	if err := windows.SetProcessWorkingSetSizeEx(handle, trimWorkingSetMinimum, trimWorkingSetMinimum, 0); err != nil {
		return 0, false
	}
	after, ok := workingSet(handle)
	if !ok {
		// The trim itself succeeded; only the measurement is missing.
		return 0, true
	}
	if freed := before - after; freed > 0 {
		return freed, true
	}
	return 0, true
}

func clear(all bool) (Result, error) {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return Result{}, err
	}
	defer windows.CloseHandle(snapshot)

	self := uint32(os.Getpid())
	var result Result

	entry := windows.ProcessEntry32{Size: processEntry32Size}
	for err := windows.Process32First(snapshot, &entry); err == nil; err = windows.Process32Next(snapshot, &entry) {
		if entry.ProcessID == 0 {
			continue
		}
		if !all && entry.ProcessID != self && !isGameProcess(windows.UTF16ToString(entry.ExeFile[:])) {
			result.Skipped++
			continue
		}

		freed, ok := trim(entry.ProcessID)
		if !ok {
			result.Skipped++
			continue
		}
		result.Processes++
		result.Freed += freed
	}
	return result, nil
}
