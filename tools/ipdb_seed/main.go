package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/user/anyconnect-split/internal/ipdb"
)

func main() {
	outDir := flag.String("out", "data", "directory to write china_ip_list.txt")
	flag.Parse()

	cidrs, err := ipdb.New(*outDir).Update()
	if err != nil {
		fmt.Fprintf(os.Stderr, "update IP database: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Wrote %d CIDRs to %s\n", len(cidrs), *outDir)
}
