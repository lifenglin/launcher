package launcher

import (
	"context"
	micro_cli "github.com/micro/cli"
	cook_daemon "github.com/lifenglin/cook/daemon"
	"github.com/lifenglin/cook/hospice"
	cook_log "github.com/lifenglin/cook/log"
	cook_os "github.com/lifenglin/cook/os"
	cook_util "github.com/lifenglin/cook/util"
	nice_conf "gitlab.niceprivate.com/golang/nice/config"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
)

type Task struct {
	Name    string
	Usage   string
	Version string

	Flags []micro_cli.Flag

	Worker func(*Task)

	MicroCtx   *micro_cli.Context
	Ctx        context.Context
	Ctx_cancel context.CancelFunc
}

type Option func(t *Task)

func Launch(opts ...Option) {
	task := Task{
		Name:    "task-name",
		Usage:   "Usage of task",
		Version: "1.0.0",

		Flags: []micro_cli.Flag{
			&micro_cli.StringFlag{
				Name:  "conf",
				Usage: "config file path",
			},
			&micro_cli.StringFlag{
				Name:  "pid",
				Usage: "pid file path",
			},
			&micro_cli.BoolFlag{
				Name:  "d",
				Usage: "is daemonized",
			},
			&micro_cli.StringFlag{
				Name:  "node",
				Value: "single",
				Usage: "is support run more application",
			},
		},
	}
	for _, o := range opts {
		o(&task)
	}

	app := micro_cli.NewApp()
	app.Name = task.Name
	app.Version = task.Version
	app.Usage = task.Usage
	app.Flags = task.Flags
	app.Action = func(c *micro_cli.Context) error {
		task.Startup(c)
		return nil
	}
	app.Commands = append([]*micro_cli.Command{
		&micro_cli.Command{
			Name:  "stop",
			Usage: "shutdown program",
			Action: func(c *micro_cli.Context) error {
				task.Shutdown(c)
				return nil
			},
		},
		&micro_cli.Command{
			Name:  "restart",
			Usage: "restart program",
			Action: func(c *micro_cli.Context) error {
				task.Restart(c)
				return nil
			},
		},
	})
	app.RunAndExitOnError()
}
func (t *Task) Startup(c *micro_cli.Context) {
	t.MicroCtx = c
	t.Ctx, t.Ctx_cancel = context.WithCancel(context.TODO())

	if c.Bool("d") {
		if in_daemon, err := cook_daemon.Daemonize(); err != nil {
			cook_log.Fatalf("daemonize failed: %s", err)
			return
		} else if !in_daemon {
			os.Exit(0)
		}
	}

	conf_path := c.String("conf")
	node := c.String("node")
	if len(conf_path) > 0 {
		if err := nice_conf.Init_with_program(conf_path, c.App.Name, node); err != nil {
			cook_log.Fatalf("init config failed: %s", err)
			return
		}

		if err := cook_log.SetupLog(nice_conf.C_Log); err != nil {
			cook_log.Fatalf("setup amqp failed: %s", err)
			return
		}
	}

	t.install_signal()

	pid_file := c.String("pid")
	if err := cook_os.Write_pid(pid_file); err != nil {
		cook_log.Fatalf("write pid failed: %s", err)
		return
	}

	t.Worker(t)

	t.Ctx_cancel()
	hospice.Wait()
}

func (t *Task) Shutdown(c *micro_cli.Context) {
	if pid := cook_os.Read_pid(c.String("pid")); pid <= 0 {
		cook_log.Fatalf("pid file[%s] not exists or have no valid pid", c.String("pid"))
		return
	} else {
		if err := syscall.Kill(pid, syscall.SIGINT); err != nil {
			cook_log.Fatalf("send SIGINT to process<%d> failed: %s", pid, err)
		} else {
			cook_log.Fatalf("send SIGINT to process<%d> success", pid)
		}
	}
}

func (t *Task) Restart(c *micro_cli.Context) {
	if pid := cook_os.Read_pid(c.String("pid")); pid <= 0 {
		cook_log.Fatalf("pid file[%s] not exists or have no valid pid", c.String("pid"))
		return
	} else {
		if err := syscall.Kill(pid, syscall.SIGUSR1); err != nil {
			cook_log.Fatalf("send SIGUSR1 to process<%d> failed: %s", pid, err)
		} else {
			cook_log.Fatalf("send SIGUSR1 to process<%d> success", pid)
		}
	}
}

func (t *Task) install_signal() {
	sigCh := make(chan os.Signal)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGUSR1)

	go func() {
		var cancelWithRestart = false
		for {
			select {
			case <-t.Ctx.Done():
				if cancelWithRestart {
					cmd := &exec.Cmd{
						Path:   cook_util.ExecFile(),
						Args:   os.Args,
						Env:    cook_daemon.CleanEnviron(),
						Stdin:  os.Stdin,
						Stdout: os.Stdout,
						Stderr: os.Stderr,
					}
					if err := cmd.Start(); err != nil {
						cook_log.Fatalf("command %#v restart failed: %s", cmd, err)
					}
				}
				return
			case sig := <-sigCh:
				cook_log.Infof("receive signal: %s", sig.String())
				switch sig {
				case syscall.SIGINT, syscall.SIGTERM:
					t.Ctx_cancel()
				case syscall.SIGUSR1:
					cancelWithRestart = true
					t.Ctx_cancel()
				}
			}
		}

	}()
}

func Name(name string) Option {
	return func(t *Task) {
		t.Name = name
	}
}

func Usage(usage string) Option {
	return func(t *Task) {
		t.Usage = usage
	}
}

func Version(version string) Option {
	return func(t *Task) {
		t.Version = version
	}
}

func Flags(flags ...micro_cli.Flag) Option {
	return func(t *Task) {
		t.Flags = append(t.Flags, flags...)
	}
}

func Worker(worker func(*Task)) Option {
	return func(t *Task) {
		t.Worker = worker
	}
}
