// task-scheduler CLI は、YAMLで定義した開発計画の検証、日程計算、表示、候補反映を提供する。
package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/tzklflb/task-scheduler/internal/input"
	"github.com/tzklflb/task-scheduler/internal/output"
	"github.com/tzklflb/task-scheduler/internal/scheduling"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		writeUsage(stderr)
		return 2
	}
	if args[0] == "apply-candidate" {
		return runApplyCandidate(args[1:], stdout, stderr)
	}
	if args[0] != "check" && args[0] != "summary" && args[0] != "render" {
		writeUsage(stderr)
		return 2
	}

	command := args[0]
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(stderr)
	path := flags.String("input", "", "YAML input path")
	outputPath := flags.String("output", "", "output path")
	format := flags.String("format", "", "output format")
	var available values
	flags.Var(&available, "available", "override worker availability (WORKER=YYYY-MM-DD)")
	asOf := flags.String("as-of", "", "override planning base date (YYYY-MM-DD)")
	if err := flags.Parse(args[1:]); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "unexpected positional arguments")
		return 2
	}
	if *path == "" {
		fmt.Fprintln(stderr, "--input is required")
		return 2
	}
	if command == "summary" && *format != "json" {
		fmt.Fprintln(stderr, "summary requires --format json")
		return 2
	}
	if command == "render" && *format != "html" {
		fmt.Fprintln(stderr, "render requires --format html")
		return 2
	}
	if command == "render" && *outputPath == "" {
		fmt.Fprintln(stderr, "--output is required")
		return 2
	}

	result, issues := input.Load(*path, input.Overrides{Available: available, AsOf: *asOf})
	if len(issues) > 0 {
		fmt.Fprintln(stderr, "NG: 入力に問題があります")
		for _, issue := range issues {
			fmt.Fprintf(stderr, "  - %s\n", issue)
		}
		return 1
	}
	if command == "check" {
		fmt.Fprintf(stdout, "OK: %d タスク・整合性問題なし\n", len(result.Tasks))
		return 0
	}

	calculated := scheduling.Calculate(*result)
	view := output.Build(*result, calculated)
	if command == "render" {
		content, err := output.HTML(view)
		if err != nil {
			fmt.Fprintf(stderr, "出力を生成できません: %v\n", err)
			return 1
		}
		if err := os.WriteFile(*outputPath, []byte(content), 0o600); err != nil {
			fmt.Fprintf(stderr, "出力を書き込めません: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "OK: %s を生成（開発完了（バッファ込み） %s）\n", *outputPath, view.CompletionWithBuffer)
		return 0
	}

	summary, err := output.Summary(view)
	if err != nil {
		fmt.Fprintf(stderr, "JSON を生成できません: %v\n", err)
		return 1
	}
	if _, err := fmt.Fprintln(stdout, summary); err != nil {
		fmt.Fprintf(stderr, "JSON を書き込めません: %v\n", err)
		return 1
	}
	return 0
}

func writeUsage(stderr io.Writer) {
	fmt.Fprintln(stderr, "usage: task-scheduler check --input PATH [--available WORKER=YYYY-MM-DD] [--as-of YYYY-MM-DD]")
	fmt.Fprintln(stderr, "       task-scheduler summary --format json --input PATH [--available WORKER=YYYY-MM-DD] [--as-of YYYY-MM-DD]")
	fmt.Fprintln(stderr, "       task-scheduler render --format html --input PATH --output PATH [--available WORKER=YYYY-MM-DD] [--as-of YYYY-MM-DD]")
	fmt.Fprintln(stderr, "       task-scheduler apply-candidate --target PATH --candidate PATH (--expected-sha256 SHA256 | --expect-absent) [--allow-as-of-change]")
	fmt.Fprintln(stderr, "       apply-candidate must be run inside the target Git repository")
}

type values []string

func (values *values) String() string {
	return ""
}

func (values *values) Set(value string) error {
	*values = append(*values, value)
	return nil
}
