package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"relaywatch/internal/store"
)

func TestDeliveriesScopedToOwner(t *testing.T) {
	env := newMultiUserEnv(t)
	alice := env.mustUser(t, "alice-del", "user", "TestPassw0rd!")
	bob := env.mustUser(t, "bob-del", "user", "TestPassw0rd!")
	env.mustUser(t, "admin-del", "admin", "TestPassw0rd!")

	ctx := context.Background()
	ch := &store.Channel{Name: "c", Type: "webhook", Config: "{}", Enabled: true, OwnerID: &alice.ID}
	if err := env.st.CreateChannel(ctx, ch); err != nil {
		t.Fatal(err)
	}
	ma := &store.Monitor{Name: "alice-mon", SourceType: "manual", SourceConfig: `{"version":"1"}`, IntervalSec: 300, Enabled: true, State: store.StateUnknown, OwnerID: alice.ID}
	mb := &store.Monitor{Name: "bob-mon", SourceType: "manual", SourceConfig: `{"version":"1"}`, IntervalSec: 300, Enabled: true, State: store.StateUnknown, OwnerID: bob.ID}
	for _, m := range []*store.Monitor{ma, mb} {
		if err := env.st.CreateMonitor(ctx, m); err != nil {
			t.Fatal(err)
		}
		if err := env.st.AddDelivery(ctx, &store.Delivery{MonitorID: m.ID, ChannelID: ch.ID, Status: store.DeliveryPending}); err != nil {
			t.Fatal(err)
		}
	}

	read := func(username string) (int, int) {
		t.Helper()
		cookie, csrf := env.loginAs(t, username, "TestPassw0rd!")
		rec := env.do(t, "GET", "/api/v1/deliveries?per_page=10", cookie, csrf, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET deliveries as %s = %d %s", username, rec.Code, rec.Body.String())
		}
		var got struct {
			Items []any `json:"items"`
			Total int   `json:"total"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		return len(got.Items), got.Total
	}

	if n, total := read("alice-del"); n != 1 || total != 1 {
		t.Fatalf("alice deliveries = %d/%d, want 1/1", n, total)
	}
	if n, total := read("admin-del"); n != 2 || total != 2 {
		t.Fatalf("admin deliveries = %d/%d, want 2/2", n, total)
	}
}
