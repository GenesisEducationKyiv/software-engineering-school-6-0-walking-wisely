package architecture

type Rule struct {
	Name            string
	Package         string
	AllowedPrefixes []string
}

var Rules = []Rule{
	// Subscriptions
	{
		Name:    "subscriptions-app",
		Package: "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/subscriptions/app",
		AllowedPrefixes: []string{
			"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/subscriptions/app",
			"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/subscriptions/domain",
			"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/",
			"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/contracts/",
		},
	},
	{
		Name:    "subscriptions-grpc",
		Package: "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/subscriptions/grpc",
		AllowedPrefixes: []string{
			"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/subscriptions/grpc",
			"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/subscriptions/app",
			"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/subscriptions/domain",
			"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/",
			"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/contracts/",
			"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/gen/",
		},
	},
	{
		Name:    "subscriptions-postgres",
		Package: "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/subscriptions/postgres",
		AllowedPrefixes: []string{
			"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/subscriptions/postgres",
			"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/subscriptions/app",
			"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/subscriptions/domain",
			"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/",
			"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/contracts/",
		},
	},
	{
		Name:    "subscriptions-notify",
		Package: "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/subscriptions/notify",
		AllowedPrefixes: []string{
			"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/subscriptions/notify",
			"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/",
			"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/contracts/",
			"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/gen/",
		},
	},

	// Notifications
	{
		Name:    "notifications-app",
		Package: "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/notifications/app",
		AllowedPrefixes: []string{
			"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/notifications/app",
			"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/notifications/domain",
			"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/",
			"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/contracts/",
		},
	},
	{
		Name:    "notifications-grpc",
		Package: "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/notifications/grpc",
		AllowedPrefixes: []string{
			"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/notifications/grpc",
			"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/notifications/app",
			"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/notifications/domain",
			"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/",
			"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/contracts/",
			"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/gen/",
		},
	},
	{
		Name:    "notifications-postgres",
		Package: "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/notifications/postgres",
		AllowedPrefixes: []string{
			"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/notifications/postgres",
			"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/notifications/domain",
			"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/",
			"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/contracts/",
		},
	},
	{
		Name:    "notifications-worker",
		Package: "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/notifications/worker",
		AllowedPrefixes: []string{
			"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/notifications/worker",
			"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/notifications/app",
			"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/notifications/domain",
			"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/",
			"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/contracts/",
		},
	},

	// Release monitoring
	{
		Name:    "release-monitoring-app",
		Package: "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/release_monitoring/app",
		AllowedPrefixes: []string{
			"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/release_monitoring/app",
			"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/release_monitoring/domain",
			"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/",
			"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/contracts/",
			"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/integrations/",
		},
	},
	{
		Name:    "release-monitoring-postgres",
		Package: "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/release_monitoring/postgres",
		AllowedPrefixes: []string{
			"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/release_monitoring/postgres",
			"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/release_monitoring/domain",
			"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/",
			"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/contracts/",
		},
	},
	{
		Name:    "release-monitoring-worker",
		Package: "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/release_monitoring/worker",
		AllowedPrefixes: []string{
			"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/release_monitoring/worker",
			"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/release_monitoring/app",
			"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/release_monitoring/domain",
			"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/",
			"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/contracts/",
			"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/integrations/",
		},
	},
}
