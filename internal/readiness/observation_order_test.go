package readiness

import (
	"testing"
	"time"
)

func TestRunOrderingUsesStartTimeAndRetainsCompletionForFreshness(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Microsecond)
	e := Evidence{}
	recordRunObservation(&e, now.Add(time.Second), now.Add(2*time.Second), false)
	recordRunObservation(&e, now, now.Add(3*time.Second), true)
	if e.LastPassed || !e.LastReconciledAt.Equal(now.Add(2*time.Second)) {
		t.Fatal("older run replaced the newer failure")
	}
	recordRunObservation(&e, now.Add(4*time.Second), now.Add(6*time.Second), true)
	if !e.LastPassed || !e.LastReconciliationStartedAt.Equal(now.Add(4*time.Second)) || !e.LastReconciledAt.Equal(now.Add(6*time.Second)) {
		t.Fatal("a genuine subsequent run could not recover current health")
	}
}

func TestRunOrderingNormalizesFirestorePrecisionAndLegacyFences(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Microsecond)
	for _, failureFirst := range []bool{false, true} {
		e := Evidence{}
		if failureFirst {
			recordRunObservation(&e, now.Add(200*time.Nanosecond), now.Add(time.Second), false)
			recordRunObservation(&e, now.Add(100*time.Nanosecond), now.Add(2*time.Second), true)
		} else {
			recordRunObservation(&e, now.Add(100*time.Nanosecond), now.Add(time.Second), true)
			recordRunObservation(&e, now.Add(200*time.Nanosecond), now.Add(2*time.Second), false)
		}
		if e.LastPassed {
			t.Fatal("failure lost within one Firestore timestamp tick")
		}
	}
	e := Evidence{LastReconciledAt: now.Add(time.Second), LastPassed: false}
	recordRunObservation(&e, now, now.Add(2*time.Second), true)
	if e.LastPassed {
		t.Fatal("legacy completion fence was ignored")
	}
	recordRunObservation(&e, now.Add(3*time.Second), now.Add(4*time.Second), true)
	if !e.LastPassed {
		t.Fatal("legacy record could not recover with a newer run")
	}
}
