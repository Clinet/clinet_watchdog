/*
//NOTES:
//When using this watchdog package, it is important that you use
// the imported pflag package from spf13 on GitHub to define extra args.
// This is how flags are currently processed, and is not expected to
// change. Wrappers are planned but too bloated for now.
//It should also expected that the watchdog will panic if it cannot
// spawn a new main process, so please use recover() accordingly.
*/
package watchdog

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/mitchellh/go-ps"
	"github.com/spf13/pflag"
)

var (
	Delay time.Duration = (time.Second * 5) //How often to check if the main process is alive
	Footer string //Displayed when program exits
	Header string //Displayed when program starts
	KillOldMain bool //Set to true if this should be the only existing watchdog parent, killing dangling processes with a matching os.Args[0]
	Signals []syscall.Signal = []syscall.Signal{syscall.SIGINT, syscall.SIGKILL}

	isMain bool
	mainArgs []string
	mainPID int = -1
	watchdogPID int = -1
)

//Parse returns false if it has to block and act as a watchdog until exit, or returns true if code execution should continue
func Parse() bool {
	pflag.BoolVar(&isMain, "isMain", false, "act as the main process instead of the watchdog process")
	pflag.IntVar(&watchdogPID, "watchdogPID", -1, "used as the exception when killing old main processes")
	pflag.ParseAll(parseFlag)
	pflag.Parse()

	if isMain {
		return true
	}

	if Header != "" {
		fmt.Println(Header)
	}

	sc := make(chan os.Signal, 1)
	for i := 0; i < len(Signals); i++ {
		signal.Notify(sc, Signals[i])
	}
	watchdogTicker := time.Tick(Delay)

	for {
		exitWatchdog := false
		select {
		case <-watchdogTicker:
			if !isProcessRunning(mainPID) {
				mainPID = spawnMain()
				if mainPID == -1 {
					panic("watchdog: failed to start new main process")
				}
			}
		case _, ok := <-sc:
			if ok {
				if isProcessRunning(mainPID) {
					mainProc, err := os.FindProcess(mainPID)
					if err == nil {
						_ = mainProc.Signal(syscall.SIGINT)
						waitProcess(mainPID)
					}
				}
				exitWatchdog = true
				break
			}
		}
		if exitWatchdog {
			break
		}
	}

	if Footer != "" {
		fmt.Println(Footer)
	}
	return false
}

func parseFlag(flag *pflag.Flag, value string) error {
	//We don't want to append self-defined args to mainCmd
	if flag.Name != "isMain" && flag.Name != "watchdogPID" {
		mainArgs = append(mainArgs, []string{"--" + flag.Name, value}...)
	}
	return nil
}

func spawnMain() int {
	if KillOldMain {
		processList, err := ps.Processes()
		if err == nil {
			for _, process := range processList {
				if process.Pid() != os.Getpid() && process.Pid() != watchdogPID && process.Executable() == filepath.Base(os.Args[0]) {
					oldProcess, err := os.FindProcess(process.Pid())
					if oldProcess != nil && err == nil {
						oldProcess.Signal(syscall.SIGKILL)
					}
				}
			}
		}
	}

	mainCmdArgs := []string{"--isMain", "--watchdogPID", fmt.Sprintf("%d", os.Getpid())}
	mainCmdArgs = append(mainCmdArgs, mainArgs...)

	mainCmd := exec.Command(os.Args[0], mainCmdArgs...)
	mainCmd.Stdout = os.Stdout
	mainCmd.Stderr = os.Stderr
	mainCmd.Stdin = os.Stdin

	err := mainCmd.Start()
	if err != nil {
		return -1
	}

	return mainCmd.Process.Pid
}

func isProcessRunning(pid int) bool {
	if pid < 0 {
		return false
	}

	process, err := ps.FindProcess(pid)
	if err != nil {
		return false
	}

	return process != nil
}

func waitProcess(pid int) {
	process, err := os.FindProcess(pid)
	if err != nil {
		return
	}
	_, _ = process.Wait()
}