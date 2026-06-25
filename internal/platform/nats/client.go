// Package nats initializes NATS and JetStream infrastructure clients.
package nats

import (
	"errors"
	"fmt"
	"time"

	gonats "github.com/nats-io/nats.go"

	platformconfig "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/config"
	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/logger"
)

// NewClient connects to NATS with the standard infrastructure retry policy.
func NewClient(natsURL, clientName string, log logger.Logger) (*gonats.Conn, error) {
	return NewClientWithRetry(natsURL, clientName, platformconfig.NATSRetryConfigFromEnv(), log)
}

// NewClientWithRetry connects to NATS with bounded startup retries.
func NewClientWithRetry(
	natsURL, clientName string,
	retry platformconfig.RetryConfig,
	log logger.Logger,
) (*gonats.Conn, error) {
	if natsURL == "" {
		return nil, errors.New("nats url is required")
	}
	if err := validateRetry(retry); err != nil {
		return nil, err
	}
	if log == nil {
		log = logger.NoopLogger{}
	}

	var lastErr error
	wait := retry.InitialWait
	for attempt := 1; attempt <= retry.MaxAttempts; attempt++ {
		opts := append([]gonats.Option{gonats.Name(clientName)}, connectOptions(log)...)
		nc, err := gonats.Connect(natsURL, opts...)
		if err == nil {
			if err := nc.FlushTimeout(5 * time.Second); err == nil {
				return nc, nil
			} else {
				lastErr = err
				nc.Close()
			}
		} else {
			lastErr = err
		}

		if attempt == retry.MaxAttempts {
			break
		}
		log.Warn("nats connect failed, retrying", "attempt", attempt, "err", lastErr)
		time.Sleep(wait)
		wait *= 2
		if wait > retry.MaxWait {
			wait = retry.MaxWait
		}
	}

	return nil, fmt.Errorf("connect nats after %d attempts: %w", retry.MaxAttempts, lastErr)
}

func connectOptions(log logger.Logger) []gonats.Option {
	return []gonats.Option{
		gonats.Timeout(5 * time.Second),
		gonats.MaxReconnects(-1),
		gonats.ReconnectWait(2 * time.Second),
		gonats.ReconnectJitter(500*time.Millisecond, 2*time.Second),
		gonats.DisconnectErrHandler(func(_ *gonats.Conn, err error) {
			log.Warn("nats disconnected", "err", err)
		}),
		gonats.ReconnectHandler(func(_ *gonats.Conn) {
			log.Info("nats reconnected")
		}),
		gonats.ClosedHandler(func(_ *gonats.Conn) {
			log.Info("nats connection closed")
		}),
	}
}

func validateRetry(retry platformconfig.RetryConfig) error {
	if retry.MaxAttempts < 1 {
		return errors.New("nats retry max attempts must be positive")
	}
	if retry.InitialWait <= 0 {
		return errors.New("nats retry initial wait must be positive")
	}
	if retry.MaxWait <= 0 {
		return errors.New("nats retry max wait must be positive")
	}
	return nil
}
