package mago

import (
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

type LogWriter struct {
	logger *log.Logger
}

type Runnable interface {
	Run() bool
}

func (lw LogWriter) Write(p []byte) (n int, err error) {
	lw.logger.Print(string(p))
	return len(p), nil
}

type Cmd struct {
	cmd *exec.Cmd
}

func (c Cmd) Run() (ok bool) {
	Info.Printf("CMD: %s\n", c.String())
	if err := c.cmd.Run(); err != nil {
		return false
	}
	return true
}

func (c Cmd) Start() (ok bool) {
	Info.Printf("CMD: %s\n", c.String())
	if err := c.cmd.Start(); err != nil {
		return false
	}
	return true
}

func (c Cmd) String() string {
	return strings.Join(c.cmd.Args, " ")
}

func (c Cmd) Wait() error {
	return c.cmd.Wait()
}

func (c Cmd) SetDirectory(directory string) {
	c.cmd.Dir = directory
}

func (c Cmd) SetStdout(stdout io.Writer) {
	c.cmd.Stdout = stdout
}

func (c Cmd) SetStderr(stderr io.Writer) {
	c.cmd.Stderr = stderr
}

func (c Cmd) SetStdin(stdin io.Reader) {
	c.cmd.Stdin = stdin
}

func (c Cmd) Process() *os.Process {
	return c.cmd.Process
}

func (c Cmd) KillGroup() (ok bool) {
	pid := c.Process().Pid
	pgid, err := syscall.Getpgid(pid)
	if err != nil {
		Error.Printf("Could not get pgid of process with id: %d: %v\n", pid, err)
		return false
	}

	if err := syscall.Kill(-pgid, syscall.SIGTERM); err != nil {
		Error.Printf("Could not kill pgid: %d: %v\n", pgid, err)
		return false
	}

	cmd.Wait()
	return true
}

type PipedCmds struct {
	cmds []Cmd
}

func NewPipedCmds(cmds ...Cmd) PipedCmds {
	return PipedCmds{cmds}
}

func (p PipedCmds) String() string {
	var cmdStrings []string
	for _, cmd := range p.cmds {
		cmdStrings = append(cmdStrings, cmd.String())
	}
	return strings.Join(cmdStrings, " | ")
}

func (p PipedCmds) Run() (ok bool) {
	Info.Printf("CMD: %s\n", p.String())
	return pipe(p.cmds...) == nil
}

// https://stackoverflow.com/questions/10781516/how-to-pipe-several-commands-in-go
func pipe(cmds ...Cmd) (err error) {
	pipeStack := make([]*io.PipeWriter, len(cmds)-1)
	i := 0
	for ; i < len(cmds)-1; i++ {
		stdinPipe, stdoutPipe := io.Pipe()
		cmds[i].SetStdout(stdoutPipe)
		cmds[i+1].SetStdin(stdinPipe)
		pipeStack[i] = stdoutPipe
	}
	if err := call(cmds, pipeStack); err != nil {
		return err
	}
	return err
}

func call(stack []Cmd, pipes []*io.PipeWriter) (err error) {
	if stack[0].Process() == nil {
		if !stack[0].Start() {
			return err
		}
	}
	if len(stack) > 1 {
		if !stack[1].Start() {
			return err
		}
		defer func() {
			if err == nil {
				pipes[0].Close()
				err = call(stack[1:], pipes[1:])
			}
		}()
	}

	err = stack[0].Wait()
	return
}

func NewCmd(name string, arg ...string) Cmd {
	cmd := exec.Command(name, arg...)
	cmd.Stdout = InfoLogWriter
	cmd.Stderr = ErrorLogWriter
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	return Cmd{cmd}
}

func CmdSync(name string, arg ...string) (ok bool) {
	cmd := NewCmd(name, arg...)
	if !cmd.Run() {
		return false
	}
	return true
}

func CmdSyncCd(directory string, name string, arg ...string) (ok bool) {
	cmd := NewCmd(name, arg...)
	// TODO(KD): Maybe log that the directory was changed
	cmd.SetDirectory(directory)
	return cmd.Run()
}

func CmdAsync(name string, arg ...string) (cmd Cmd, ok bool) {
	cmd = NewCmd(name, arg...)

	if !cmd.Start() {
		return cmd, false
	}

	return cmd, true
}

func SearchProgram(name string) (programPath string, ok bool) {
	programPath, err := exec.LookPath(name)
	if err != nil {
		Error.Println(err.Error())
		return programPath, false
	}
	return programPath, true
}

func YesOrNoPrompt(text string) bool {
	fmt.Print(text)
	var answer string
	fmt.Scanln(&answer)
	answer = strings.ToLower(answer)
	return answer == "y" || answer == "yes"
}

func MaybeInstallProgram(name string, installCmd Runnable) (ok bool) {
	_, ok = SearchProgram(name)
	if !ok {
		if YesOrNoPrompt(fmt.Sprintf("%q is not installed. Do you want to install it? (y/n): ", name)) {
			return installCmd.Run()
		} else {
			return false
		}
	}
	return ok
}

func refreshWatchFile() (ok bool) {
	var err error
	watchFile, err = os.CreateTemp(os.TempDir(), "mago")
	if err != nil {
		Error.Printf("Could not create temp file for watch mode: %v\n", err)
		return false
	}
	return true
}

func WatchFiles(patterns []string, ignoredPatterns []string) bool {
	if watchFile == nil {
		if !refreshWatchFile() {
			return false
		}
	}

	watchedFileChanged := false
	err := filepath.Walk(".", func(path string, info fs.FileInfo, err error) error {
		if err != nil {
			return err
		}

		for _, ignoredPattern := range ignoredPatterns {
			ignoredPath, err := filepath.Match(ignoredPattern, path)
			if err == nil && ignoredPath {
				return nil
			}
			ignoredName, err := filepath.Match(ignoredPattern, info.Name())
			if err == nil && ignoredName {
				return nil
			}
		}

		patternMatched := false
		for _, pattern := range patterns {
			matched, _ := filepath.Match(pattern, info.Name())
			if matched {
				patternMatched = true
				break
			}
		}
		if patternMatched {
			watchFileInfo, err := watchFile.Stat()
			if err != nil {
				Error.Printf("Could not stat watch file: %v\n", err)
				return fs.SkipAll
			}
			if info.ModTime().After(watchFileInfo.ModTime()) {
				watchedFileChanged = true
				refreshWatchFile()
				return fs.SkipAll
			}
		}

		return nil
	})

	if err != nil {
		Error.Printf("Could not walk current directory: %v\n", err)
	}

	return watchedFileChanged
}

func Watch(patterns, ignoredPatterns []string, name string, args ...string) {
	cmd, _ := CmdAsync(name, args...)
	for {
		if WatchFiles(patterns, ignoredPatterns) {
			cmd.KillGroup()
			cmd, _ = CmdAsync(name, args...)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

var (
	Info           *log.Logger
	Warning        *log.Logger
	Error          *log.Logger
	InfoLogWriter  LogWriter
	ErrorLogWriter LogWriter
	watchFile      *os.File
)

func init() {
	Info = log.New(os.Stdout, "[INFO] ", 0)
	Warning = log.New(os.Stdout, "[WARNING] ", 0)
	Error = log.New(os.Stdout, "[ERROR] ", 0)

	InfoLogWriter = LogWriter{Info}
	ErrorLogWriter = LogWriter{Error}
}
