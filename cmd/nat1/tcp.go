package main

import (
	"github.com/spf13/cobra"
)

var (
	tcpStun      string
	keepaliveUrl string
)

func init() {
	rootCmd.AddCommand(tcpCmd)
	tcpCmd.Flags().StringVarP(&tcpStun, "stun", "s", "stun.xiaoyaoyou.xyz:3478", "STUN server address")
	tcpCmd.Flags().StringVarP(&keepaliveUrl, "url", "u", "http://connectivitycheck.platform.hicloud.com/generate_204", "url to keep connection alive")
}

var tcpCmd = &cobra.Command{
	Use:   "tcp [port]",
	Short: "Create a TCP mapping.",
	Long: "Create a TCP mapping. \n\n" +
		"If the port argument is specified, a NAT entry is automatically created on gateway.\n" +
		"Otherwise, you need to manually configure the mapping on gateway.",
	Args: cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		run("tcp", tcpStun, args)
	},
}
