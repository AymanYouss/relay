package usage

import "context"

// Recorder is the accounting facade used by the gateway. It owns the pricing
// table and delegates persistence to a Store.
type Recorder struct {
	store   Store
	pricing map[string]Pricing
}

// NewRecorder builds a Recorder with a pricing table keyed by logical model name.
func NewRecorder(store Store, pricing map[string]Pricing) *Recorder {
	if pricing == nil {
		pricing = map[string]Pricing{}
	}
	return &Recorder{store: store, pricing: pricing}
}

// Cost computes the USD cost of a completion on the given model.
func (r *Recorder) Cost(model string, promptTokens, completionTokens int) float64 {
	p, ok := r.pricing[model]
	if !ok {
		return 0
	}
	return p.Cost(promptTokens, completionTokens)
}

// Record persists a completed request event.
func (r *Recorder) Record(ctx context.Context, e Event) error {
	return r.store.Record(ctx, e)
}

// MonthSpend returns a key's month-to-date spend.
func (r *Recorder) MonthSpend(ctx context.Context, keyID string) (float64, error) {
	return r.store.MonthSpend(ctx, keyID)
}

// BudgetExceeded reports whether a key has met or passed its monthly budget.
// A budget of 0 means unlimited.
func (r *Recorder) BudgetExceeded(ctx context.Context, keyID string, budgetUSD float64) (bool, float64, error) {
	if budgetUSD <= 0 {
		return false, 0, nil
	}
	spent, err := r.MonthSpend(ctx, keyID)
	if err != nil {
		return false, 0, err
	}
	return spent >= budgetUSD, spent, nil
}

// Dashboard returns aggregated analytics for the last windowDays days.
func (r *Recorder) Dashboard(ctx context.Context, windowDays int) (Dashboard, error) {
	return r.store.Query(ctx, windowDays)
}

// Store exposes the underlying store (used to close it on shutdown).
func (r *Recorder) Store() Store { return r.store }
