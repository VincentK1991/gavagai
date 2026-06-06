package cmd

import "github.com/spf13/cobra"

// compileOptions holds the flag values for the compile command.
type compileOptions struct {
	model   string
	query   string
	dialect string
	pretty  bool
	explain bool
}

// newCompileCmd builds the compile subcommand. The pipeline is wired in a
// later phase; for now it returns ErrNotImplemented.
func newCompileCmd() *cobra.Command {
	opts := &compileOptions{}

	cmd := &cobra.Command{
		Use:   "compile",
		Short: "Compile a query against a model and print SQL",
		RunE: func(_ *cobra.Command, _ []string) error {
			return ErrNotImplemented
		},
	}

	flags := cmd.Flags()
	flags.StringVarP(&opts.model, "model", "m", "", "path to the OSI semantic model file (YAML or JSON)")
	flags.StringVarP(&opts.query, "query", "q", "", "path to the query IR file (JSON)")
	flags.StringVarP(&opts.dialect, "dialect", "d", "", "target SQL dialect (bigquery | postgres)")
	flags.BoolVar(&opts.pretty, "pretty", false, "pretty-print the output SQL")
	flags.BoolVar(&opts.explain, "explain", false, "print the query plan to stderr before the SQL")

	return cmd
}
