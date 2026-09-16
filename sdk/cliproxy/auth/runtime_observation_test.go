package auth

import (
	"context"
	"testing"
)

func TestUpdateRuntimeObservationPreservesLatestConfiguration(t *testing.T) {
	manager := NewManager(nil, nil, nil)
	registered, errRegister := manager.Register(context.Background(), &Auth{ID: "observation", Provider: "glm", Attributes: map[string]string{"organization": "old"}})
	if errRegister != nil {
		t.Fatal(errRegister)
	}
	latest, _ := manager.GetByID(registered.ID)
	latest.Attributes["organization"] = "new"
	if _, errUpdate := manager.Update(context.Background(), latest); errUpdate != nil {
		t.Fatal(errUpdate)
	}
	if _, errObservation := manager.UpdateRuntimeObservation(context.Background(), registered.ID, func(auth *Auth) {
		auth.Quota.Signals = map[string]string{"status": "ready"}
	}); errObservation != nil {
		t.Fatal(errObservation)
	}
	final, _ := manager.GetByID(registered.ID)
	if final.Attributes["organization"] != "new" || final.Quota.Signals["status"] != "ready" {
		t.Fatalf("final = %#v", final)
	}
}
