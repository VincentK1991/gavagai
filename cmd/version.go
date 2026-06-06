package cmd

import "github.com/spf13/cobra"

// newVersionCmd builds the version subcommand, printing the build version.
func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the gavagai version",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cmd.Println(version)
			return nil
		},
	}
}
