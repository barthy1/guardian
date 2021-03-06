package dadoo

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"code.cloudfoundry.org/garden"
	"code.cloudfoundry.org/guardian/rundmc/runrunc"
	"code.cloudfoundry.org/lager"
	"github.com/cloudfoundry/gunk/command_runner"
	"github.com/kr/logfmt"
)

//go:generate counterfeiter . PidGetter
type PidGetter interface {
	Pid(pidFilePath string) (int, error)
}

type ExecRunner struct {
	dadooPath     string
	runcPath      string
	processIDGen  runrunc.UidGenerator
	pidGetter     PidGetter
	commandRunner command_runner.CommandRunner
}

func NewExecRunner(dadooPath, runcPath string, processIDGen runrunc.UidGenerator, pidGetter PidGetter, commandRunner command_runner.CommandRunner) *ExecRunner {
	return &ExecRunner{
		dadooPath:     dadooPath,
		runcPath:      runcPath,
		processIDGen:  processIDGen,
		pidGetter:     pidGetter,
		commandRunner: commandRunner,
	}
}

func (d *ExecRunner) Run(log lager.Logger, spec *runrunc.PreparedSpec, processesPath, handle string, tty *garden.TTYSpec, pio garden.ProcessIO) (p garden.Process, theErr error) {
	log = log.Session("execrunner")

	log.Info("start")
	defer log.Info("done")

	processID := d.processIDGen.Generate()

	processPath := filepath.Join(processesPath, processID)
	if err := os.MkdirAll(processPath, 0700); err != nil {
		return nil, err
	}

	fd3r, fd3w, err := os.Pipe()
	if err != nil {
		return nil, err
	}

	logr, logw, err := os.Pipe()
	if err != nil {
		return nil, err
	}

	defer fd3r.Close()
	defer logr.Close()

	process := newProcess(processID, processPath, filepath.Join(processPath, "pidfile"), d.pidGetter)
	process.mkfifos()
	if err != nil {
		return nil, err
	}

	var cmd *exec.Cmd
	if tty != nil {
		var rows, cols int
		if tty.WindowSize != nil {
			rows = tty.WindowSize.Rows
			cols = tty.WindowSize.Columns
		}

		cmd = exec.Command(d.dadooPath, "-tty", "-rows", strconv.Itoa(rows), "-cols", strconv.Itoa(cols), "-uid", strconv.Itoa(spec.HostUID), "-gid", strconv.Itoa(spec.HostGID), "exec", d.runcPath, processPath, handle)
	} else {
		cmd = exec.Command(d.dadooPath, "exec", d.runcPath, processPath, handle)
	}

	cmd.ExtraFiles = []*os.File{
		fd3w,
		logw,
	}

	encodedSpec, err := json.Marshal(spec.Process)
	if err != nil {
		return nil, err // this could *almost* be a panic: a valid spec should always encode (but out of caution we'll error)
	}

	cmd.Stdin = bytes.NewReader(encodedSpec)
	if err := d.commandRunner.Start(cmd); err != nil {
		return nil, err
	}

	go d.commandRunner.Wait(cmd) // wait on spawned process to avoid zombies

	fd3w.Close()
	logw.Close()

	if err := process.start(pio, tty); err != nil {
		return nil, err
	}

	log.Info("read-exit-fd")

	runcExitStatus := make([]byte, 1)
	fd3r.Read(runcExitStatus)

	log.Info("runc-exit-status", lager.Data{"status": runcExitStatus[0]})

	defer func() {
		theErr = processLogs(log, logr, theErr)
	}()

	if runcExitStatus[0] != 0 {
		return nil, fmt.Errorf("exit status %d", runcExitStatus[0])
	}

	return process, nil
}

func (d *ExecRunner) Attach(log lager.Logger, processID string, io garden.ProcessIO, processesPath string) (garden.Process, error) {
	processPath := filepath.Join(processesPath, processID)
	process := newProcess(processID, processPath, filepath.Join(processPath, "pidfile"), d.pidGetter)
	if err := process.start(io, &garden.TTYSpec{}); err != nil {
		return nil, err
	}

	return process, nil
}

type osSignal garden.Signal

func (s osSignal) OsSignal() syscall.Signal {
	switch garden.Signal(s) {
	case garden.SignalTerminate:
		return syscall.SIGTERM
	default:
		return syscall.SIGKILL
	}
}

type process struct {
	id                                           string
	stdin, stdout, stderr, exit, winsz, exitcode string
	ioWg                                         *sync.WaitGroup
	winszCh                                      chan garden.WindowSize
	cleanup                                      func() error

	*signaller
}

func newProcess(id, dir string, pidFilePath string, pidGetter PidGetter) *process {
	stdin, stdout, stderr, winsz, exit, exitcode := filepath.Join(dir, "stdin"),
		filepath.Join(dir, "stdout"),
		filepath.Join(dir, "stderr"),
		filepath.Join(dir, "winsz"),
		filepath.Join(dir, "exit"),
		filepath.Join(dir, "exitcode")

	return &process{
		id:       id,
		stdin:    stdin,
		stdout:   stdout,
		stderr:   stderr,
		winsz:    winsz,
		exit:     exit,
		exitcode: exitcode,
		ioWg:     &sync.WaitGroup{},
		winszCh:  make(chan garden.WindowSize, 5),
		cleanup: func() error {
			return os.RemoveAll(dir)
		},
		signaller: &signaller{
			pidFilePath: pidFilePath,
			pidGetter:   pidGetter,
		},
	}
}

func (p *process) ID() string {
	return p.id
}

func (p *process) mkfifos() error {
	for _, pipe := range []string{p.stdin, p.stdout, p.stderr, p.winsz, p.exit} {
		if err := syscall.Mkfifo(pipe, 0); err != nil {
			return err
		}
	}

	return nil
}

func (p process) start(pio garden.ProcessIO, ttySize *garden.TTYSpec) error {
	stdin, err := os.OpenFile(p.stdin, os.O_WRONLY, 0600)
	if err != nil {
		return err
	}

	if pio.Stdin != nil {
		go func() {
			io.Copy(stdin, pio.Stdin)
			stdin.Close()
		}()
	}

	stdout, err := os.Open(p.stdout)
	if err != nil {
		return err
	}

	if pio.Stdout != nil {
		p.ioWg.Add(1)
		go func() {
			io.Copy(pio.Stdout, stdout)
			p.ioWg.Done()
		}()
	}

	stderr, err := os.Open(p.stderr)
	if err != nil {
		return err
	}

	if pio.Stderr != nil {
		p.ioWg.Add(1)
		go func() {
			io.Copy(pio.Stderr, stderr)
			p.ioWg.Done()
		}()
	}

	return nil
}

func (p process) Wait() (int, error) {
	// open non-blocking incase exit pipe is already closed
	exit, err := os.OpenFile(p.exit, os.O_RDONLY|syscall.O_NONBLOCK, 0600)
	if err != nil {
		return 1, err
	}

	// go back to blocking mode so Read below blocks
	if err := syscall.SetNonblock(int(exit.Fd()), false); err != nil {
		return 1, err
	}

	buf := make([]byte, 1)
	exit.Read(buf)

	p.ioWg.Wait()

	if _, err := os.Stat(p.exitcode); os.IsNotExist(err) {
		return 1, fmt.Errorf("could not find the exitcode file for the process: %s", err.Error())
	}

	exitcode, err := ioutil.ReadFile(p.exitcode)
	if err != nil {
		return 1, err
	}

	if len(exitcode) == 0 {
		return 1, fmt.Errorf("the exitcode file is empty")
	}

	code, err := strconv.Atoi(string(exitcode))
	if err != nil {
		return 1, fmt.Errorf("failed to parse exit code: %s", err.Error())
	}

	if err := p.cleanup(); err != nil {
		return 1, err
	}

	return code, nil
}

func (p process) SetTTY(spec garden.TTYSpec) error {
	if spec.WindowSize == nil {
		return nil
	}

	winSize, err := os.OpenFile(p.winsz, os.O_WRONLY|syscall.O_NONBLOCK, 0600)
	if err != nil {
		return err
	}

	defer winSize.Close()
	return json.NewEncoder(winSize).Encode(spec.WindowSize)
}

func processLogs(log lager.Logger, logs io.Reader, err error) error {
	buff, readErr := ioutil.ReadAll(logs)
	if readErr != nil {
		return fmt.Errorf("start: read log file: %s", readErr)
	}

	forwardRuncLogsToLager(log, buff)

	if err != nil {
		return wrapWithErrorFromRuncLog(log, err, buff)
	}

	return nil
}

func forwardRuncLogsToLager(log lager.Logger, buff []byte) {
	parsedLogLine := struct{ Msg string }{}
	for _, logLine := range strings.Split(string(buff), "\n") {
		if err := logfmt.Unmarshal([]byte(logLine), &parsedLogLine); err == nil {
			log.Debug("runc", lager.Data{
				"message": parsedLogLine.Msg,
			})
		}
	}
}

func wrapWithErrorFromRuncLog(log lager.Logger, originalError error, buff []byte) error {
	parsedLogLine := struct{ Msg string }{}
	logfmt.Unmarshal(buff, &parsedLogLine)
	return fmt.Errorf("runc exec: %s: %s", originalError, parsedLogLine.Msg)
}

type signaller struct {
	pidFilePath string
	pidGetter   PidGetter
}

func (s *signaller) Signal(signal garden.Signal) error {
	pid, err := s.pidGetter.Pid(s.pidFilePath)
	if err != nil {
		return errors.New(fmt.Sprintf("fetching-pid: %s", err))
	}

	process, err := os.FindProcess(pid)
	if err != nil {
		return errors.New(fmt.Sprintf("finding-process: %s", err))
	}

	return process.Signal(osSignal(signal).OsSignal())
}
