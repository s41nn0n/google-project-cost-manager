package config

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"

	secretmanager "cloud.google.com/go/secretmanager/apiv1"
	secretmanagerpb "cloud.google.com/go/secretmanager/apiv1/secretmanagerpb"
	"gopkg.in/yaml.v3"
)

const (
	BackendFile          = "file"
	BackendSecretManager = "secretmanager"
)

type Config struct {
	SchemaVersion               int                      `yaml:"schemaVersion" json:"schemaVersion"`
	OrganizationID              string                   `yaml:"organizationId" json:"organizationId"`
	ControlProjectID            string                   `yaml:"controlProjectId" json:"controlProjectId"`
	EventState                  EventStateConfig         `yaml:"eventState" json:"eventState"`
	Defaults                    Defaults                 `yaml:"defaults" json:"defaults"`
	Budgets                     []Budget                 `yaml:"budgets" json:"budgets"`
	Projects                    map[string]ProjectPolicy `yaml:"projects" json:"projects"`
	ProtectedProjects           []string                 `yaml:"protectedProjects" json:"protectedProjects"`
	MaxProjectsDisabledPerEvent int                      `yaml:"maxProjectsDisabledPerEvent" json:"maxProjectsDisabledPerEvent"`
	UnknownAlertPolicy          string                   `yaml:"unknownAlertPolicy" json:"unknownAlertPolicy"`
	UnknownAlertProjects        []string                 `yaml:"unknownAlertProjects" json:"unknownAlertProjects"`
	SelfTest                    SelfTestConfig           `yaml:"selfTest" json:"selfTest"`
	Reconcile                   ReconcileConfig          `yaml:"reconcile" json:"reconcile"`
}

type EventStateConfig struct {
	Backend    string `yaml:"backend" json:"backend"`
	ProjectID  string `yaml:"projectId" json:"projectId"`
	DatabaseID string `yaml:"databaseId" json:"databaseId"`
	Collection string `yaml:"collection" json:"collection"`
}

type ReconcileConfig struct {
	Enabled             bool     `yaml:"enabled" json:"enabled"`
	SourceOfTruth       string   `yaml:"sourceOfTruth" json:"sourceOfTruth"`
	Mode                string   `yaml:"mode" json:"mode"`
	BillingAccountName  string   `yaml:"billingAccountName" json:"billingAccountName"`
	BillingAccountNames []string `yaml:"billingAccountNames" json:"billingAccountNames"`
	RequiredPubSubTopic string   `yaml:"requiredPubSubTopic" json:"requiredPubSubTopic"`
}

type Defaults struct {
	Threshold float64 `yaml:"threshold" json:"threshold"`
	DryRun    *bool   `yaml:"dryRun" json:"dryRun"`
	Action    string  `yaml:"action" json:"action"`
}

type Budget struct {
	BudgetResourceName string   `yaml:"budgetResourceName" json:"budgetResourceName"`
	BillingAccountName string   `yaml:"billingAccountName" json:"billingAccountName"`
	ProjectID          string   `yaml:"projectId" json:"projectId"`
	ProjectNumber      string   `yaml:"projectNumber" json:"projectNumber"`
	EnforcementMode    string   `yaml:"enforcementMode" json:"enforcementMode"`
	Names              []string `yaml:"names" json:"names"`
	Projects           []string `yaml:"projects" json:"projects"`
	Threshold          float64  `yaml:"threshold" json:"threshold"`
	DryRun             *bool    `yaml:"dryRun" json:"dryRun"`
	Action             string   `yaml:"action" json:"action"`
}

type ProjectPolicy struct {
	DryRun *bool  `yaml:"dryRun" json:"dryRun"`
	Action string `yaml:"action" json:"action"`
}

type SelfTestConfig struct {
	Enabled                         bool   `yaml:"enabled" json:"enabled"`
	Mode                            string `yaml:"mode" json:"mode"`
	TestProjectID                   string `yaml:"testProjectId" json:"testProjectId"`
	BillingAccountName              string `yaml:"billingAccountName" json:"billingAccountName"`
	RestoreBillingAccountNameSecret string `yaml:"restoreBillingAccountNameSecret" json:"restoreBillingAccountNameSecret"`
	AllowedProjectIDPattern         string `yaml:"allowedProjectIdPattern" json:"allowedProjectIdPattern"`
	MinimumIntervalHours            int    `yaml:"minimumIntervalHours" json:"minimumIntervalHours"`
}

func LoadFromEnv(ctx context.Context) (*Config, error) {
	backend := os.Getenv("CONFIG_BACKEND")
	if backend == "" {
		backend = BackendFile
	}
	switch backend {
	case BackendFile:
		path := os.Getenv("CONFIG_PATH")
		if path == "" {
			path = "configs/example.yaml"
		}
		return LoadFile(path)
	case BackendSecretManager:
		name := os.Getenv("CONFIG_SECRET_NAME")
		version := os.Getenv("CONFIG_SECRET_VERSION")
		if version == "" {
			version = "latest"
		}
		if name == "" {
			return nil, errors.New("CONFIG_SECRET_NAME is required")
		}
		return LoadSecret(ctx, name, version)
	default:
		return nil, fmt.Errorf("unsupported CONFIG_BACKEND %q", backend)
	}
}

func LoadFile(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(b)
}

func LoadSecret(ctx context.Context, secretName, version string) (*Config, error) {
	data, err := AccessSecretString(ctx, secretName, version)
	if err != nil {
		return nil, err
	}
	return Parse([]byte(data))
}

func AccessSecretString(ctx context.Context, secretName, version string) (string, error) {
	client, err := secretmanager.NewClient(ctx)
	if err != nil {
		return "", err
	}
	defer client.Close()
	name := secretName
	if version != "" && !regexp.MustCompile(`/versions/[^/]+$`).MatchString(name) {
		name += "/versions/" + version
	}
	res, err := client.AccessSecretVersion(ctx, &secretmanagerpb.AccessSecretVersionRequest{Name: name})
	if err != nil {
		return "", err
	}
	return string(res.Payload.Data), nil
}

func Parse(data []byte) (*Config, error) {
	var c Config
	if err := yaml.Unmarshal(data, &c); err != nil {
		return nil, err
	}
	c.ApplyDefaults()
	return &c, c.Validate()
}

func (c *Config) ApplyDefaults() {
	if c.Defaults.Threshold == 0 {
		c.Defaults.Threshold = 0.8
	}
	if c.Defaults.DryRun == nil {
		v := true
		c.Defaults.DryRun = &v
	}
	if c.Defaults.Action == "" {
		c.Defaults.Action = "disable_billing"
	}
	if c.UnknownAlertPolicy == "" {
		c.UnknownAlertPolicy = "dry_run"
	}
	if c.SchemaVersion >= 2 {
		if c.UnknownAlertPolicy == "dry_run" {
			c.UnknownAlertPolicy = "ignore"
		}
		if c.MaxProjectsDisabledPerEvent == 0 {
			c.MaxProjectsDisabledPerEvent = 1
		}
		if c.EventState.Collection == "" {
			c.EventState.Collection = "budget-events"
		}
	}
	if c.Projects == nil {
		c.Projects = map[string]ProjectPolicy{}
	}
	if c.Reconcile.SourceOfTruth == "" {
		c.Reconcile.SourceOfTruth = "gcp_budgets"
	}
	if c.Reconcile.Mode == "" {
		c.Reconcile.Mode = "diff_only"
	}
}

func (c *Config) Validate() error {
	if c.Defaults.Threshold < 0 || c.Defaults.Threshold > 1 {
		return errors.New("defaults.threshold must be between 0 and 1")
	}
	if !validAction(c.Defaults.Action) {
		return fmt.Errorf("defaults.action must be one of disable_billing, dry_run, ignore")
	}
	if !validUnknownAlertPolicy(c.UnknownAlertPolicy) {
		return fmt.Errorf("unknownAlertPolicy must be one of dry_run, ignore, disable_billing")
	}
	if c.SchemaVersion >= 2 && c.MaxProjectsDisabledPerEvent != 1 {
		return errors.New("schemaVersion 2 requires maxProjectsDisabledPerEvent: 1")
	}
	if c.SchemaVersion >= 2 && c.UnknownAlertPolicy != "ignore" {
		return errors.New("schemaVersion 2 requires unknownAlertPolicy: ignore")
	}
	if c.SchemaVersion >= 2 && c.ControlProjectID == "" {
		return errors.New("schemaVersion 2 requires controlProjectId")
	}
	if c.SchemaVersion >= 2 {
		protected := false
		for _, p := range c.ProtectedProjects {
			if p == c.ControlProjectID {
				protected = true
			}
		}
		if !protected {
			return errors.New("control project must be protected")
		}
	}
	if c.MaxProjectsDisabledPerEvent < 0 {
		return errors.New("maxProjectsDisabledPerEvent cannot be negative")
	}
	for _, b := range c.Budgets {
		if c.SchemaVersion >= 2 {
			if b.EnforcementMode != "dry_run" && b.EnforcementMode != "live" {
				return errors.New("budget.enforcementMode must be dry_run or live")
			}
			if b.BudgetResourceName == "" || b.BillingAccountName == "" || b.ProjectID == "" || b.ProjectNumber == "" {
				return errors.New("schemaVersion 2 budgets require canonical budget, billing account, project ID, and project number")
			}
			if len(b.Projects) != 1 || b.Projects[0] != b.ProjectID {
				return errors.New("schemaVersion 2 budgets must cover exactly their canonical project")
			}
			if b.EnforcementMode == "live" && (c.EventState.Backend != "firestore" || c.EventState.ProjectID == "" || c.EventState.DatabaseID == "") {
				return errors.New("live enforcement requires persistent Firestore eventState")
			}
		}
		if b.Threshold < 0 || b.Threshold > 1 {
			return errors.New("budget.threshold must be between 0 and 1")
		}
		if b.Action != "" && !validAction(b.Action) {
			return fmt.Errorf("budget.action must be one of disable_billing, dry_run, ignore")
		}
	}
	for _, p := range c.Projects {
		if p.Action != "" && !validAction(p.Action) {
			return fmt.Errorf("project.action must be one of disable_billing, dry_run, ignore")
		}
	}
	if c.Reconcile.SourceOfTruth != "gcp_budgets" && c.Reconcile.SourceOfTruth != "terraform" {
		return fmt.Errorf("reconcile.sourceOfTruth must be gcp_budgets or terraform")
	}
	if c.Reconcile.Mode != "diff_only" {
		return fmt.Errorf("reconcile.mode must be diff_only")
	}
	if c.Reconcile.Enabled {
		if len(c.Reconcile.EffectiveBillingAccountNames()) == 0 {
			return errors.New("at least one of reconcile.billingAccountNames or reconcile.billingAccountName is required when reconcile.enabled is true")
		}
		if c.Reconcile.RequiredPubSubTopic == "" {
			return errors.New("reconcile.requiredPubSubTopic is required when reconcile.enabled is true")
		}
	}
	if c.SelfTest.Mode != "" && !validSelfTestMode(c.SelfTest.Mode) {
		return fmt.Errorf("selfTest.mode must be one of dry_run, billing_read_check, billing_toggle, setup_validation")
	}
	if c.SelfTest.MinimumIntervalHours < 0 {
		return errors.New("selfTest.minimumIntervalHours cannot be negative")
	}
	if p := c.SelfTest.AllowedProjectIDPattern; p != "" {
		if _, err := regexp.Compile(p); err != nil {
			return fmt.Errorf("invalid allowedProjectIdPattern: %w", err)
		}
	}
	return nil
}

func (r ReconcileConfig) EffectiveBillingAccountNames() []string {
	seen := map[string]bool{}
	var accounts []string
	add := func(name string) {
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		accounts = append(accounts, name)
	}
	for _, name := range r.BillingAccountNames {
		add(name)
	}
	add(r.BillingAccountName)
	return accounts
}

func validAction(a string) bool {
	switch a {
	case "disable_billing", "dry_run", "ignore":
		return true
	default:
		return false
	}
}

func validUnknownAlertPolicy(p string) bool {
	switch p {
	case "dry_run", "ignore", "disable_billing":
		return true
	default:
		return false
	}
}

func validSelfTestMode(m string) bool {
	switch m {
	case "dry_run", "billing_read_check", "billing_toggle", "setup_validation":
		return true
	default:
		return false
	}
}

func Bool(v bool) *bool { return &v }
