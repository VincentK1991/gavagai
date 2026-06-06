package cmd

import "github.com/spf13/cobra"

// validateOptions holds the flag values for the validate command.
type validateOptions struct {
	model string
}

// newValidateCmd builds the validate subcommand. Model validation is wired in
// a later phase; for now it returns ErrNotImplemented.
func newValidateCmd() *cobra.Command {
	opts := &validateOptions{}

	cmd := &cobra.Command{
		Use:   "validate",
		Short: "Validate an OSI semantic model file",
		RunE: func(_ *cobra.Command, _ []string) error {
			return ErrNotImplemented
		},
	}

	cmd.Flags().StringVarP(&opts.model, "model", "m", "", "path to the OSI semantic model file (YAML or JSON)")

	return cmd
}
