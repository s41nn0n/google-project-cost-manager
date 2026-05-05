package app

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"github.com/example/google-project-cost-manager/internal/billing"
	"github.com/example/google-project-cost-manager/internal/config"
	"github.com/example/google-project-cost-manager/internal/policy"
	"github.com/example/google-project-cost-manager/internal/pubsub"
	"github.com/example/google-project-cost-manager/internal/reconcile"
	"github.com/example/google-project-cost-manager/internal/selftest"
)

type ReconcileRunner func(context.Context, *config.Config, reconcile.BudgetLister, reconcile.ProjectResolver) (reconcile.Result, error)

type Options struct {
	Billing         billing.Client
	LoadConfig      func(context.Context) (*config.Config, error)
	SelfTestStore   selftest.Store
	ReconcileLister reconcile.BudgetLister
	ProjectResolver reconcile.ProjectResolver
	ReconcileRunner ReconcileRunner
}

type Server struct {
	opt Options
	mux *http.ServeMux
}

func NewServer(opt Options) (*Server, error) {
	if opt.LoadConfig == nil {
		opt.LoadConfig = config.LoadFromEnv
	}
	if opt.SelfTestStore == nil {
		opt.SelfTestStore = &selftest.MemoryStore{}
	}
	s := &Server{opt: opt, mux: http.NewServeMux()}
	s.routes()
	return s, nil
}

func (s *Server) Router() http.Handler { return s.mux }

func (s *Server) routes() {
	s.mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})
	s.mux.HandleFunc("GET /readyz", s.ready)
	s.mux.HandleFunc("POST /pubsub/billing-alert", s.billingAlert)
	s.mux.HandleFunc("POST /reconcile", s.reconcile)
	s.mux.HandleFunc("POST /self-test", s.selfTest)
}

func (s *Server) ready(w http.ResponseWriter, r *http.Request) {
	if _, err := s.opt.LoadConfig(r.Context()); err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ready": true})
}

func (s *Server) billingAlert(w http.ResponseWriter, r *http.Request) {
	alert, err := pubsub.DecodePush(readAll(r))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	cfg, err := s.opt.LoadConfig(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	ev := policy.Evaluate(cfg, *alert)
	bc := s.opt.Billing
	needsBilling := false
	for _, d := range ev.Decisions {
		if d.Disable && !d.DryRun && d.ProjectID != "" {
			needsBilling = true
			break
		}
	}
	if bc == nil && needsBilling {
		bc, err = billing.NewCloudClient(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	disableFailed := false
	for i, d := range ev.Decisions {
		if d.Disable && !d.DryRun && d.ProjectID != "" {
			if err := bc.DisableBilling(r.Context(), d.ProjectID); err != nil {
				ev.Decisions[i].Reason = "disable_failed: " + err.Error()
				disableFailed = true
				log.Printf("disable billing failed for %s: %v", d.ProjectID, err)
			}
		}
	}
	if disableFailed {
		writeJSON(w, http.StatusInternalServerError, ev)
		return
	}
	writeJSON(w, http.StatusOK, ev)
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
	if mode == "billing_toggle" && cfg.SelfTest.RestoreBillingAccountNameSecret != "" && cfg.SelfTest.BillingAccountName == "" {
		secretValue, err := config.AccessSecretString(r.Context(), cfg.SelfTest.RestoreBillingAccountNameSecret, "latest")
		if err != nil {
			http.Error(w, "resolve restore billing account secret: "+err.Error(), http.StatusInternalServerError)
			return
		}
		cfg.SelfTest.BillingAccountName = strings.TrimSpace(secretValue)
	}
	bc := s.opt.Billing
	var rr selftest.ReconcileRunner
	if mode == "setup_validation" {
		lister, resolver, err := s.reconcileClients(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		runner := s.opt.ReconcileRunner
		if runner == nil {
			runner = reconcile.Run
		}
		rr = func(ctx context.Context, cfg *config.Config) (reconcile.Result, error) {
			return runner(ctx, cfg, lister, resolver)
		}
	} else if bc == nil && mode != "dry_run" {
		bc, err = billing.NewCloudClient(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	writeJSON(w, http.StatusOK, selftest.RunWithReconciler(r.Context(), mode, cfg, bc, s.opt.SelfTestStore, rr))
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
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) reconcileClients(ctx context.Context) (reconcile.BudgetLister, reconcile.ProjectResolver, error) {
	lister := s.opt.ReconcileLister
	resolver := s.opt.ProjectResolver
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
	var v any
	if err := json.NewDecoder(r.Body).Decode(&v); err != nil {
		return nil
	}
	b, _ := json.Marshal(v)
	return b
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
