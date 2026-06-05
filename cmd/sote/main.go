package main

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	daemonMode := flag.Bool("daemon", false, "run as daemon only")
	flag.Parse()

	if *daemonMode {
		fmt.Println("Daemon mode not yet implemented")
	} else {
		fmt.Println("SOTE - Decentralized Messenger")
		fmt.Println("TUI mode not yet implemented")
	}
	os.Exit(0)
}
