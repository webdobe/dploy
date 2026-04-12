package cli

import "strings"

// Policy requirement names recognised by the CLI. Policy rules refer
// to these strings in their `require:` field.
const (
	reqConfirm      = "confirm"
	reqSanitization = "sanitization"
)

// collectSatisfiedRequirements reads the global acknowledgment flags
// (--confirm, --sanitized) and returns the set of policy requirement
// names the caller has acknowledged for this invocation.
//
// This list feeds operation.Request.Satisfied, which policy.Evaluate
// uses to compute Decision.Unmet.
func collectSatisfiedRequirements() []string {
	var out []string
	if confirmFlag {
		out = append(out, reqConfirm)
	}
	if sanitizedFlag {
		out = append(out, reqSanitization)
	}
	return out
}

// suggestFlagsFor maps unmet requirement names back to the flags that
// would satisfy them. Returns a human-readable hint string, or "" when
// none of the unmet items have a known flag mapping (future-proofing
// for requirement types we haven't wired a flag for yet).
func suggestFlagsFor(unmet []string) string {
	var flags []string
	seen := map[string]bool{}
	for _, u := range unmet {
		var flag string
		switch u {
		case reqConfirm:
			flag = "--confirm"
		case reqSanitization:
			flag = "--sanitized"
		}
		if flag != "" && !seen[flag] {
			seen[flag] = true
			flags = append(flags, flag)
		}
	}
	if len(flags) == 0 {
		return ""
	}
	return "pass " + strings.Join(flags, ", ") + " to acknowledge"
}
