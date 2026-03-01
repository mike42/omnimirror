package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

func main() {
	var (
		output string
		url    string
		suites []string
		comps  []string
		arches []string
		dryRun bool
	)

	rootCmd := &cobra.Command{
		Use:   "omnimirror-apt",
		Short: "Mirror an APT repository for offline use",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Printf("output:     %s\n", output)
			fmt.Printf("url:        %s\n", url)
			fmt.Printf("suites:     %s\n", strings.Join(suites, ", "))
			fmt.Printf("components: %s\n", strings.Join(comps, ", "))
			fmt.Printf("arches:     %s\n", strings.Join(arches, ", "))
			fmt.Printf("dry-run:    %t\n", dryRun)
			return nil
		},
	}

	rootCmd.Flags().StringVar(&output, "output", "", "Directory to save this repository to. This will be created if it does not yet exist.")
	rootCmd.Flags().StringVar(&url, "url", "", "Base URL to the repository, as it would appear in /etc/apt/sources.list. Only HTTP URLs are currently supported.")
	rootCmd.Flags().StringSliceVar(&suites, "suite", nil, "The suite or release code-name to download, as it would appear immediately after the URL in /etc/apt/sources.list - often stable/testing or the code-name for a release. This may be specified multiple times, in which case the releases are downloaded in the order specified.")
	rootCmd.Flags().StringSliceVar(&comps, "component", nil, "Components to consider for download, such as main, contrib non-free, universe, multiverse, etc. If this is not specified, then all components are downloaded. This may also be specified multiple times, in which case the requested components are downloaded in the order specified.")
	rootCmd.Flags().StringSliceVar(&arches, "arch", nil, "Architectures to consider for download, such as amd64, arm64, etc. If this is not specified, then all architectures are downloaded. This may also be specified multiple times, in which case the requested architectures will be downloaded in the order specified. The 'all' architecture, for packages which are not architecture-specific, is implicitly always included.")
	rootCmd.Flags().BoolVar(&dryRun, "dry-run", false, "Fetch metadata but do not download any packages. This option can be used to discover which architectures and components are available in the specified repository.")

	_ = rootCmd.MarkFlagRequired("output")
	_ = rootCmd.MarkFlagRequired("url")
	_ = rootCmd.MarkFlagRequired("suite")

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
