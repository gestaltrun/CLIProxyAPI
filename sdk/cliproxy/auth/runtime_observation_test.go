package auth

import (
	"context"
	"testing"
	"testing/synctest"
	"time"
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

// observationReentryHook calls Update on the same auth from OnAuthUpdated.
// entered prevents the nested Update from reentering the hook.
type observationReentryHook struct {
	NoopHook
	manager *Manager
	entered bool
	err     error
}

func (h *observationReentryHook) OnAuthUpdated(ctx context.Context, auth *Auth) {
	if h.entered {
		return
	}
	h.entered = true
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	_, h.err = h.manager.Update(WithSkipPersist(ctx), auth)
}

func TestUpdateRuntimeObservationHookReentry(t *testing.T) {
	for _, observation := range []bool{false, true} {
		name := "Update"
		if observation {
			name = "RuntimeObservation"
		}
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				hook := &observationReentryHook{}
				manager := NewManager(nil, nil, hook)
				hook.manager = manager
				registered, errRegister := manager.Register(context.Background(), &Auth{ID: "reentry", Provider: "glm"})
				if errRegister != nil {
					t.Fatal(errRegister)
				}
				var err error
				if observation {
					_, err = manager.UpdateRuntimeObservation(context.Background(), registered.ID, func(auth *Auth) {
						auth.Quota.Signals = map[string]string{"status": "ready"}
					})
				} else {
					_, err = manager.Update(context.Background(), registered)
				}
				if err != nil {
					t.Fatal(err)
				}
				if !hook.entered {
					t.Fatal("OnAuthUpdated was not called")
				}
				if hook.err != nil {
					t.Fatalf("hook reentrant Update: %v", hook.err)
				}
			})
		})
	}
}
