package main

import (
	"log/slog"

	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector"
	confluencecollector "github.com/eshu-hq/eshu/go/internal/collector/confluence"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

func buildCollectorService(
	database postgres.SQLDB,
	getenv func(string) string,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
	logger *slog.Logger,
) (collector.Service, error) {
	config, err := confluencecollector.LoadConfig(getenv)
	if err != nil {
		return collector.Service{}, err
	}
	client, err := confluencecollector.NewHTTPClient(confluencecollector.HTTPClientConfig{
		BaseURL:     config.BaseURL,
		Email:       config.Email,
		APIToken:    config.APIToken,
		BearerToken: config.BearerToken,
		Instruments: instruments,
	})
	if err != nil {
		return collector.Service{}, err
	}

	committer := postgres.NewIngestionStore(database)
	committer.Logger = logger

	return collector.Service{
		Source: &confluencecollector.Source{
			Client:      client,
			Config:      config,
			Logger:      logger,
			Instruments: instruments,
		},
		Committer:    committer,
		PollInterval: config.PollInterval,
		Tracer:       tracer,
		Instruments:  instruments,
		Logger:       logger,
	}, nil
}
