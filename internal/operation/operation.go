// Package operation defines the core operation model.
//
// Every user-facing action in dploy — deploy, capture, restore, rollback,
// validate, status, inspect — is an operation. The lifecycle is fixed:
//
//	request → resolve → validate → plan → execute → record
//
// This lifecycle is the contract all interfaces (CLI, CI, future GUI)
// share. It must not be bypassed or forked per operation type.
package operation

import "time"

// Type enumerates the operation categories defined in OPERATION_MODEL.md.
type Type string

const (
	TypeDeploy   Type = "deploy"
	TypeCapture  Type = "capture"
	TypeRestore  Type = "restore"
	TypeRollback Type = "rollback"
	TypeValidate Type = "validate"
	TypeStatus   Type = "status"
	TypeInspect  Type = "inspect"
	TypePromote  Type = "promote"
)

// Request is the pre-resolution ask: what the user (or CI, or future UI) wants.
//
// Class carries the resolved environment's class so trusted policy rules
// can match on it (e.g. "deny deploy to any production-class environment").
//
// Satisfied lists policy requirements the caller has acknowledged via
// CLI flags (e.g. "confirm" via --confirm, "sanitization" via
// --sanitized). Policy evaluation treats required items present here as met.
type Request struct {
	Type        Type
	Environment string
	Class       string   // environment class for the operation
	Targets     []string // optional scope restriction
	Roles       []string // optional scope restriction
	Resources   []string // capture/restore: database, files, media, etc.
	Artifact    string   // optional artifact reference
	Satisfied   []string // acknowledged policy requirements
}

// ResultStatus is the final state of an operation run. See FAILURE_MODEL.md.
type ResultStatus string

const (
	StatusSuccess          ResultStatus = "success"
	StatusFailedValidation ResultStatus = "failed_validation"
	StatusFailedPolicy     ResultStatus = "failed_policy"
	StatusFailedResolution ResultStatus = "failed_resolution"
	StatusFailedExecution  ResultStatus = "failed_execution"
	StatusPartialFailure   ResultStatus = "partial_failure"
	StatusCancelled        ResultStatus = "cancelled"
)

// IsFailure reports whether the status indicates any kind of failure,
// including partial failure. From an automation perspective all of these
// are non-zero exit scenarios.
func (s ResultStatus) IsFailure() bool {
	return s != StatusSuccess
}

// Result is the structured record of an operation run. It is the basis
// for logs, status, audit, and future rollback eligibility.
type Result struct {
	Type        Type         `json:"type"`
	Environment string       `json:"environment,omitempty"`
	SourceEnv   string       `json:"source_env,omitempty"`
	TargetEnv   string       `json:"target_env,omitempty"`
	Resources   []string     `json:"resources,omitempty"`
	Artifact    string       `json:"artifact,omitempty"`
	SnapshotID  string       `json:"snapshot_id,omitempty"`
	Status      ResultStatus `json:"status"`
	Steps       []StepResult `json:"steps,omitempty"`
	StartedAt   time.Time    `json:"started_at"`
	FinishedAt  time.Time    `json:"finished_at"`
	LogPath     string       `json:"log_path,omitempty"`
	PolicySrc   string       `json:"policy_source,omitempty"`
	Error       string       `json:"error,omitempty"`
}

// StepResult records the outcome of a single step against a single target.
type StepResult struct {
	Index    int           `json:"index"`
	Command  string        `json:"command"`
	Target   string        `json:"target"`
	Status   ResultStatus  `json:"status"`
	ExitCode int           `json:"exit_code"`
	Output   string        `json:"output,omitempty"`
	Error    string        `json:"error,omitempty"`
	Duration time.Duration `json:"duration_ns"`
}
