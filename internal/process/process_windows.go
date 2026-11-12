//go:build windows

package process

import (
	"context"
	"errors"
	"io"
	"os"
	"runtime"
	"sort"
	"strings"
	"syscall"
	"unicode/utf16"
	"unsafe"
)

var kernel32 = syscall.NewLazyDLL("kernel32.dll")
var createJobObject = kernel32.NewProc("CreateJobObjectW")
var setInformationJobObject = kernel32.NewProc("SetInformationJobObject")
var assignProcessToJobObject = kernel32.NewProc("AssignProcessToJobObject")
var terminateJobObject = kernel32.NewProc("TerminateJobObject")
var resumeThread = kernel32.NewProc("ResumeThread")
var initializeProcThreadAttributeList = kernel32.NewProc("InitializeProcThreadAttributeList")
var updateProcThreadAttribute = kernel32.NewProc("UpdateProcThreadAttribute")
var deleteProcThreadAttributeList = kernel32.NewProc("DeleteProcThreadAttributeList")

type jobBasicLimit struct {
	PerProcessUserTimeLimit int64
	PerJobUserTimeLimit     int64
	LimitFlags              uint32
	MinimumWorkingSetSize   uintptr
	MaximumWorkingSetSize   uintptr
	ActiveProcessLimit      uint32
	Affinity                uintptr
	PriorityClass           uint32
	SchedulingClass         uint32
}

type jobExtendedLimit struct {
	BasicLimitInformation jobBasicLimit
	IoInfo                [6]uint64
	ProcessMemoryLimit    uintptr
	JobMemoryLimit        uintptr
	PeakProcessMemoryUsed uintptr
	PeakJobMemoryUsed     uintptr
}

type startupInfoEx struct {
	syscall.StartupInfo
	AttributeList unsafe.Pointer
}

// Replace cannot replace a Windows process. It owns a child job instead and
// preserves the child's byte streams and exact native exit status.
func Replace(ctx context.Context, path string, args, env []string) error {
	code, err := Run(ctx, path, args, env, os.Stdin, os.Stdout, os.Stderr)
	if err != nil {
		return err
	}
	os.Exit(code)
	return nil
}

// Run starts the child suspended, assigns a kill-on-close job, and only then
// resumes it. This closes the spawn-before-job race and also kills descendants
// when the host terminates the adapter. Only the three standard handles are
// inherited. Arguments and environment never pass through cmd.exe or PowerShell.
func Run(ctx context.Context, path string, args, env []string, stdin io.Reader, stdout, stderr io.Writer) (int, error) {
	if err := ctx.Err(); err != nil {
		return -1, err
	}
	job, _, err := createJobObject.Call(0, 0)
	if job == 0 {
		return -1, err
	}
	defer syscall.CloseHandle(syscall.Handle(job))
	limits := jobExtendedLimit{}
	limits.BasicLimitInformation.LimitFlags = 0x2000 // JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if ok, _, callErr := setInformationJobObject.Call(job, 9, uintptr(unsafe.Pointer(&limits)), unsafe.Sizeof(limits)); ok == 0 {
		return -1, callErr
	}
	var owned []*os.File
	defer func() {
		for _, f := range owned {
			_ = f.Close()
		}
	}()
	input, ok := stdin.(*os.File)
	if !ok {
		if stdin == nil {
			input, err = os.Open(os.DevNull)
		} else {
			var writer *os.File
			input, writer, err = os.Pipe()
			if err == nil {
				owned = append(owned, writer)
				go func() { _, _ = io.Copy(writer, stdin); _ = writer.Close() }()
			}
		}
		if err != nil {
			return -1, err
		}
		owned = append(owned, input)
	}
	type copiedOutput struct {
		reader *os.File
		writer io.Writer
	}
	var outputs []copiedOutput
	outputFile := func(writer io.Writer) (*os.File, error) {
		if file, ok := writer.(*os.File); ok {
			return file, nil
		}
		if writer == nil {
			writer = io.Discard
		}
		r, w, e := os.Pipe()
		if e == nil {
			owned = append(owned, r, w)
			outputs = append(outputs, copiedOutput{r, writer})
		}
		return w, e
	}
	out, err := outputFile(stdout)
	if err != nil {
		return -1, err
	}
	errOut, err := outputFile(stderr)
	if err != nil {
		return -1, err
	}
	parent, err := syscall.GetCurrentProcess()
	if err != nil {
		return -1, err
	}
	var handles [3]syscall.Handle
	defer func() {
		for _, handle := range handles {
			if handle != 0 {
				_ = syscall.CloseHandle(handle)
			}
		}
	}()
	for i, file := range []*os.File{input, out, errOut} {
		if err = syscall.DuplicateHandle(parent, syscall.Handle(file.Fd()), parent, &handles[i], 0, true, syscall.DUPLICATE_SAME_ACCESS); err != nil {
			return -1, err
		}
	}
	var attributeBytes uintptr
	_, _, _ = initializeProcThreadAttributeList.Call(0, 1, 0, uintptr(unsafe.Pointer(&attributeBytes)))
	if attributeBytes == 0 {
		return -1, errors.New("unable to size process attributes")
	}
	attributes := make([]byte, attributeBytes)
	attributeList := unsafe.Pointer(&attributes[0])
	if ok, _, callErr := initializeProcThreadAttributeList.Call(uintptr(attributeList), 1, 0, uintptr(unsafe.Pointer(&attributeBytes))); ok == 0 {
		return -1, callErr
	}
	defer deleteProcThreadAttributeList.Call(uintptr(attributeList))
	if ok, _, callErr := updateProcThreadAttribute.Call(uintptr(attributeList), 0, 0x20002, uintptr(unsafe.Pointer(&handles[0])), unsafe.Sizeof(handles), 0, 0); ok == 0 {
		return -1, callErr
	}
	application, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return -1, err
	}
	arguments := append([]string{path}, args...)
	for i := range arguments {
		arguments[i] = syscall.EscapeArg(arguments[i])
	}
	commandLine, err := syscall.UTF16PtrFromString(strings.Join(arguments, " "))
	if err != nil {
		return -1, err
	}
	environment := append([]string(nil), env...)
	sort.SliceStable(environment, func(i, j int) bool { return strings.ToUpper(environment[i]) < strings.ToUpper(environment[j]) })
	for _, item := range environment {
		if strings.IndexByte(item, 0) >= 0 {
			return -1, syscall.EINVAL
		}
	}
	block := utf16.Encode([]rune(strings.Join(environment, "\x00") + "\x00\x00"))
	si := startupInfoEx{AttributeList: attributeList}
	si.Cb = uint32(unsafe.Sizeof(si))
	si.Flags = syscall.STARTF_USESTDHANDLES | syscall.STARTF_USESHOWWINDOW
	si.ShowWindow = syscall.SW_HIDE
	si.StdInput, si.StdOutput, si.StdErr = handles[0], handles[1], handles[2]
	var child syscall.ProcessInformation
	const createSuspended = 0x4
	const extendedStartupInfoPresent = 0x80000
	err = syscall.CreateProcess(application, commandLine, nil, nil, true,
		createSuspended|syscall.CREATE_UNICODE_ENVIRONMENT|extendedStartupInfoPresent,
		&block[0], nil, &si.StartupInfo, &child)
	runtime.KeepAlive(attributes)
	runtime.KeepAlive(handles)
	if err != nil {
		return -1, err
	}
	defer syscall.CloseHandle(child.Process)
	defer syscall.CloseHandle(child.Thread)
	if ok, _, callErr := assignProcessToJobObject.Call(job, uintptr(child.Process)); ok == 0 {
		_ = syscall.TerminateProcess(child.Process, 1)
		return -1, callErr
	}
	if previous, _, callErr := resumeThread.Call(uintptr(child.Thread)); uint32(previous) == 0xffffffff {
		_, _, _ = terminateJobObject.Call(job, 1)
		return -1, callErr
	}
	// Close the parent's copies of child pipe endpoints now; otherwise EOF would
	// never arrive at the copy workers after the child exits.
	for _, f := range owned {
		if f == input || f == out || f == errOut {
			_ = f.Close()
		}
	}
	for i, handle := range handles {
		_ = syscall.CloseHandle(handle)
		handles[i] = 0
	}
	var copyResults []<-chan error
	for _, output := range outputs {
		result := make(chan error, 1)
		copyResults = append(copyResults, result)
		go func(output copiedOutput) { _, e := io.Copy(output.writer, output.reader); result <- e }(output)
	}
	done := make(chan error, 1)
	go func() { _, e := syscall.WaitForSingleObject(child.Process, syscall.INFINITE); done <- e }()
	select {
	case err = <-done:
	case <-ctx.Done():
		_, _, _ = terminateJobObject.Call(job, 1)
		err = <-done
	}
	// A successful child can leave descendants holding captured pipe ends. Kill
	// them before draining captured output, as their lifetime belongs to this call.
	_, _, _ = terminateJobObject.Call(job, 1)
	for _, result := range copyResults {
		if copyErr := <-result; err == nil && copyErr != nil {
			err = copyErr
		}
	}
	if ctx.Err() != nil {
		return -1, ctx.Err()
	}
	if err != nil {
		return -1, err
	}
	var exit uint32
	if err = syscall.GetExitCodeProcess(child.Process, &exit); err != nil {
		return -1, err
	}
	return int(exit), nil
}
