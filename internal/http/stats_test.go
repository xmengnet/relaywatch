package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"relaywatch/internal/store"
)

func TestStatsScopedToOwner(t *testing.T) {
	env := newMultiUserEnv(t)
	alice := env.mustUser(t, "alice-stats", "user", "TestPassw0rd!")
	env.mustUser(t, "bob-stats", "user", "TestPassw0rd!")
	env.mustUser(t, "admin-stats", "admin", "TestPassw0rd!")

	ctx := context.Background()
	if err := env.st.CreateMonitor(ctx, &store.Monitor{
		Name: "alice-mon", SourceType: "manual", SourceConfig: `{"version":"1.0.0"}`,
		IntervalSec: 300, Enabled: true, State: store.StateHasUpdate, OwnerID: alice.ID,
	}); err != nil {
		t.Fatal(err)
	}
	if err := env.st.CreateMonitor(ctx, &store.Monitor{
		Name: "bob-mon", SourceType: "manual", SourceConfig: `{"version":"1.0.0"}`,
		IntervalSec: 300, Enabled: true, State: store.StateError, OwnerID: 2,
	}); err != nil {
		t.Fatal(err)
	}

	read := func(username string) map[string]int {
		t.Helper()
		cookie, csrf := env.loginAs(t, username, "TestPassw0rd!")
		rec := env.do(t, "GET", "/api/v1/stats", cookie, csrf, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET stats as %s = %d %s", username, rec.Code, rec.Body.String())
		}
		var got map[string]int
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		return got
	}

	aliceStats := read("alice-stats")
	if aliceStats["total"] != 1 || aliceStats["has_update"] != 1 || aliceStats["error"] != 0 {
		t.Fatalf("alice stats = %v, want only her monitor", aliceStats)
	}

	adminStats := read("admin-stats")
	if adminStats["total"] != 2 || adminStats["has_update"] != 1 || adminStats["error"] != 1 {
		t.Fatalf("admin stats = %v, want both monitors", adminStats)
	}
}
