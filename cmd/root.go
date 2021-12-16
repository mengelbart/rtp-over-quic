package cmd

import (
	"log"

	"github.com/spf13/cobra"
)

const addr = "localhost:4242"

var rootCmd = &cobra.Command{
	Use: "send",
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		log.Fatal(err)
	}
}
