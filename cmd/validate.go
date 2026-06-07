package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/vincentk1991/gavagai/internal/pipeline"
)

// validateOptions holds the flag values for the validate command.
type validateOptions struct {
	model string
}

// newValidateCmd builds the validate subcommand: it parses and structurally
// validates a semantic model, exiting non-zero with a list of problems on
// failure.
func newValidateCmd() *cobra.Command {
	opts := &validateOptions{}

	cmd := &cobra.Command{
		Use:   "validate",
		Short: "Validate an OSI semantic model file",
		RunE: func(cmd *cobra.Command, _ []string) error {
			m, err := pipeline.LoadAndValidateModel(opts.model)
			if err != nil {
				return err
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(),
				"model %q is valid (%d datasets, %d metrics)\n",
				m.Name, len(m.Datasets), len(m.Metrics))
			return err
		},
	}

	cmd.Flags().StringVarP(&opts.model, "model", "m", "", "path to the OSI semantic model file (YAML or JSON)")
	_ = cmd.MarkFlagRequired("model")

	return cmd
}
