package worker

import (
	"context"
	"time"

	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/logger"
	releasemonitoringapp "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/release_monitoring/app"
)

func StartScanner(ctx context.Context, service *releasemonitoringapp.ScannerService, interval time.Duration, log logger.Logger) {
	if log == nil {
		log = logger.NoopLogger{}
	}
	log.Info("scanner started", "interval", interval)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Info("scanner stopped")
			return
		case <-ticker.C:
			service.Scan(ctx)
		}
	}
}
