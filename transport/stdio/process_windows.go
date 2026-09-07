//go:build windows

package stdio

import (
	"errors"
	"fmt"
	"os/exec"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	createSuspended = 0x00000004
	createNoWindow  = 0x08000000
)

var isProcessInJobProc = windows.NewLazySystemDLL("kernel32.dll").NewProc("IsProcessInJob")

func configureProcessCommand(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.HideWindow = true
	cmd.SysProcAttr.CreationFlags |= createNoWindow | createSuspended
}

type windowsJob struct {
	mu            sync.Mutex
	handle        windows.Handle
	terminateOnce sync.Once
	terminateErr  error
}

type jobBasicProcessIDListHeader struct {
	numberOfAssignedProcesses uint32
	numberOfProcessIDsInList  uint32
}

func attachProcessTree(cmd *exec.Cmd, _ <-chan struct{}) (processTree, error) {
	if cmd.Process == nil {
		return nil, errors.New("stdio: started process is unavailable")
	}
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, fmt.Errorf("stdio: create process job: %w", err)
	}
	cleanup := func() {
		_ = windows.TerminateJobObject(job, 1)
		_ = windows.CloseHandle(job)
	}

	limits := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	limits.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(
		job,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&limits)),
		uint32(unsafe.Sizeof(limits)),
	); err != nil {
		cleanup()
		return nil, fmt.Errorf("stdio: configure process job: %w", err)
	}

	processHandle, err := windows.OpenProcess(
		windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE,
		false,
		uint32(cmd.Process.Pid),
	)
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("stdio: open child process: %w", err)
	}
	assignErr := windows.AssignProcessToJobObject(job, processHandle)
	closeProcessErr := windows.CloseHandle(processHandle)
	if assignErr != nil {
		cleanup()
		return nil, fmt.Errorf("stdio: assign child to process job: %w", assignErr)
	}
	if closeProcessErr != nil {
		cleanup()
		return nil, fmt.Errorf("stdio: close child process handle: %w", closeProcessErr)
	}
	if err := resumeSuspendedProcess(uint32(cmd.Process.Pid)); err != nil {
		cleanup()
		return nil, err
	}
	return &windowsJob{handle: job}, nil
}

func resumeSuspendedProcess(pid uint32) error {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPTHREAD, 0)
	if err != nil {
		return fmt.Errorf("stdio: enumerate suspended child threads: %w", err)
	}
	defer windows.CloseHandle(snapshot)

	entry := windows.ThreadEntry32{Size: uint32(unsafe.Sizeof(windows.ThreadEntry32{}))}
	if err := windows.Thread32First(snapshot, &entry); err != nil {
		return fmt.Errorf("stdio: inspect suspended child threads: %w", err)
	}
	var threadID uint32
	var threadCount int
	for {
		if entry.OwnerProcessID == pid {
			threadID = entry.ThreadID
			threadCount++
		}
		if err := windows.Thread32Next(snapshot, &entry); err != nil {
			if errors.Is(err, windows.ERROR_NO_MORE_FILES) {
				break
			}
			return fmt.Errorf("stdio: inspect suspended child threads: %w", err)
		}
	}
	if threadCount != 1 {
		return fmt.Errorf("stdio: suspended child has %d threads, want exactly one", threadCount)
	}
	thread, err := windows.OpenThread(windows.THREAD_SUSPEND_RESUME, false, threadID)
	if err != nil {
		return fmt.Errorf("stdio: open suspended child thread: %w", err)
	}
	_, resumeErr := windows.ResumeThread(thread)
	closeErr := windows.CloseHandle(thread)
	if resumeErr != nil {
		return fmt.Errorf("stdio: resume suspended child: %w", resumeErr)
	}
	if closeErr != nil {
		return fmt.Errorf("stdio: close child thread handle: %w", closeErr)
	}
	return nil
}

func (j *windowsJob) terminate() error {
	j.terminateOnce.Do(func() {
		j.mu.Lock()
		defer j.mu.Unlock()
		if j.handle != 0 {
			// Retain existing members before termination. A terminating process
			// can disappear from the job's PID list before its handle is signaled.
			before, beforeErr := openJobProcessHandles(j.handle)
			if err := windows.TerminateJobObject(j.handle, 1); err != nil {
				j.terminateErr = errors.Join(beforeErr, err, closeWindowsHandles(before))
				return
			}
			// Also retain members created while taking the first snapshot. Never
			// replace the pre-termination handles with this second, narrower view.
			after, afterErr := openJobProcessHandles(j.handle)
			handles := append(before, after...)
			waitErr := waitForWindowsProcesses(handles)
			emptyErr := waitForWindowsJobEmpty(j.handle)
			closeErr := closeWindowsHandles(handles)
			j.terminateErr = errors.Join(beforeErr, afterErr, waitErr, emptyErr, closeErr)
		}
	})
	return j.terminateErr
}

func openJobProcessHandles(job windows.Handle) ([]windows.Handle, error) {
	processIDs, err := queryJobProcessIDs(job)
	if err != nil {
		return nil, err
	}
	handles := make([]windows.Handle, 0, len(processIDs))
	var openErr error
	for _, processID := range processIDs {
		handle, err := windows.OpenProcess(
			windows.SYNCHRONIZE|windows.PROCESS_QUERY_LIMITED_INFORMATION,
			false,
			processID,
		)
		if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
			// The process exited after the job snapshot and before OpenProcess.
			continue
		}
		if err != nil {
			openErr = errors.Join(openErr, fmt.Errorf("stdio: open job process %d: %w", processID, err))
			continue
		}
		inJob, err := processInJob(handle, job)
		if err != nil {
			openErr = errors.Join(openErr, fmt.Errorf("stdio: verify job process %d: %w", processID, err))
			if closeErr := windows.CloseHandle(handle); closeErr != nil {
				openErr = errors.Join(openErr, closeErr)
			}
			continue
		}
		if !inJob {
			// The listed process exited and its PID was recycled before
			// OpenProcess. Never wait on a process outside the owned job.
			if closeErr := windows.CloseHandle(handle); closeErr != nil {
				openErr = errors.Join(openErr, closeErr)
			}
			continue
		}
		handles = append(handles, handle)
	}
	return handles, openErr
}

func processInJob(process, job windows.Handle) (bool, error) {
	var result int32
	succeeded, _, callErr := isProcessInJobProc.Call(
		uintptr(process),
		uintptr(job),
		uintptr(unsafe.Pointer(&result)),
	)
	if succeeded == 0 {
		if callErr == nil || errors.Is(callErr, windows.ERROR_SUCCESS) {
			callErr = syscall.EINVAL
		}
		return false, callErr
	}
	return result != 0, nil
}

func queryJobProcessIDs(job windows.Handle) ([]uint32, error) {
	capacity := uint32(16)
	for {
		// uintptr storage keeps the variable-length process ID list aligned on
		// both 32-bit and 64-bit Windows. The two DWORD counters occupy the
		// first eight bytes and are followed immediately by ULONG_PTR IDs.
		buffer := make([]uintptr, int(capacity)+2)
		header := (*jobBasicProcessIDListHeader)(unsafe.Pointer(&buffer[0]))
		err := windows.QueryInformationJobObject(
			job,
			windows.JobObjectBasicProcessIdList,
			uintptr(unsafe.Pointer(&buffer[0])),
			uint32(len(buffer))*uint32(unsafe.Sizeof(uintptr(0))),
			nil,
		)
		if err != nil && !errors.Is(err, syscall.ERROR_MORE_DATA) && !errors.Is(err, windows.ERROR_INSUFFICIENT_BUFFER) {
			return nil, fmt.Errorf("stdio: query job process list: %w", err)
		}
		if header.numberOfProcessIDsInList < header.numberOfAssignedProcesses || err != nil {
			if header.numberOfAssignedProcesses > capacity {
				capacity = header.numberOfAssignedProcesses
			} else {
				capacity *= 2
			}
			continue
		}

		ids := unsafe.Slice(
			(*uintptr)(unsafe.Add(unsafe.Pointer(&buffer[0]), unsafe.Sizeof(*header))),
			header.numberOfProcessIDsInList,
		)
		processIDs := make([]uint32, len(ids))
		for index, processID := range ids {
			processIDs[index] = uint32(processID)
		}
		return processIDs, nil
	}
}

func waitForWindowsProcesses(handles []windows.Handle) error {
	var waitErr error
	for _, handle := range handles {
		status, err := windows.WaitForSingleObject(handle, windows.INFINITE)
		if err != nil {
			waitErr = errors.Join(waitErr, err)
			continue
		}
		if status != windows.WAIT_OBJECT_0 {
			waitErr = errors.Join(waitErr, fmt.Errorf("stdio: wait for terminated job process returned %#x", status))
		}
	}
	return waitErr
}

// The accounting count covers descendants created during enumeration as well.
// Completion-port exit notifications are not guaranteed to be delivered, so
// query the authoritative count instead of relying on receipt of a message.
func waitForWindowsJobEmpty(job windows.Handle) error {
	type basicAccounting struct {
		totalUserTime, totalKernelTime                                                 int64
		thisPeriodTotalUserTime, thisPeriodTotalKernelTime                             int64
		totalPageFaultCount, totalProcesses, activeProcesses, totalTerminatedProcesses uint32
	}
	for {
		var info basicAccounting
		if err := windows.QueryInformationJobObject(job, windows.JobObjectBasicAccountingInformation,
			uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)), nil); err != nil {
			return fmt.Errorf("stdio: query terminating job accounting: %w", err)
		}
		if info.activeProcesses == 0 {
			return nil
		}
		time.Sleep(time.Millisecond)
	}
}

func closeWindowsHandles(handles []windows.Handle) error {
	var closeErr error
	for _, handle := range handles {
		if err := windows.CloseHandle(handle); err != nil {
			closeErr = errors.Join(closeErr, err)
		}
	}
	return closeErr
}

func (j *windowsJob) release() error {
	terminateErr := j.terminate()
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.handle == 0 {
		return terminateErr
	}
	closeErr := windows.CloseHandle(j.handle)
	j.handle = 0
	return errors.Join(terminateErr, closeErr)
}
