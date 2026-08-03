package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/Ad-Quanta/alcor-device-farm/internal/buildinfo"
)

func main() {
	version := flag.Bool("version", false, "print version information and exit")
	flag.Parse()

	if *version {
		fmt.Fprintln(os.Stdout, buildinfo.String("device-farm-server"))
		return
	}

	fmt.Fprintln(os.Stdout, "device-farm-server foundation is ready; HTTP startup is implemented in DF-002")
}
