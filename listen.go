package main

import (
	"flag"

	"github.com/albert-tarly/dungerra-api/internal/config"
)

// listenSettings is where and how the HTTP server listens.
type listenSettings struct {
	Host, Port        string
	CertFile, KeyFile string
	UseTLS            bool
}

// explicitFlags returns the names of the flags given on the command line.
func explicitFlags() map[string]bool {
	set := map[string]bool{}
	flag.Visit(func(f *flag.Flag) { set[f.Name] = true })
	return set
}

// resolveListenSettings takes each setting from its flag when that flag was
// given explicitly, and otherwise from the loaded configuration. The config is
// read after .env, so this must run after config.LoadConfig.
func resolveListenSettings(explicit map[string]bool, flags listenSettings, cfg *config.Config) listenSettings {
	s := listenSettings{
		Host:     cfg.Host,
		Port:     cfg.Port,
		CertFile: cfg.TLSCert,
		KeyFile:  cfg.TLSKey,
		UseTLS:   cfg.UseTLS,
	}
	if explicit["host"] {
		s.Host = flags.Host
	}
	if explicit["port"] {
		s.Port = flags.Port
	}
	if explicit["cert"] {
		s.CertFile = flags.CertFile
	}
	if explicit["key"] {
		s.KeyFile = flags.KeyFile
	}
	if explicit["tls"] {
		s.UseTLS = flags.UseTLS
	}
	return s
}
