package e2e

import (
	"strings"
	"testing"

	"github.com/playwright-community/playwright-go"
)

// IndexPage models the browser-visible release notification page.
type IndexPage struct {
	page    playwright.Page
	baseURL string
}

// NewIndexPage returns a page object for the index page served by the server.
func NewIndexPage(page playwright.Page, baseURL string) *IndexPage {
	return &IndexPage{
		page:    page,
		baseURL: baseURL,
	}
}

func (p *IndexPage) Open(t *testing.T) {
	t.Helper()

	if _, err := p.page.Goto(p.baseURL); err != nil {
		t.Fatalf("open index page: %v", err)
	}
	p.expectVisible(t, "h1", "GitHub Release Notifications")
}

func (p *IndexPage) Subscribe(t *testing.T, email, repo string) {
	t.Helper()

	p.fillByLabel(t, "Email address", email, 0)
	p.fillByLabel(t, "GitHub repository", repo, 0)
	p.clickButton(t, "Subscribe")
}

func (p *IndexPage) ExpectSubscribeAlert(t *testing.T, alertType, wantText string) {
	t.Helper()

	p.expectVisible(t, "#sub-alert."+alertType, wantText)
}

func (p *IndexPage) ExpectSubscribeFormCleared(t *testing.T) {
	t.Helper()

	p.expectInputValue(t, "#sub-email", "")
	p.expectInputValue(t, "#sub-repo", "")
}

func (p *IndexPage) Lookup(t *testing.T, email string) {
	t.Helper()

	p.fillByLabel(t, "Email address", email, 1)
	p.clickButton(t, "Look up subscriptions")
}

func (p *IndexPage) ExpectSubscription(t *testing.T, repo, meta, badge string) {
	t.Helper()

	item := p.page.Locator("#sub-list .sub-item").First()
	if err := item.WaitFor(playwright.LocatorWaitForOptions{
		State: playwright.WaitForSelectorStateVisible,
	}); err != nil {
		t.Fatalf("wait for subscription item: %v", err)
	}
	p.expectLocatorTextContains(t, item.Locator(".sub-item-repo"), repo)
	p.expectLocatorTextContains(t, item.Locator(".sub-item-meta"), meta)
	p.expectLocatorTextContains(t, item.Locator(".badge"), badge)
}

func (p *IndexPage) fillByLabel(t *testing.T, label, value string, nth int) {
	t.Helper()

	locator := p.page.GetByLabel(label, playwright.PageGetByLabelOptions{
		Exact: playwright.Bool(true),
	}).Nth(nth)
	if err := locator.Fill(value); err != nil {
		t.Fatalf("fill %q: %v", label, err)
	}
}

func (p *IndexPage) clickButton(t *testing.T, name string) {
	t.Helper()

	err := p.page.GetByRole(*playwright.AriaRoleButton, playwright.PageGetByRoleOptions{
		Name:  name,
		Exact: playwright.Bool(true),
	}).Click()
	if err != nil {
		t.Fatalf("click button %q: %v", name, err)
	}
}

func (p *IndexPage) expectVisible(t *testing.T, selector, wantText string) {
	t.Helper()

	locator := p.page.Locator(selector)
	if err := locator.WaitFor(playwright.LocatorWaitForOptions{
		State: playwright.WaitForSelectorStateVisible,
	}); err != nil {
		t.Fatalf("wait for %q: %v", selector, err)
	}
	p.expectLocatorTextContains(t, locator, wantText)
}

func (p *IndexPage) expectLocatorTextContains(t *testing.T, locator playwright.Locator, want string) {
	t.Helper()

	got, err := locator.TextContent()
	if err != nil {
		t.Fatalf("read locator text: %v", err)
	}
	if !strings.Contains(got, want) {
		t.Fatalf("expected text to contain %q, got %q", want, got)
	}
}

func (p *IndexPage) expectInputValue(t *testing.T, selector, want string) {
	t.Helper()

	got, err := p.page.Locator(selector).InputValue()
	if err != nil {
		t.Fatalf("read input %q value: %v", selector, err)
	}
	if got != want {
		t.Fatalf("expected %q value %q, got %q", selector, want, got)
	}
}
