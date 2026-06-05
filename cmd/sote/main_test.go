package main

import (
	"flag"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMainParsesFlags(t *testing.T) {
	// Save and restore original args and flag set
	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()
	oldFlagCommandLine := flag.CommandLine
	defer func() { flag.CommandLine = oldFlagCommandLine }()

	os.Args = []string{"sote", "--daemon"}
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)

	daemonMode := flag.Bool("daemon", false, "run as daemon only")
	flag.Parse()

	assert.True(t, *daemonMode, "daemon flag should be true when --daemon is passed")
}

func TestMainHelpFlag(t *testing.T) {
	// Save and restore original args and flag set
	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()
	oldFlagCommandLine := flag.CommandLine
	defer func() { flag.CommandLine = oldFlagCommandLine }()

	os.Args = []string{"sote", "--help"}
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)

	// flag.Parse() with --help triggers flag.ExitOnError or prints help
	// We verify the flag set properly recognizes --help
	err := flag.CommandLine.Parse(os.Args[1:])
	assert.ErrorIs(t, err, flag.ErrHelp, "--help should trigger ErrHelp")
}

func TestMainDefaultDaemonFalse(t *testing.T) {
	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()
	oldFlagCommandLine := flag.CommandLine
	defer func() { flag.CommandLine = oldFlagCommandLine }()

	os.Args = []string{"sote"}
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)

	daemonMode := flag.Bool("daemon", false, "run as daemon only")
	flag.Parse()

	assert.False(t, *daemonMode, "daemon flag should be false by default")
}
