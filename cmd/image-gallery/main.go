// Command image-gallery runs one role of the application:
//
//	image-gallery serve     web UI and API (default)
//	image-gallery worker    asynchronous image processing (queue consumer)
//	image-gallery loadgen   load generator
package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/KimMachineGun/automemlimit/memlimit"

	"image-gallery/internal/app"
)

func init() {
	// GOMEMLIMIT = 90 % of this container's cgroup limit: the GC works harder
	// before the kernel OOM-kills. Each container has its own cgroup, so the
	// web and worker containers each get their own limit.
	if _, err := memlimit.Set(
		memlimit.WithRatio(0.9),
		memlimit.WithProvider(memlimit.FromCgroup),
		memlimit.WithLogger(slog.Default()),
	); err != nil {
		slog.Warn("Failed to set automatic memory limit", "error", err)
	}
}

// cmdServe is the default subcommand, used both to pick it and to compare
// against it below.
const cmdServe = "serve"

const usageText = `Usage: image-gallery <command> [flags]

Commands:
  serve     web UI and API (default)
  worker    asynchronous image processing
  loadgen   load generator (image-gallery loadgen -h)
`

func main() { os.Exit(run(os.Args[1:])) }

func run(args []string) int {
	cmd := cmdServe
	if len(args) > 0 {
		//nolint:staticcheck // args feeds Task 13 (worker) / Task 15 (loadgen) subcommand flags
		cmd, args = args[0], args[1:]
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	switch cmd {
	case cmdServe:
		return exitCode(app.RunServe(ctx))
	case "worker":
		return exitCode(app.RunWorker(ctx))
	case "help", "-h", "--help":
		usage(os.Stdout)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", cmd)
		usage(os.Stderr)
		return 2
	}
}

func exitCode(err error) int {
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	return 0
}

func usage(w io.Writer) {
	_, _ = fmt.Fprint(w, usageText) //nolint:errcheck // writing usage text; nothing to recover from
}
