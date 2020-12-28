package launcher

import (
	"context"
	micro_cli "github.com/micro/cli"
	"github.com/micro/go-grpc"
	"github.com/micro/go-micro"
	"github.com/micro/go-micro/cmd"
	_ "github.com/micro/go-plugins/registry/etcd"
	cook_daemon "github.com/lifenglin/cook/daemon"
	"github.com/lifenglin/cook/hospice"
	cook_log "github.com/lifenglin/cook/log"
	cook_os "github.com/lifenglin/cook/os"
	nice_conf "gitlab.niceprivate.com/golang/nice/config"
	"os"
	"time"
)

type Task struct {
	Name    string
	Usage   string
	Version string

	MicroCtx   *micro_cli.Context
	Ctx        context.Context
	Ctx_cancel context.CancelFunc

	Flags  []micro_cli.Flag
	Worker func(t *Task)

	Service micro.Service
}

type Option func(t *Task)

func Launch(opts ...Option) {
	task := Task{
		Name:    "task-name",
		Usage:   "Usage of task",
		Version: "1.0.0",

		Flags: []micro_cli.Flag{
			micro_cli.StringFlag{
				Name:  "conf",
				Usage: "config file path",
			},
			micro_cli.StringFlag{
				Name:  "pid",
				Usage: "pid file path",
			},
			micro_cli.BoolFlag{
				Name:  "d",
				Usage: "is daemonized",
			},
		},
	}
	for _, o := range opts {
		o(&task)
	}

	app := cmd.App()
	app.Flags = append(app.Flags, task.Flags...)
	prev_before := app.Before
	app.Before = func(ctx *micro_cli.Context) error {
		task.MicroCtx = ctx
		task.Ctx, task.Ctx_cancel = context.WithCancel(context.TODO())

		if ctx.Bool("d") {
			if in_daemon, err := cook_daemon.Daemonize(); err != nil {
				cook_log.Fatalf("daemonize failed: %s", err)
				return err
			} else if !in_daemon {
				os.Exit(0)
			}
		}

		conf_path := ctx.String("conf")
		if len(conf_path) > 0 {
			if err := nice_conf.Init_with_program(conf_path, ctx.App.Name, "single"); err != nil {
				cook_log.Fatalf("init config failed: %s", err)
				return err
			}

			if err := cook_log.SetupLog(nice_conf.C_Log); err != nil {
				cook_log.Fatalf("setup amqp failed: %s", err)
				return err
			}
		}

		pid_file := ctx.String("pid")
		if err := cook_os.Write_pid(pid_file); err != nil {
			cook_log.Fatalf("write pid failed: %s", err)
			return err
		}

		return prev_before(ctx)
	}
	app.Action = func(ctx *micro_cli.Context) {
		task.Service = grpc.NewService(
			micro.Name(task.Name),
			micro.Version(task.Version),
			micro.RegisterTTL(time.Second*30),
			micro.RegisterInterval(time.Second*10),
		)

		task.Worker(&task)

		if err := task.Service.Run(); err != nil {
			cook_log.Fatalf("Service[%s} termination with error: %s", task.Name, err)
		}

		hospice.Wait()
	}

	cmd.Init(
		cmd.Name(task.Name),
		cmd.Description(task.Usage),
		cmd.Version(task.Version),
	)
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

func Worker(worker func(task *Task)) Option {
	return func(t *Task) {
		t.Worker = worker
	}
}
