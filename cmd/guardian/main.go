package main

import (
	"fmt"
	"os"

	"code.cloudfoundry.org/guardian/guardiancmd"
	"github.com/jessevdk/go-flags"
)

func main() {
	cmd := &guardiancmd.GuardianCommand{}

	parser := flags.NewParser(cmd, flags.Default)
	parser.NamespaceDelimiter = "-"

	args, err := parser.Parse()
	if err != nil {
		os.Exit(1)
	}

	err = cmd.Execute(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
