package subscriptions

import subscriptionsdomain "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/subscriptions/domain"

type (
	Subscription    = subscriptionsdomain.Subscription
	SubscribeAction = subscriptionsdomain.SubscribeAction
	SubscribeResult = subscriptionsdomain.SubscribeResult
)

const (
	SubscribeActionCreated               = subscriptionsdomain.SubscribeActionCreated
	SubscribeActionConfirmationRefreshed = subscriptionsdomain.SubscribeActionConfirmationRefreshed
)
