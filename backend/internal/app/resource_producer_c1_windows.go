package app

import (
	"errors"
	"os"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

func playbackControlSupported() error { return nil }

func validateC1PlatformPath(path string) error {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	h, err := windows.CreateFile(name, 0, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(h)
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(h, &info); err != nil {
		return err
	}
	if info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 || info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY == 0 && info.NumberOfLinks != 1 {
		return os.ErrInvalid
	}
	return nil
}

type windowsPlaybackDomain struct {
	mu           sync.Mutex
	job, process windows.Handle
	created      bool
	terminal     string
}

func newPlaybackControlDomain() (playbackControlDomain, error) {
	job, err := windows.CreateJobObject(nil, nil) // unnamed, non-inheritable, never reopened
	if err != nil {
		return nil, err
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		_ = windows.CloseHandle(job)
		return nil, err
	}
	return &windowsPlaybackDomain{job: job}, nil
}

func (d *windowsPlaybackDomain) start(executable string, args []string, directory string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.job == 0 || d.created || d.terminal != "" {
		return os.ErrInvalid
	}
	attrs, err := windows.NewProcThreadAttributeList(1)
	if err != nil {
		return err
	}
	defer attrs.Delete()
	job := d.job
	// PROC_THREAD_ATTRIBUTE_JOB_LIST associates the child during CreateProcess,
	// before user code. No uncontained Start -> Assign window and no breakaway.
	if err := attrs.Update(0x0002000d, unsafe.Pointer(&job), unsafe.Sizeof(job)); err != nil {
		return err
	}
	si := windows.StartupInfoEx{ProcThreadAttributeList: attrs.List()}
	si.Cb = uint32(unsafe.Sizeof(si))
	app, err := windows.UTF16PtrFromString(executable)
	if err != nil {
		return err
	}
	command, err := windows.UTF16PtrFromString(windows.ComposeCommandLine(append([]string{executable}, args...)))
	if err != nil {
		return err
	}
	cwd, err := windows.UTF16PtrFromString(directory)
	if err != nil {
		return err
	}
	var pi windows.ProcessInformation
	if err := windows.CreateProcess(app, command, nil, nil, false, windows.EXTENDED_STARTUPINFO_PRESENT|windows.CREATE_NO_WINDOW, nil, cwd, &si.StartupInfo, &pi); err != nil {
		return err
	}
	d.process, d.created = pi.Process, true
	return windows.CloseHandle(pi.Thread)
}

type playbackJobAccounting struct {
	User, Kernel, PeriodUser, PeriodKernel int64
	Faults, Total, Active, Terminated      uint32
}

func (d *windowsPlaybackDomain) result(stop bool) (string, error) {
	d.mu.Lock()
	if d.terminal != "" {
		result := d.terminal
		d.mu.Unlock()
		return result, nil
	}
	if d.job == 0 {
		d.mu.Unlock()
		return "", os.ErrInvalid
	}
	if stop {
		if err := windows.TerminateJobObject(d.job, 1); err != nil {
			d.mu.Unlock()
			return "", err
		}
	}
	d.mu.Unlock()
	deadline := time.Now().Add(10 * time.Second)
	for {
		d.mu.Lock()
		if d.terminal != "" {
			result := d.terminal
			d.mu.Unlock()
			return result, nil
		}
		if d.job == 0 {
			d.mu.Unlock()
			return "", os.ErrInvalid
		}
		var info playbackJobAccounting
		err := windows.QueryInformationJobObject(d.job, windows.JobObjectBasicAccountingInformation, uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)), nil)
		if err != nil {
			d.mu.Unlock()
			return "", err
		}
		if info.Active == 0 {
			result := "failed"
			if d.created && !stop {
				var code uint32
				if err := windows.GetExitCodeProcess(d.process, &code); err != nil {
					d.mu.Unlock()
					return "", err
				}
				if code == 259 { // STILL_ACTIVE, not an encoder exit result.
					d.mu.Unlock()
					time.Sleep(time.Millisecond)
					continue
				}
				if code == 0 {
					result = "success"
				}
			}
			d.terminal = result
			d.mu.Unlock()
			return result, nil
		}
		d.mu.Unlock()
		if stop && time.Now().After(deadline) {
			return "", os.ErrDeadlineExceeded
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func (d *windowsPlaybackDomain) close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	var err error
	if d.process != 0 {
		err = windows.CloseHandle(d.process)
		if err == nil {
			d.process = 0
		}
	}
	if d.job != 0 {
		e := windows.CloseHandle(d.job)
		if e == nil {
			d.job = 0
		}
		err = errors.Join(err, e)
	}
	return err
}
