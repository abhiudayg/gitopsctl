package cmd

import (
	"context"

	"abhiudayg.com/github/gitopsctl/internal/events"
	"go.uber.org/zap"
)

func commandEmitter() events.Emitter {
	var sinks []events.Sink
	if eventsFile != "" {
		fs, err := events.NewFileSink(eventsFile)
		if err != nil {
			if logger != nil {
				logger.Warn("failed to initialize events file sink", zap.Error(err))
			}
			return events.Noop{}
		}
		sinks = append(sinks, fs)
	}
	if eventsWebhookURL != "" {
		sinks = append(sinks, events.NewWebhookSinkWithOptions(eventsWebhookURL, eventsWebhookBearer, events.WebhookOptions{
			Timeout:       eventsWebhookTimeout,
			Retries:       eventsWebhookRetry,
			RetryBackoff:  eventsWebhookBackoff,
			SigningSecret: eventsWebhookSecret,
		}))
	}
	if len(sinks) == 0 {
		return events.Noop{}
	}
	return events.NewBus(logger, "gitopsctl-cli", sinks...)
}

func emitCommandEvent(typ events.Type, data map[string]any) {
	commandEmitter().Emit(context.Background(), typ, data)
}
