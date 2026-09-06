package main

import (
	"flag"
	"fmt"
	"os"

	"webtyp.com/devflow"
)

func main() {
	fs := flag.NewFlagSet("devbackup", flag.ExitOnError)
	setCmd := fs.String("s", "", "Set backup command")
	getCmd := fs.Bool("g", false, "Get current backup command")

	fs.Parse(os.Args[1:])

	backup := devflow.NewDevBackup()

	// Handle -s flag (set command)
	if *setCmd != "" {
		if err := backup.SetCommand(*setCmd); err != nil {
			fmt.Fprintf(os.Stderr, "Error setting backup command: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("✅ Backup command saved to ~/.bashrc")
		return
	}

	// Handle -g flag (get command)
	if *getCmd {
		command, err := backup.GetCommand()
		if err != nil {
			fmt.Fprintf(os.Stderr, "No backup command configured\n")
			os.Exit(1)
		}
		fmt.Println(command)
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
