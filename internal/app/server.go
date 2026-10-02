package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/example/google-project-cost-manager/internal/billing"
	"github.com/example/google-project-cost-manager/internal/config"
	"github.com/example/google-project-cost-manager/internal/eventstate"
	"github.com/example/google-project-cost-manager/internal/policy"
	"github.com/example/google-project-cost-manager/internal/pubsub"
	"github.com/example/google-project-cost-manager/internal/readiness"
	"github.com/example/google-project-cost-manager/internal/reconcile"
	"github.com/example/google-project-cost-manager/internal/selftest"
)

const (
	RouteModeAll      = "all"
	RouteModeReceiver = "receiver"
	RouteModeAdmin    = "admin"
)

type ReconcileRunner func(context.Context, *config.Config, reconcile.BudgetLister, reconcile.ProjectResolver) (reconcile.Result, error)
type EventStoreFactory func(context.Context, config.EventStateConfig) (eventstate.Store, error)

type Options struct {
	Billing           billing.Client
	LoadConfig        func(context.Context) (*config.Config, error)
	SelfTestStore     selftest.Store
	ReconcileLister   reconcile.BudgetLister
	ProjectResolver   reconcile.ProjectResolver
	ReconcileRunner   ReconcileRunner
	EventStore        eventstate.Store
	EventStoreFactory EventStoreFactory
	RouteMode         string
	ReadinessCheck    func(context.Context, *config.Config) error
}

type Server struct {
	opt     Options
	mux     *http.ServeMux
	storeMu sync.Mutex
	store   eventstate.Store
}

func NewServer(opt Options) (*Server, error) {
	if opt.LoadConfig == nil {
		opt.LoadConfig = config.LoadFromEnv
	}
	if opt.ReadinessCheck == nil {
		opt.ReadinessCheck = readiness.VerifyEnforcement
	}
	if opt.SelfTestStore == nil {
		opt.SelfTestStore = &selftest.MemoryStore{}
	}
	if opt.RouteMode == "" {
		opt.RouteMode = RouteModeAll
	}
	if opt.RouteMode != RouteModeAll && opt.RouteMode != RouteModeReceiver && opt.RouteMode != RouteModeAdmin {
		return nil, fmt.Errorf("unsupported route mode %q", opt.RouteMode)
	}
	if opt.EventStoreFactory == nil {
		opt.EventStoreFactory = func(ctx context.Context, cfg config.EventStateConfig) (eventstate.Store, error) {
			return eventstate.NewFirestoreStore(ctx, cfg.ProjectID, cfg.DatabaseID, cfg.Collection)
		}
	}
	server := &Server{opt: opt, mux: http.NewServeMux(), store: opt.EventStore}
	server.routes()
	return server, nil
}

func (s *Server) Router() http.Handler { return s.mux }

func (s *Server) routes() {
	s.mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})
	s.mux.HandleFunc("GET /readyz", s.ready)
	if s.opt.RouteMode == RouteModeAll || s.opt.RouteMode == RouteModeReceiver {
		s.mux.HandleFunc("POST /pubsub/billing-alert", s.billingAlert)
	}
	if s.opt.RouteMode == RouteModeAll || s.opt.RouteMode == RouteModeAdmin {
		s.mux.HandleFunc("POST /reconcile", s.reconcile)
		s.mux.HandleFunc("POST /self-test", s.selfTest)
	}
}

func (s *Server) ready(w http.ResponseWriter, r *http.Request) {
	if _, err := s.opt.LoadConfig(r.Context()); err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ready": true, "routeMode": s.opt.RouteMode})
}

func (s *Server) billingAlert(w http.ResponseWriter, r *http.Request) {
	envelope, err := pubsub.DecodeEnvelope(readAll(r))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	cfg, err := s.opt.LoadConfig(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	evaluation := policy.Evaluate(cfg, envelope.Alert)
	if cfg.SchemaVersion < 2 {
		writeJSON(w, http.StatusOK, evaluation)
		return
	}
	if envelope.MessageID == "" || envelope.PublishTime.IsZero() {
		http.Error(w, "canonical enforcement requires Pub/Sub messageId and publishTime", http.StatusBadRequest)
		return
	}
	failed := false
	for i := range evaluation.Decisions {
		decision := &evaluation.Decisions[i]
		if decision.Reason == "protected_project" {
			logAlert("protected_attempt", decision.ProjectID, errors.New("protected project alert rejected"))
		}
		if !decision.Disable || decision.DryRun || decision.ProjectID == "" {
			continue
		}
		if err := s.opt.ReadinessCheck(r.Context(), cfg); err != nil {
			decision.Disable, decision.Reason = false, "live_readiness_failed"
			logAlert("stale_inventory", decision.ProjectID, err)
			failed = true
			continue
		}
		if err := s.enforceCanonical(r.Context(), cfg, envelope, decision); err != nil {
			failed = true
			logAlert("disable_failed", decision.ProjectID, err)
		}
	}
	if failed {
		writeJSON(w, http.StatusInternalServerError, evaluation)
		return
	}
	writeJSON(w, http.StatusOK, evaluation)
}

func (s *Server) enforceCanonical(ctx context.Context, cfg *config.Config, envelope *pubsub.Envelope, decision *policy.Decision) error {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	client := s.opt.Billing
	var err error
	if client == nil {
		client, err = billing.NewCloudClient(ctx)
		if err != nil {
			decision.Reason = "billing_client_failed"
			return err
		}
	}
	info, err := client.GetProjectBillingInfo(ctx, decision.ProjectID)
	if err != nil {
		decision.Reason = "billing_permission_or_read_failed"
		logAlert("permission_missing", decision.ProjectID, err)
		return err
	}
	if info == nil {
		decision.Reason = "billing_info_missing"
		return errors.New("billing API returned empty project billing info")
	}
	if !info.BillingEnabled {
		decision.Disable = false
		decision.Reason = "already_disabled"
		if err := s.recordAlreadyDisabled(ctx, cfg, envelope, decision); err != nil {
			decision.Reason = "event_state_idempotency_failed"
			return err
		}
		return nil
	}
	if info.BillingAccountName != decision.ExpectedBillingAccount {
		decision.Disable = false
		decision.Reason = "wrong_billing_account"
		return nil
	}
	start, err := envelope.Alert.PeriodStart()
	if err != nil {
		decision.Disable = false
		decision.Reason = "malformed_period"
		return nil
	}
	store, err := s.eventStore(ctx, cfg.EventState)
	if err != nil {
		decision.Reason = "event_state_unavailable"
		return err
	}
	event := eventstate.Event{MessageID: envelope.MessageID, PublishTime: envelope.PublishTime, CostAmount: envelope.Alert.CostAmount}
	key := eventstate.Key(decision.MatchedBudget, decision.ProjectID, start.UTC().Format("2006-01"))
	disposition, err := store.Claim(ctx, key, &event)
	if err != nil {
		decision.Reason = "event_state_claim_failed"
		return err
	}
	if disposition != eventstate.Accepted {
		decision.Disable = false
		decision.Reason = disposition + "_event"
		if disposition == eventstate.Busy {
			return errors.New("event processing lease is active; retry delivery")
		}
		return nil
	}
	info, err = client.GetProjectBillingInfo(ctx, decision.ProjectID)
	if err != nil || info == nil {
		_ = store.Complete(ctx, key, event, false)
		return errors.New("billing link recheck failed")
	}
	if !info.BillingEnabled {
		decision.Disable, decision.Reason = false, "already_disabled"
		return store.Complete(ctx, key, event, true)
	}
	if info.BillingAccountName != decision.ExpectedBillingAccount {
		decision.Disable, decision.Reason = false, "wrong_billing_account"
		return store.Complete(ctx, key, event, false)
	}
	if err := s.opt.ReadinessCheck(ctx, cfg); err != nil {
		decision.Disable, decision.Reason = false, "operator_enforcement_stopped"
		_ = store.Complete(ctx, key, event, false)
		return err
	}
	if err := client.DisableBilling(ctx, decision.ProjectID); err != nil {
		decision.Reason = "disable_failed: " + err.Error()
		_ = store.Complete(ctx, key, event, false)
		return err
	}
	if err := store.Complete(ctx, key, event, true); err != nil {
		decision.Reason = "event_state_complete_failed"
		return err
	}
	decision.Reason = "billing_disabled"
	return nil
}

func (s *Server) eventStore(ctx context.Context, cfg config.EventStateConfig) (eventstate.Store, error) {
	s.storeMu.Lock()
	defer s.storeMu.Unlock()
	if s.store != nil {
		return s.store, nil
	}
	store, err := s.opt.EventStoreFactory(ctx, cfg)
	if err != nil {
		return nil, err
	}
	s.store = store
	return store, nil
}

func (s *Server) selfTest(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Mode string `json:"mode"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	cfg, err := s.opt.LoadConfig(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	mode := req.Mode
	if mode == "" {
		mode = cfg.SelfTest.Mode
	}
	if mode == "" {
		mode = "dry_run"
	}
	if mode == "billing_toggle" {
		http.Error(w, "billing toggle is an explicit operator CLI action, not a runtime route", http.StatusForbidden)
		return
	}
	client := s.opt.Billing
	var runner selftest.ReconcileRunner
	if mode == "setup_validation" {
		lister, resolver, err := s.reconcileClients(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		reconcileRunner := s.opt.ReconcileRunner
		if reconcileRunner == nil {
			reconcileRunner = reconcile.Run
		}
		runner = func(ctx context.Context, cfg *config.Config) (reconcile.Result, error) {
			return reconcileRunner(ctx, cfg, lister, resolver)
		}
	} else if client == nil && mode != "dry_run" {
		client, err = billing.NewCloudClient(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	writeJSON(w, http.StatusOK, selftest.RunWithReconciler(r.Context(), mode, cfg, client, s.opt.SelfTestStore, runner))
}

func (s *Server) reconcile(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.opt.LoadConfig(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !cfg.Reconcile.Enabled {
		http.Error(w, "reconcile.enabled must be true", http.StatusBadRequest)
		return
	}
	lister, resolver, err := s.reconcileClients(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	runner := s.opt.ReconcileRunner
	if runner == nil {
		runner = reconcile.Run
	}
	result, err := runner(r.Context(), cfg, lister, resolver)
	if cfg.SchemaVersion >= 2 {
		passed := err == nil && result.Summary.Errors == 0 && readiness.CheckFresh(cfg, time.Now().UTC()) == nil
		evidence, openErr := readiness.New(r.Context(), cfg)
		if openErr == nil {
			openErr = evidence.RecordDay(r.Context(), cfg, time.Now().UTC(), passed)
			_ = evidence.Close()
		}
		if openErr != nil {
			err = fmt.Errorf("persist reconciliation evidence: %w", openErr)
		}
		if !passed {
			logAlert("stale_inventory", "", errors.New("reconciliation or inventory freshness check failed"))
		}
	}
	if err != nil {
		logAlert("reconciliation_error", "", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if result.Summary.Errors > 0 {
		logAlert("reconciliation_error", "", fmt.Errorf("reconciliation reported %d errors", result.Summary.Errors))
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) reconcileClients(ctx context.Context) (reconcile.BudgetLister, reconcile.ProjectResolver, error) {
	lister, resolver := s.opt.ReconcileLister, s.opt.ProjectResolver
	var err error
	if lister == nil {
		lister, err = reconcile.NewCloudBudgetClient(ctx)
		if err != nil {
			return nil, nil, err
		}
	}
	if resolver == nil {
		resolver, err = reconcile.NewCloudProjectResolver(ctx)
		if err != nil {
			return nil, nil, err
		}
	}
	return lister, resolver, nil
}

func readAll(r *http.Request) []byte {
	defer r.Body.Close()
	var value any
	if err := json.NewDecoder(r.Body).Decode(&value); err != nil {
		return nil
	}
	data, _ := json.Marshal(value)
	return data
}

func writeJSON(w http.ResponseWriter, code int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(value)
}

func logAlert(kind, project string, err error) {
	payload, _ := json.Marshal(map[string]any{"alert_kind": kind, "project_id": project, "error": err.Error(), "timestamp": time.Now().UTC().Format(time.RFC3339)})
	fmt.Println(string(payload))
}
