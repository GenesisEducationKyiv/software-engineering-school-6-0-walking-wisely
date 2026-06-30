// Package commands defines cross-service command and reply contracts for the
// orchestrated subscription saga.
package commands

import (
	"time"

	"github.com/google/uuid"

	contractevents "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/contracts/events"
)

// SendConfirmationEmail is sent by the subscriptions service to the notifications
// service, requesting that a confirmation email be dispatched.
type SendConfirmationEmail struct {
	contractevents.Metadata
	SagaID         string `json:"saga_id"`
	SubscriptionID string `json:"subscription_id"`
	Email          string `json:"email"`
	Repo           string `json:"repo"`
	ConfirmToken   string `json:"confirm_token"`
	UnsubToken     string `json:"unsub_token"`
}

func (SendConfirmationEmail) EventName() string     { return "subscriptions.send_confirmation_email" }
func (SendConfirmationEmail) AggregateType() string { return "saga" }

//nolint:gocritic
func (e SendConfirmationEmail) AggregateID() string { return e.SagaID }

// NewSendConfirmationEmail constructs a SendConfirmationEmail command.
func NewSendConfirmationEmail(sagaID, subscriptionID, email, repo, confirmToken, unsubToken string) SendConfirmationEmail {
	return SendConfirmationEmail{
		Metadata: contractevents.Metadata{
			ID:    uuid.NewString(),
			At:    time.Now().UTC(),
			V:     1,
			IdKey: "subscriptions.send_confirmation_email:" + sagaID,
		},
		SagaID:         sagaID,
		SubscriptionID: subscriptionID,
		Email:          email,
		Repo:           repo,
		ConfirmToken:   confirmToken,
		UnsubToken:     unsubToken,
	}
}

// ConfirmationEmailSent is sent by the notifications service when the confirmation
// email has been successfully handed to the provider.
type ConfirmationEmailSent struct {
	contractevents.Metadata
	SagaID string `json:"saga_id"`
}

func (ConfirmationEmailSent) EventName() string     { return "notifications.confirmation_email_sent" }
func (ConfirmationEmailSent) AggregateType() string { return "saga" }

//nolint:gocritic
func (e ConfirmationEmailSent) AggregateID() string { return e.SagaID }

// NewConfirmationEmailSent constructs a ConfirmationEmailSent reply.
func NewConfirmationEmailSent(sagaID string) ConfirmationEmailSent {
	return ConfirmationEmailSent{
		Metadata: contractevents.Metadata{
			ID:    uuid.NewString(),
			At:    time.Now().UTC(),
			V:     1,
			IdKey: "notifications.confirmation_email_sent:" + sagaID,
		},
		SagaID: sagaID,
	}
}

// ConfirmationEmailFailed is sent by the notifications service when the
// confirmation email job has exhausted all retries or was synchronously rejected.
type ConfirmationEmailFailed struct {
	contractevents.Metadata
	SagaID string `json:"saga_id"`
	Reason string `json:"reason"`
}

func (ConfirmationEmailFailed) EventName() string     { return "notifications.confirmation_email_failed" }
func (ConfirmationEmailFailed) AggregateType() string { return "saga" }

//nolint:gocritic
func (e ConfirmationEmailFailed) AggregateID() string { return e.SagaID }

// NewConfirmationEmailFailed constructs a ConfirmationEmailFailed reply.
func NewConfirmationEmailFailed(sagaID, reason string) ConfirmationEmailFailed {
	return ConfirmationEmailFailed{
		Metadata: contractevents.Metadata{
			ID:    uuid.NewString(),
			At:    time.Now().UTC(),
			V:     1,
			IdKey: "notifications.confirmation_email_failed:" + sagaID,
		},
		SagaID: sagaID,
		Reason: reason,
	}
}

// RegisterTypes registers all saga command and reply types with the caller's codec.
func RegisterTypes(register func(contractevents.Event)) {
	register(SendConfirmationEmail{})
	register(ConfirmationEmailSent{})
	register(ConfirmationEmailFailed{})
}
