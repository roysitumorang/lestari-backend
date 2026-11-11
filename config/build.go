package config

import "time"

// Set at build time with -ldflags "-X …/config.AppName=… -X …/config.Commit=… -X …/config.Build=…" (see Makefile).
var (
	Version = "0.1.0"
	AppName = "lestari"
	Commit,
	Build string
	Now = time.Now()
)
