package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/eshu-hq/eshu/go/internal/buildinfo"
	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

const defaultFormat = "text"

func main() {
	if handled, err := printAdminStatusVersionFlag(os.Args[1:], os.Stdout); handled {
		if err != nil {
			_, _ = fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	if err := run(context.Background(), os.Args[1:], os.Stdout, os.Stderr, os.Getenv); err != nil {
		log.Fatal(err)
	}
}

func printAdminStatusVersionFlag(args []string, stdout io.Writer) (bool, error) {
	return buildinfo.PrintVersionFlag(args, stdout, "eshu-admin-status")
}

func run(
	parent context.Context,
	args []string,
	stdout io.Writer,
	stderr io.Writer,
	getenv func(string) string,
) error {
	db, err := runtimecfg.OpenPostgres(parent, getenv)
	if err != nil {
		return err
	}
	defer func() {
		_ = db.Close()
	}()

	return renderStatus(
		parent,
		args,
		stdout,
		stderr,
		postgres.NewStatusStore(postgres.SQLQueryer{DB: db}),
		func() time.Time { return time.Now().UTC() },
	)
}

func renderStatus(
	ctx context.Context,
	args []string,
	stdout io.Writer,
	stderr io.Writer,
	reader statuspkg.Reader,
	now func() time.Time,
) error {
	flags := flag.NewFlagSet("admin-status", flag.ContinueOnError)
	flags.SetOutput(stderr)

	format := flags.String("format", defaultFormat, "output format: text or json")
	if err := flags.Parse(args); err != nil {
		return err
	}

	report, err := statuspkg.LoadReport(ctx, reader, now(), statuspkg.DefaultOptions())
	if err != nil {
		return err
	}

	switch strings.ToLower(strings.TrimSpace(*format)) {
	case "text", "":
		_, err = fmt.Fprintln(stdout, statuspkg.RenderText(report))
		return err
	case "json":
		payload, err := statuspkg.RenderJSON(report)
		if err != nil {
			return fmt.Errorf("render json: %w", err)
		}
		_, err = fmt.Fprintln(stdout, string(payload))
		return err
	default:
		return fmt.Errorf("unsupported format %q", *format)
	}
}
