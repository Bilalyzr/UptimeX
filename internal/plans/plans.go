// Package plans defines the SaaS plan catalog and the limits each plan
// enforces. Plans live in code rather than the database so quota checks are
// deterministic and the catalog is versioned with the deploy that enforces it.
package plans

import "uptimex/internal/models"

// Plan is one commercial tier. Prices are stored in minor units (paise) of
// the catalog currency (INR).
type Plan struct {
	ID                 string   `json:"id"`
	Name               string   `json:"name"`
	PriceMonthlyCents  int      `json:"price_monthly_cents"`
	Currency           string   `json:"currency"`
	MaxEndpoints       int      `json:"max_endpoints"`
	MinIntervalSeconds int      `json:"min_interval_seconds"`
	HistoryDays        int      `json:"history_days"`
	Features           []string `json:"features"`
}

// CurrencyINR is the catalog currency.
const CurrencyINR = "INR"

// catalog is the complete, ordered plan list shown on pricing/billing pages.
var catalog = []Plan{
	{
		ID: models.PlanFree, Name: "Free", PriceMonthlyCents: 0, Currency: CurrencyINR,
		MaxEndpoints: 2, MinIntervalSeconds: 60, HistoryDays: 7,
		Features: []string{
			"2 monitored endpoints",
			"60-second check interval",
			"7-day metrics history",
			"Email + webhook alerts",
			"Public status page",
		},
	},
	{
		ID: models.PlanPro, Name: "Pro", PriceMonthlyCents: 149900, Currency: CurrencyINR,
		MaxEndpoints: 50, MinIntervalSeconds: 30, HistoryDays: 30,
		Features: []string{
			"50 monitored endpoints",
			"30-second check interval",
			"30-day metrics history",
			"P50 / P95 / P99 analytics",
			"HMAC-signed webhook alerts",
			"Public status page",
		},
	},
	{
		ID: models.PlanBusiness, Name: "Business", PriceMonthlyCents: 799900, Currency: CurrencyINR,
		MaxEndpoints: 250, MinIntervalSeconds: 10, HistoryDays: 90,
		Features: []string{
			"250 monitored endpoints",
			"10-second check interval",
			"90-day metrics history",
			"Failure-state-machine incident accuracy",
			"Self-monitoring heartbeat watchdog",
			"Priority support",
		},
	},
}

// All returns the full catalog, ordered Free → Business.
func All() []Plan {
	out := make([]Plan, len(catalog))
	copy(out, catalog)
	return out
}

// ByID resolves a plan by its wire identifier.
func ByID(id string) (Plan, bool) {
	for _, p := range catalog {
		if p.ID == id {
			return p, true
		}
	}
	return Plan{}, false
}

// Default is the plan new organizations start on.
func Default() Plan { return catalog[0] }
