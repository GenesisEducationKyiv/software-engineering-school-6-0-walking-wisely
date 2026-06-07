//go:build e2e

package main

import (
	"context"
	"testing"
	"time"

	"github.com/playwright-community/playwright-go"

	servere2e "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/cmd/server/e2e"
	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/notifications/mail"
)

func TestIndexPageSubscriptionFlow(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	db := newGatewayTestDB(t, ctx)
	emailChan := make(chan mail.Message, 1)
	httpServer := newGatewayTestServer(t, ctx, db, emailChan, "http://subscription.test")

	pw, err := playwright.Run()
	if err != nil {
		t.Fatalf("start playwright: %v", err)
	}
	t.Cleanup(func() {
		if err := pw.Stop(); err != nil {
			t.Logf("stop playwright: %v", err)
		}
	})

	browser, err := pw.Chromium.Launch(playwright.BrowserTypeLaunchOptions{
		Headless: playwright.Bool(true),
	})
	if err != nil {
		t.Fatalf("launch chromium: %v", err)
	}
	t.Cleanup(func() {
		if err := browser.Close(); err != nil {
			t.Logf("close browser: %v", err)
		}
	})

	context, err := browser.NewContext()
	if err != nil {
		t.Fatalf("create browser context: %v", err)
	}
	page, err := context.NewPage()
	if err != nil {
		t.Fatalf("create page: %v", err)
	}

	index := servere2e.NewIndexPage(page, httpServer.URL)
	index.Open(t)

	index.Subscribe(t, "E2E.User@Example.COM", "OWNER/Repo")
	index.ExpectSubscribeAlert(t, "success", "Check your inbox")
	index.ExpectSubscribeFormCleared(t)

	msg := receiveGatewayTestEmail(t, emailChan)
	confirmToken := extractGatewayTestToken(t, msg.HTML, "/api/confirm/")
	getStatus(t, httpServer.URL+"/api/confirm/"+confirmToken, 200)

	index.Lookup(t, "e2e.user@example.com")
	index.ExpectSubscription(t, "OWNER/Repo", "No releases seen yet", "Active")

	index.Subscribe(t, "e2e.user@example.com", "OWNER/Repo")
	index.ExpectSubscribeAlert(t, "info", "already subscribed")
}
