package inspector2

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// featureRelationship records that an account has an enabled-or-disabled scan
// feature status (account-to-feature-status). It returns false when the
// feature key is empty so empty SDK entries do not emit dangling edges.
func featureRelationship(
	boundary awscloud.Boundary,
	account AccountStatus,
	feature FeatureStatus,
) (awscloud.RelationshipObservation, bool) {
	accountID := firstNonEmpty(account.AccountID, boundary.AccountID)
	featureKey := strings.TrimSpace(feature.Feature)
	if accountID == "" || featureKey == "" {
		return awscloud.RelationshipObservation{}, false
	}
	featureID := "inspector2/feature/" + accountID + "/" + featureKey
	return awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipInspector2AccountHasFeatureStatus,
		SourceResourceID: accountResourceID(accountID),
		TargetResourceID: featureID,
		TargetType:       awscloud.ResourceTypeInspector2Account,
		Attributes: map[string]any{
			"feature": featureKey,
			"status":  strings.TrimSpace(feature.Status),
		},
		SourceRecordID: accountResourceID(accountID) + "->" + featureID,
	}, true
}

// memberRelationship records that a member account is managed by the delegated
// administrator account (member-to-administrator).
func memberRelationship(
	boundary awscloud.Boundary,
	member MemberAccount,
) awscloud.RelationshipObservation {
	memberID := strings.TrimSpace(member.AccountID)
	adminID := firstNonEmpty(member.AdministratorID, boundary.AccountID)
	return awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipInspector2MemberManagedByAdministrator,
		SourceResourceID: memberResourceID(memberID),
		TargetResourceID: accountResourceID(adminID),
		TargetType:       awscloud.ResourceTypeInspector2Account,
		Attributes: map[string]any{
			"account_id":          memberID,
			"administrator_id":    adminID,
			"relationship_status": strings.TrimSpace(member.RelationshipStatus),
		},
		SourceRecordID: memberResourceID(memberID) + "->" + accountResourceID(adminID),
	}
}

// cisTargetRelationship records that a CIS scan configuration targets one
// member account (CIS-config-to-target-account-set). It returns false for an
// empty account id so target lists with blanks do not emit dangling edges.
func cisTargetRelationship(
	boundary awscloud.Boundary,
	config CisScanConfiguration,
	targetAccount string,
) (awscloud.RelationshipObservation, bool) {
	configARN := strings.TrimSpace(config.ARN)
	accountID := strings.TrimSpace(targetAccount)
	if configARN == "" || accountID == "" {
		return awscloud.RelationshipObservation{}, false
	}
	configID := firstNonEmpty(configARN, config.Name)
	return awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipInspector2CisScanConfigurationTargetsAccount,
		SourceResourceID: configID,
		SourceARN:        configARN,
		TargetResourceID: accountResourceID(accountID),
		TargetType:       awscloud.ResourceTypeInspector2Account,
		Attributes: map[string]any{
			"target_account_id": accountID,
		},
		SourceRecordID: configID + "->" + accountResourceID(accountID),
	}, true
}
