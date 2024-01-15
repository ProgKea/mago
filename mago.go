package mago

import (
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"strings"
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

func (c Cmd) Run() bool {
	Info.Printf("CMD: %s\n", c.String())
	if err := c.cmd.Run(); err != nil {
		return false
	}
	return true
}

func (c Cmd) Start() bool {
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
	return Cmd{cmd}
}

func CmdSync(name string, arg ...string) (ok bool) {
	cmd := NewCmd(name, arg...)
	if !cmd.Run() {
		return false
	}
	return true
}

func CmdSyncCd(directory string, name string, arg ...string) bool {
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

var (
	Info           *log.Logger
	Warning        *log.Logger
	Error          *log.Logger
	InfoLogWriter  LogWriter
	ErrorLogWriter LogWriter
)

func init() {
	Info = log.New(os.Stdout, "[INFO] ", 0)
	Warning = log.New(os.Stdout, "[WARNING] ", 0)
	Error = log.New(os.Stdout, "[ERROR] ", 0)

	InfoLogWriter = LogWriter{Info}
	ErrorLogWriter = LogWriter{Error}
}
