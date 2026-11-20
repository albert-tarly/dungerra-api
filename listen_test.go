package main

import (
	"testing"

	"github.com/albert-tarly/dungerra-api/internal/config"
)

func TestResolveListenSettings(t *testing.T) {
	// As loaded from env/.env
	cfg := &config.Config{Host: "0.0.0.0", Port: "9090", TLSCert: "env.pem", TLSKey: "env.key", UseTLS: true}
	flags := listenSettings{Host: "127.0.0.1", Port: "0", CertFile: "flag.pem", KeyFile: "flag.key", UseTLS: false}

	t.Run("no flags: config (including .env) wins", func(t *testing.T) {
		got := resolveListenSettings(map[string]bool{}, flags, cfg)
		want := listenSettings{Host: "0.0.0.0", Port: "9090", CertFile: "env.pem", KeyFile: "env.key", UseTLS: true}
		if got != want {
			t.Errorf("got %+v, want %+v", got, want)
		}
	})

	t.Run("explicit flags override", func(t *testing.T) {
		explicit := map[string]bool{"host": true, "port": true, "cert": true, "key": true, "tls": true}
		if got := resolveListenSettings(explicit, flags, cfg); got != flags {
			t.Errorf("got %+v, want %+v", got, flags)
		}
	})

	t.Run("explicit -tls=false disables env TLS", func(t *testing.T) {
		got := resolveListenSettings(map[string]bool{"tls": true}, flags, cfg)
		if got.UseTLS {
			t.Error("UseTLS stayed on despite -tls=false")
		}
		if got.Port != "9090" {
			t.Errorf("Port = %q, want the config value", got.Port)
		}
	})
}
