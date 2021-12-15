package cmd

import (
	"github.com/spf13/cobra"
)

const addr = "localhost:4242"

var rootCmd = &cobra.Command{
	Use: "send",
}

func Execute() {
	rootCmd.Execute()
}
