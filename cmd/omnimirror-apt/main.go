package main

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "omnimirror-apt",
		Short: "Mirror an APT repository for offline use",
	}

	rootCmd.AddCommand(newDownloadCmd())

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func newDownloadCmd() *cobra.Command {
	var (
		output string
		url    string
		suites []string
		comps  []string
		archs  []string
		dryRun bool
	)

	cmd := &cobra.Command{
		Use:   "download",
		Short: "Download an APT repository for offline use",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Printf("output:     %s\n", output)
			fmt.Printf("url:        %s\n", url)
			fmt.Printf("suites:     %s\n", strings.Join(suites, ", "))
			fmt.Printf("components: %s\n", strings.Join(comps, ", "))
			fmt.Printf("archs:     %s\n", strings.Join(archs, ", "))
			fmt.Printf("dry-run:    %t\n", dryRun)
			// Usage is ok, suppress printing it if we get failures after this
			cmd.SilenceUsage = true
			return errors.New("not implemented")
		},
	}

	cmd.Flags().StringVar(&output, "output", "", "Directory to save this repository to. This will be created if it does not yet exist.")
	cmd.Flags().StringVar(&url, "url", "", "Base URL to the repository, as it would appear in /etc/apt/sources.list. Only HTTP URLs are currently supported.")
	cmd.Flags().StringSliceVar(&suites, "suite", nil, "The suite or release code-name to download, as it would appear immediately after the URL in /etc/apt/sources.list - often stable/testing or the code-name for a release. This may be specified multiple times, in which case the releases are downloaded in the order specified.")
	cmd.Flags().StringSliceVar(&comps, "component", nil, "Components to consider for download, such as main, contrib non-free, universe, multiverse, etc. If this is not specified, then all components are downloaded. This may also be specified multiple times, in which case the requested components are downloaded in the order specified.")
	cmd.Flags().StringSliceVar(&archs, "arch", nil, "Architectures to consider for download, such as amd64, arm64, etc. If this is not specified, then all architectures are downloaded. This may also be specified multiple times, in which case the requested architectures will be downloaded in the order specified. The 'all' architecture, for packages which are not architecture-specific, is implicitly always included.")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Fetch metadata but do not download any packages. This option can be used to discover which architectures and components are available in the specified repository.")
	_ = cmd.MarkFlagRequired("output")
	_ = cmd.MarkFlagRequired("url")
	_ = cmd.MarkFlagRequired("suite")

	return cmd
}
