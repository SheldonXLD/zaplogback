package main

import (
	"testing"

	"github.com/SheldonXLD/zaplogback"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func BenchmarkMylogger(b *testing.B) {
	log_format := `%date{%Y-%m-%d %H:%M:%S.%3f} %level{lower} %caller %x{tid:["tid":$0]} %message %fields`
	zap.RegisterEncoder("custom", func(encoderConfig zapcore.EncoderConfig) (zapcore.Encoder, error) {
		return zaplogback.NewZaplogbackEncoder(encoderConfig, log_format), nil
	})

	cfg := zap.Config{
		Level:       zap.NewAtomicLevelAt(zap.InfoLevel),
		Development: false,
		Sampling: &zap.SamplingConfig{
			Initial:    100,
			Thereafter: 100,
		},
		Encoding:         "custom",
		EncoderConfig:    zap.NewProductionEncoderConfig(),
		OutputPaths:      []string{"./a.log"},
		ErrorOutputPaths: []string{"./a.log"},
	}
	my_logger, _ := cfg.Build()
	// my_logger, _ := core.NewProduction()
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		my_logger.Info("this is a test log", zap.Int("name", n), zap.String("tid", "我是全局跟踪号"), zap.String("qqqq", "dfdjkfeiz"))
	}
}

func BenchmarkZaplogger(b *testing.B) {
	// zap_logger, _ := zap.NewProduction()
	cfg := zap.Config{
		Level:       zap.NewAtomicLevelAt(zap.InfoLevel),
		Development: false,
		Sampling: &zap.SamplingConfig{
			Initial:    100,
			Thereafter: 100,
		},
		Encoding:         "json",
		EncoderConfig:    zap.NewProductionEncoderConfig(),
		OutputPaths:      []string{"./a.log"},
		ErrorOutputPaths: []string{"./a.log"},
	}

	zap_logger, _ := cfg.Build()

	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		zap_logger.Info("this is a test log", zap.Int("name", n), zap.String("tid", "我是全局跟踪号"), zap.String("qqqq", "dfdjkfeiz"))
	}
}
