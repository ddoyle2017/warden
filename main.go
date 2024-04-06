package main

import (
	"warden/command"
	"warden/data"
)

func main() {
	// Boot strap the app and initialize dependencies
	// Set up API clients
	// Pass dependencies to console

	data.OpenDatabase()
	// data.CreateModsTable()
	command.Execute()
}
