package main

import (
	"fmt"
	"os"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

func msgFormatter(msg interface{}) string {
	return fmt.Sprintf("msg=%q", msg)
}
func lvlFormatter(level interface{}) string {
	if level == nil {
		return ""
	}
	return fmt.Sprintf("level=%s", level)
}

func initLogger(level string) error {
	log.Logger = log.Logger.Output(zerolog.ConsoleWriter{
		Out:           os.Stderr,
		NoColor:       false,
		FormatLevel:   lvlFormatter,
		FormatMessage: msgFormatter,
		FormatTimestamp: func(interface{}) string {
			return ""
		},
	})

	l, err := zerolog.ParseLevel(level)
	if err != nil {
		return err
	}
	zerolog.SetGlobalLevel(l)

	return nil
}
