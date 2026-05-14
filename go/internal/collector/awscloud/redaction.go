package awscloud

import (
	"github.com/eshu-hq/eshu/go/internal/redact"
)

const (
	// RedactionPolicyVersion identifies the AWS launch scanner sensitive-key
	// policy attached to redacted fact payload values.
	RedactionPolicyVersion = "aws-launch-2026-05-14"
)

var awsRedactionRules = mustAWSRedactionRules()

// RedactString returns the shared AWS redaction payload for a scalar string.
//
// AWS environment values are treated as unknown provider schema, so callers
// preserve key names but redact all runtime values. Known sensitive key names
// receive a stronger reason label for audit and downstream proof.
func RedactString(raw string, source string, key redact.Key) map[string]any {
	decision := awsRedactionRules.Classify(source, redact.SchemaUnknown, redact.FieldScalar)
	value := redact.String(raw, decision.Reason, decision.Source, key)
	return map[string]any{
		"marker":          value.Marker,
		"reason":          value.Reason,
		"source":          value.Source,
		"ruleset_version": decision.RuleSetVersion,
	}
}

func mustAWSRedactionRules() redact.RuleSet {
	rules, err := redact.NewRuleSet(RedactionPolicyVersion, awsSensitiveKeys)
	if err != nil {
		panic(err)
	}
	return rules
}

var awsSensitiveKeys = []string{
	"access_key",
	"access_key_id",
	"api_key",
	"apikey",
	"auth_token",
	"authorization",
	"aws_access_key_id",
	"aws_secret_access_key",
	"aws_session_token",
	"bearer_token",
	"client_secret",
	"connection_string",
	"database_url",
	"db_password",
	"db_url",
	"dsn",
	"github_token",
	"jdbc_url",
	"mongo_uri",
	"password",
	"passwd",
	"private_key",
	"pwd",
	"redis_url",
	"secret",
	"secret_access_key",
	"secret_key",
	"session_token",
	"slack_webhook_url",
	"stripe_secret_key",
	"token",
	"webhook_url",
}
