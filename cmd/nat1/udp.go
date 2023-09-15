package main

import (
	"github.com/spf13/cobra"
)

var udpStun string

func init() {
	rootCmd.AddCommand(udpCmd)
	udpCmd.Flags().StringVarP(&udpStun, "stun", "s", "stun.qq.com:3478", "STUN server address")
}

var udpCmd = &cobra.Command{
	Use:   "udp [port] [description]",
	Short: "Create a UDP mapping.",
	Long: "Create a UDP mapping. \n\n" +
		"If the port argument is specified, a NAT entry is automatically created on gateway.\n" +
		"Otherwise, you need to manually configure the mapping on gateway.",
	Args: cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		run("udp", udpStun, args)
	},
}
