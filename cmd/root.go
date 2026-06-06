package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// version is the build version reported by the version command and the
// --version flag. It is overridable at build time via -ldflags.
var version = "dev"

// newRootCmd constructs the root command and attaches every subcommand. It is
// a constructor (rather than a package-level singleton) so tests can build an
// isolated command tree with its own flag and output state.
func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "gavagai",
		Short: "Compile an OSI semantic model and a query into SQL",
		Long: "gavagai is a deterministic query compiler. It accepts an OSI " +
			"semantic model and a query IR (JSON) and emits SQL for a target " +
			"dialect (BigQuery or PostgreSQL).",
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       version,
	}

	root.AddCommand(newCompileCmd())
	root.AddCommand(newValidateCmd())
	root.AddCommand(newVersionCmd())

	return root
}

// Execute runs the root command and exits non-zero on error.
func Execute() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "gavagai:", err)
		os.Exit(1)
	}
}
