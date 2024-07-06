package main

import (
	"fmt"
	"time"

	"github.com/SheldonXLD/zaplogback"
	"go.uber.org/zap"
)

func main() {

	err := zaplogback.RegisterLogbackEncoder("zaplogback", `%date{%Y-%m-%d %H:%M:%S.%3f} %level{upper} %caller{} %x{tid:["tid":$0]} %message %fields`)

	if err != nil {
		fmt.Println(err)
		return
	}

	cfg := zap.Config{
		Level:       zap.NewAtomicLevelAt(zap.InfoLevel),
		Development: false,
		Sampling: &zap.SamplingConfig{
			Initial:    100,
			Thereafter: 100,
		},
		Encoding:         "zaplogback",
		EncoderConfig:    zap.NewProductionEncoderConfig(),
		OutputPaths:      []string{"stdout"},
		ErrorOutputPaths: []string{"stderr"},
	}
	logger, _ := cfg.Build()

	logger.Info("just message", zap.String("tid", "field need formatted"), zap.String("fields", "fields exclude of %x{...}"))

	date_format := zaplogback.StrftimeFormatLayout("%Y-%m-%d %H:%M:%S.%3f %a %A %w %b %B %y %I %p %z %Z %j")
	fmt.Println(date_format)
	now := time.Now().Format(date_format)
	fmt.Println(now)
}
