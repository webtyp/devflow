package main

import (
	"flag"
	"fmt"
	"os"

	"webtyp.com/devflow"
)

const (
	flagSet    = "s"
	flagTarget = "t"
)

func main() {
	fs := flag.NewFlagSet("devbackup", flag.ExitOnError)
	setCmd := fs.String(flagSet, "", "Set backup command (\"\" clears it)")
	setTarget := fs.String(flagTarget, "", "Set backup disk folder; missing = disk not connected (\"\" clears it)")
	getCmd := fs.Bool("g", false, "Get current backup command and disk")

	fs.Parse(os.Args[1:])

	// Visit sees only the flags actually given, so -s "" / -t "" clear the
	// setting instead of being mistaken for "not given".
	given := func(name string) bool {
		found := false
		fs.Visit(func(f *flag.Flag) { found = found || f.Name == name })
		return found
	}

	backup := devflow.NewDevBackup()

	if given(flagSet) {
		if err := backup.SetCommand(*setCmd); err != nil {
			fmt.Fprintf(os.Stderr, "Error setting backup command: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("✅ Backup command saved to ~/.bashrc")
		return
	}

	if given(flagTarget) {
		if err := backup.SetTarget(*setTarget); err != nil {
			fmt.Fprintf(os.Stderr, "Error setting backup disk: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("✅ Backup disk saved to ~/.bashrc")
		return
	}

	if *getCmd {
		command, err := backup.GetCommand()
		if err != nil {
			fmt.Fprintf(os.Stderr, "No backup command configured\n")
			os.Exit(1)
		}
		fmt.Println(command)
		if target, err := backup.GetTarget(); err == nil && target != "" {
			fmt.Println("disk: " + target)
		}
		return
	}

	// Default: execute backup
	msg, err := backup.Run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error executing backup: %v\n", err)
		os.Exit(1)
	}
	if msg != "" {
		fmt.Println(msg)
	}
}
