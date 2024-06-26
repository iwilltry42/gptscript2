package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/acorn-io/cmd"
	"github.com/gptscript-ai/gptscript/pkg/assemble"
	"github.com/gptscript-ai/gptscript/pkg/builtin"
	"github.com/gptscript-ai/gptscript/pkg/input"
	"github.com/gptscript-ai/gptscript/pkg/loader"
	"github.com/gptscript-ai/gptscript/pkg/monitor"
	"github.com/gptscript-ai/gptscript/pkg/mvl"
	"github.com/gptscript-ai/gptscript/pkg/openai"
	"github.com/gptscript-ai/gptscript/pkg/runner"
	"github.com/gptscript-ai/gptscript/pkg/server"
	"github.com/gptscript-ai/gptscript/pkg/version"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

type (
	DisplayOptions monitor.Options
)

type GPTScript struct {
	runner.Options
	DisplayOptions
	Debug         bool   `usage:"Enable debug logging"`
	Quiet         bool   `usage:"No output logging" short:"q"`
	Output        string `usage:"Save output to a file, or - for stdout" short:"o"`
	Input         string `usage:"Read input from a file (\"-\" for stdin)" short:"f"`
	SubTool       string `usage:"Use tool of this name, not the first tool in file"`
	Assemble      bool   `usage:"Assemble tool to a single artifact, saved to --output"`
	ListModels    bool   `usage:"List the models available and exit"`
	ListTools     bool   `usage:"List built-in tools and exit"`
	Server        bool   `usage:"Start server"`
	ListenAddress string `usage:"Server listen address" default:"127.0.0.1:9090"`
}

func New() *cobra.Command {
	return cmd.Command(&GPTScript{})
}

func (r *GPTScript) Customize(cmd *cobra.Command) {
	cmd.Use = version.ProgramName + " [flags] PROGRAM_FILE [INPUT...]"
	cmd.Flags().SetInterspersed(false)
}

func (r *GPTScript) listTools(ctx context.Context) error {
	var lines []string
	for _, tool := range builtin.ListTools() {
		lines = append(lines, tool.String())
	}
	fmt.Println(strings.Join(lines, "\n---\n"))
	return nil
}

func (r *GPTScript) listModels(ctx context.Context) error {
	c, err := openai.NewClient(openai.Options(r.OpenAIOptions))
	if err != nil {
		return err
	}

	models, err := c.ListModules(ctx)
	if err != nil {
		return err
	}

	for _, model := range models {
		fmt.Println(model)
	}

	return nil
}

func (r *GPTScript) Pre(cmd *cobra.Command, args []string) error {
	if r.Quiet {
		if term.IsTerminal(int(os.Stdout.Fd())) {
			r.Quiet = false
		} else {
			r.Quiet = true
		}
	}

	if r.Debug {
		mvl.SetDebug()
	} else {
		mvl.SetSimpleFormat()
		if r.Quiet {
			mvl.SetError()
		}
	}
	return nil
}

func (r *GPTScript) Run(cmd *cobra.Command, args []string) error {
	if r.ListModels {
		return r.listModels(cmd.Context())
	}

	if r.ListTools {
		return r.listTools(cmd.Context())
	}

	if r.Server {
		s, err := server.New(server.Options{
			CacheOptions:  r.CacheOptions,
			OpenAIOptions: r.OpenAIOptions,
			ListenAddress: r.ListenAddress,
		})
		if err != nil {
			return err
		}
		return s.Start(cmd.Context())
	}

	if len(args) == 0 {
		return fmt.Errorf("scripts argument required")
	}

	prg, err := loader.Program(cmd.Context(), args[0], r.SubTool)
	if err != nil {
		return err
	}

	if r.Assemble {
		var out io.Writer = os.Stdout
		if r.Output != "" && r.Output != "-" {
			f, err := os.Create(r.Output)
			if err != nil {
				return fmt.Errorf("opening %s: %w", r.Output, err)
			}
			defer f.Close()
			out = f
		}

		return assemble.Assemble(cmd.Context(), prg, out)
	}

	runner, err := runner.New(r.Options, runner.Options{
		CacheOptions:  r.CacheOptions,
		OpenAIOptions: r.OpenAIOptions,
		MonitorFactory: monitor.NewConsole(monitor.Options(r.DisplayOptions), monitor.Options{
			DisplayProgress: !r.Quiet,
		}),
	})
	if err != nil {
		return err
	}

	toolInput, err := input.FromCLI(r.Input, args)
	if err != nil {
		return err
	}

	s, err := runner.Run(cmd.Context(), prg, os.Environ(), toolInput)
	if err != nil {
		return err
	}

	if r.Output != "" {
		err = os.WriteFile(r.Output, []byte(s), 0644)
		if err != nil {
			return err
		}
	} else {
		if !r.Quiet {
			if toolInput != "" {
				_, _ = fmt.Fprint(os.Stderr, "\nINPUT:\n\n")
				_, _ = fmt.Fprintln(os.Stderr, toolInput)
			}
			_, _ = fmt.Fprint(os.Stderr, "\nOUTPUT:\n\n")
		}
		fmt.Print(s)
		if !strings.HasSuffix(s, "\n") {
			fmt.Println()
		}
	}

	return nil
}
