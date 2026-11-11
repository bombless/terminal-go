package main

import (
	"flag"
	"fmt"
	"io"
	"strings"
)

// startupOptions describes an optional session to open immediately after the
// GUI has been created. Commands remain a single argument at the process
// boundary; neither mode routes them through a local shell.
type startupOptions struct {
	sshAddress        string
	sshCommand        string
	powerShell        bool
	powerShellCommand string
}

func parseStartupOptions(args []string) (startupOptions, error) {
	var opts startupOptions
	fs := flag.NewFlagSet("terminal", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&opts.sshAddress, "ssh", "", "SSH address to connect after startup (user@host[:port])")
	fs.StringVar(&opts.sshCommand, "ssh-command", "", "remote command to execute after SSH login")
	fs.BoolVar(&opts.powerShell, "powershell", false, "start a local PowerShell session after startup")
	fs.StringVar(&opts.powerShellCommand, "powershell-command", "", "command to execute in the startup PowerShell session")
	if err := fs.Parse(args); err != nil {
		return startupOptions{}, err
	}
	if fs.NArg() != 0 {
		return startupOptions{}, fmt.Errorf("unexpected positional arguments: %s", strings.Join(fs.Args(), " "))
	}
	if opts.sshAddress != "" && opts.powerShell {
		return startupOptions{}, fmt.Errorf("--ssh and --powershell cannot be used together")
	}
	if opts.sshCommand != "" && opts.sshAddress == "" {
		return startupOptions{}, fmt.Errorf("--ssh-command requires --ssh")
	}
	if opts.powerShellCommand != "" && !opts.powerShell {
		return startupOptions{}, fmt.Errorf("--powershell-command requires --powershell")
	}
	return opts, nil
}
