#!/usr/bin/env bash
# shellcheck shell=bash
# shellcheck disable=SC2034
# Hand-derived from CrossRepoRelationshipHandler (deployment_mapping) and the
# canonical Repository DEPENDS_ON writer; custom cells own maintenance setup.
IFA_FAMILY_PIN_BLOCKER_KIND="shared_intent_lock"
IFA_FAMILY_PIN_WAIT_STAGE="handler"
IFA_FAMILY_PIN_WAIT_KEY="deployment_mapping"
IFA_FAMILY_PIN_ANCHOR="MERGE (source_repo)-[rel:DEPENDS_ON]->(target_repo)"
IFA_FAMILY_PIN_SHARED_CELL=0
IFA_FAMILY_PIN_CELL_KIND="custom"
