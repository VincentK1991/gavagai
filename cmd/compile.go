package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/vincentk1991/gavagai/internal/pipeline"
	"github.com/vincentk1991/gavagai/internal/pretty"
)

// compileOptions holds the flag values for the compile command.
type compileOptions struct {
	model   string
	query   string
	dialect string
	pretty  bool
	explain bool
}

// newCompileCmd builds the compile subcommand: it runs the full
// parse→validate→plan→pushdown→emit pipeline and prints SQL to stdout.
func newCompileCmd() *cobra.Command {
	opts := &compileOptions{}

	cmd := &cobra.Command{
		Use:   "compile",
		Short: "Compile a query against a model and print SQL",
		Long: "compile runs an OSI semantic model and a query IR through the " +
			"gavagai pipeline and prints SQL for the target dialect to stdout.\n\n" +
			"By default the SQL is emitted as a single compact line; pass --pretty " +
			"for the multi-line form. Use --explain to print the query plan to stderr.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			res, err := pipeline.Compile(pipeline.Options{
				ModelPath: opts.model,
				QueryPath: opts.query,
				Dialect:   opts.dialect,
			})
			if err != nil {
				return err
			}

			// The plan goes to stderr so it never contaminates the SQL on stdout.
			if opts.explain {
				_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "PLAN: "+res.Plan)
			}

			out := res.SQL
			if !opts.pretty {
				out = pretty.Compact(res.SQL)
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), strings.TrimRight(out, "\n"))
			return err
		},
	}

	flags := cmd.Flags()
	flags.StringVarP(&opts.model, "model", "m", "", "path to the OSI semantic model file (YAML or JSON)")
	flags.StringVarP(&opts.query, "query", "q", "", "path to the query IR file (JSON)")
	flags.StringVarP(&opts.dialect, "dialect", "d", "", "target SQL dialect (bigquery | postgres)")
	flags.BoolVar(&opts.pretty, "pretty", false, "emit multi-line SQL (default: compact single line)")
	flags.BoolVar(&opts.explain, "explain", false, "print the query plan to stderr before the SQL")

	for _, name := range []string{"model", "query", "dialect"} {
		// MarkFlagRequired only errors if the flag does not exist; it does here.
		_ = cmd.MarkFlagRequired(name)
	}

	return cmd
}
