//go:build integration

package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/mail"
)

func TestIntegration_HTTPGatewaySubscriptionHappyPath(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	db := newGatewayTestDB(t, ctx)
	emailChan := make(chan mail.Message, 1)
	baseURL := "http://subscription.test"

	httpServer := newGatewayTestServer(t, ctx, db, emailChan, baseURL)

	postJSON(t, httpServer.URL+"/api/subscribe", map[string]string{
		"email": "User@Example.COM",
		"repo":  "owner/repo",
	}, http.StatusOK)

	msg := receiveGatewayTestEmail(t, emailChan)
	if msg.To != "user@example.com" {
		t.Fatalf("confirmation email recipient = %q, want normalized user@example.com", msg.To)
	}
	confirmToken := extractGatewayTestToken(t, msg.HTML, "/api/confirm/")
	unsubscribeToken := extractGatewayTestToken(t, msg.HTML, "/api/unsubscribe/")

	getStatus(t, httpServer.URL+"/api/confirm/"+confirmToken, http.StatusOK)

	subscriptionsURL := httpServer.URL + "/api/subscriptions?email=" + url.QueryEscape("user@example.com")
	var listed []gatewaySubscription
	getJSON(t, subscriptionsURL, http.StatusOK, &listed)
	if len(listed) != 1 {
		t.Fatalf("listed subscriptions = %d, want 1", len(listed))
	}
	if listed[0] != (gatewaySubscription{
		Email:     "user@example.com",
		Repo:      "owner/repo",
		Confirmed: true,
	}) {
		t.Fatalf("listed subscription = %#v, want confirmed normalized subscription", listed[0])
	}

	getStatus(t, httpServer.URL+"/api/unsubscribe/"+unsubscribeToken, http.StatusOK)

	listed = nil
	getJSON(t, subscriptionsURL, http.StatusOK, &listed)
	if len(listed) != 0 {
		t.Fatalf("listed subscriptions after unsubscribe = %#v, want empty list", listed)
	}
}

func TestIntegration_HTTPGatewaySwaggerJSONIsExposed(t *testing.T) {
	t.Chdir("../..")

	gwMux := newGatewayMux()
	if err := registerGatewayRoutes(gwMux); err != nil {
		t.Fatalf("register gateway routes: %v", err)
	}

	server := httptest.NewServer(gwMux)
	defer server.Close()

	resp, err := http.Get(server.URL + "/swagger.json")
	if err != nil {
		t.Fatalf("GET /swagger.json: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var spec struct {
		Swagger string `json:"swagger"`
		Info    struct {
			Title string `json:"title"`
		} `json:"info"`
		Paths map[string]json.RawMessage `json:"paths"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&spec); err != nil {
		t.Fatalf("decode swagger JSON: %v", err)
	}
	if spec.Swagger != "2.0" {
		t.Fatalf("swagger version = %q, want 2.0", spec.Swagger)
	}
	if _, ok := spec.Paths["/api/subscribe"]; !ok {
		t.Fatalf("swagger paths missing /api/subscribe")
	}
}
