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

type jobBasicAccountingInformation struct {
	totalUserTime             int64
	totalKernelTime           int64
	thisPeriodTotalUserTime   int64
	thisPeriodTotalKernelTime int64
	totalPageFaultCount       uint32
	totalProcesses            uint32
	activeProcesses           uint32
	totalTerminatedProcesses  uint32
}

func attachProcessTree(cmd *exec.Cmd) (processTree, error) {
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
			if err := windows.TerminateJobObject(j.handle, 1); err != nil {
				j.terminateErr = err
				return
			}
			for {
				var accounting jobBasicAccountingInformation
				if err := windows.QueryInformationJobObject(
					j.handle,
					windows.JobObjectBasicAccountingInformation,
					uintptr(unsafe.Pointer(&accounting)),
					uint32(unsafe.Sizeof(accounting)),
					nil,
				); err != nil {
					j.terminateErr = err
					return
				}
				if accounting.activeProcesses == 0 {
					return
				}
				time.Sleep(time.Millisecond)
			}
		}
	})
	return j.terminateErr
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
